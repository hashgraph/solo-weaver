// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/network/nftexec"
	"github.com/joomcode/errorx"
	"github.com/rs/zerolog"
)

// Observation is what ObserveDrift found on the host.
type Observation struct {
	// Unobserved is set when the table could not be read, so TablePresent and
	// the content may not be taken as absence. ConfigPresent still says whether
	// a table is expected; a config whose presence is unknown counts as present.
	Unobserved bool
	// TablePresent reports whether the table is live in the kernel.
	TablePresent bool
	// ConfigPresent reports whether the host has a config the table is
	// expected from: the YAML config, or the rendered ruleset load() falls back
	// to without it.
	ConfigPresent bool
	// ConfigUnreadable is set when the YAML config exists but could not be
	// read or parsed, so the content was not compared.
	ConfigUnreadable bool
	// Fields pairs each compared part of the YAML config with the live table,
	// set only when both exist. Every field is included, equal or not.
	Fields []FieldObservation
	// Rules lists each allow rule the YAML config or the live table holds, by
	// name, set with Fields. A rule's fields are compared only while it is in
	// both.
	Rules []RuleObservation
}

// RuleObservation is one allow rule and where it was found.
type RuleObservation struct {
	Name     string
	InConfig bool
	// Live reports whether the table declares the rule's address set.
	Live bool
}

// FieldObservation is one part of the YAML config and the live table's
// counterpart, both normalized and sorted so that equal means unchanged.
type FieldObservation struct {
	// Field is the config key path, such as "configFile.mgmt.cidrs".
	Field  string
	Config []string
	Live   []string
}

// observeTimeout bounds ObserveDrift: a block node command calls it while
// holding its component locks, so a stalled nft must not hold them for long.
// Within it, the read waits up to observeLockWait for a mutation in progress
// to release the apply lock, checking every observeLockPoll.
const (
	observeTimeout  = 10 * time.Second
	observeLockWait = 5 * time.Second
	observeLockPoll = 25 * time.Millisecond
)

// ObserveDrift reads the host firewall as it is, for comparison with what the
// host's config and the recorded decision expect. It writes nothing, so a
// read-only command can call it.
func (m *Manager) ObserveDrift(ctx context.Context) Observation {
	ctx, cancel := context.WithTimeout(ctx, m.observeTimeout)
	defer cancel()
	// Under the apply lock, so a mutation in progress is not read half done:
	// the config, the DNS cache and the table change together.
	release, err := lockIfPresent(ctx, m.lockPath, m.lockWait, observeLockPoll)
	if err != nil {
		configPresent, statErr := m.configPresent()
		configPresent = configPresent || statErr != nil
		logReadFailure(configPresent, err, "host firewall could not be read: apply lock not taken")
		return Observation{Unobserved: true, ConfigPresent: configPresent}
	}
	defer release()

	configPresent, err := m.configPresent()
	if err != nil {
		logReadFailure(true, err, "host firewall could not be read: config presence unknown")
		return Observation{Unobserved: true, ConfigPresent: true}
	}
	listing, tablePresent, err := m.listTable(ctx)
	if err != nil {
		logReadFailure(configPresent, err, "host firewall could not be read: nft listing failed")
		return Observation{Unobserved: true, ConfigPresent: configPresent}
	}
	o := Observation{TablePresent: tablePresent, ConfigPresent: configPresent}
	if tablePresent {
		o.Fields, o.Rules, o.ConfigUnreadable = m.observeContent(listing)
	}
	o.log()
	return o
}

// log writes what was read at debug, in full: the drift lines built from it
// are capped. Keys follow the drift warning log (component, field, persisted,
// live), with the config side as persisted.
func (o Observation) log() {
	line := func(field, config, live string) {
		logx.As().Debug().Str("component", "firewall").Str("field", field).
			Str("persisted", config).Str("live", live).Msg("Host firewall as read")
	}
	present := func(p bool) string {
		if p {
			return "present"
		}
		return "absent"
	}
	line("configFile", present(o.ConfigPresent), present(o.TablePresent))
	for _, r := range o.Rules {
		line("configFile.allow["+r.Name+"]", present(r.InConfig), present(r.Live))
	}
	for _, f := range o.Fields {
		line(f.Field, strings.Join(f.Config, ", "), strings.Join(f.Live, ", "))
	}
}

// logReadFailure logs why the host firewall could not be read: a warning when
// a table is expected, since the run then reports it as unread, else debug.
func logReadFailure(expected bool, err error, msg string) {
	readFailure(expected, err).Msg(msg)
}

func readFailure(expected bool, err error) *zerolog.Event {
	e := logx.As().Debug()
	if expected {
		e = logx.As().Warn()
	}
	return e.Err(err).Str("component", "firewall").Str("table", TableName)
}

// observeContent compares the YAML config with the live listing, block by
// block and then allow rule by allow rule. It returns nothing when there is no
// YAML config to compare with: the rendered ruleset alone still expects a
// table, but holds addresses, not what the operator wrote. The last result
// reports a YAML config that exists but could not be read.
func (m *Manager) observeContent(listing string) ([]FieldObservation, []RuleObservation, bool) {
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			logx.As().Debug().Str("component", "firewall").Str("path", m.configPath).
				Msg("host firewall content not compared: no YAML config, only the rendered ruleset")
			return nil, nil, false
		}
		readFailure(true, err).Str("path", m.configPath).Msg("host firewall content not compared: config unreadable")
		return nil, nil, true
	}
	cfg, err := ParseConfig(data)
	if err != nil {
		readFailure(true, err).Str("path", m.configPath).Msg("host firewall content not compared: config does not parse")
		return nil, nil, true
	}
	t, err := cfg.Table()
	if err != nil {
		readFailure(true, err).Str("path", m.configPath).Msg("host firewall content not compared: config does not parse")
		return nil, nil, true
	}
	byName, cacheRead := m.cachedAddresses(t)
	expanded, err := t.expandFQDNs(byName)
	if err != nil {
		readFailure(true, err).Str("path", m.configPath).Msg("host firewall content not compared: names could not be expanded")
		return nil, nil, true
	}

	// Without a readable cache, what a name last rendered as is unknown.
	namesUnknown := func(r *Rule) bool {
		unknown := !cacheRead && len(ruleFQDNs(r)) > 0
		if unknown {
			logx.As().Debug().Str("rule", r.Name).Msg("host firewall addresses not compared: no readable DNS cache for the names they hold")
		}
		return unknown
	}

	var fields []FieldObservation
	for _, b := range []struct {
		key   string
		rule  *Rule
		cidrs bool
		ports bool
	}{
		{key: RuleMgmt, rule: &expanded.Mgmt, cidrs: !namesUnknown(&t.Mgmt), ports: true},
		{key: RuleBlocked, rule: &expanded.Blocked, cidrs: !namesUnknown(&t.Blocked)},
		{key: RuleInCluster, rule: &expanded.InCluster, cidrs: !cfg.InClusterCIDRsUnset(), ports: true},
	} {
		if b.cidrs {
			live := append(setElements(listing, addrSetName(b.rule.Name)), setElements(listing, v6SetName(b.rule.Name))...)
			fields = append(fields, FieldObservation{
				Field:  "configFile." + b.key + ".cidrs",
				Config: normalizeCIDRs(b.rule.CIDRs),
				Live:   normalizeCIDRs(live),
			})
		} else if b.key == RuleInCluster {
			logx.As().Debug().Str("field", "configFile."+b.key+".cidrs").Msg(
				"host firewall field not compared: the config leaves it to be detected at apply")
		}
		if b.ports {
			fields = append(fields, FieldObservation{
				Field:  "configFile." + b.key + ".ports",
				Config: normalizePorts(b.rule.Ports),
				Live:   normalizePorts(setElements(listing, portsSetName(b.rule.Name))),
			})
		}
	}
	var rules []RuleObservation
	var liveRules []*Rule
	for i := range expanded.Allow {
		r := &expanded.Allow[i]
		// Both address sets: one alone may belong to another rule, as web6 is
		// the IPv6 set of a rule named web.
		live := setDeclared(listing, addrSetName(r.Name)) && setDeclared(listing, v6SetName(r.Name))
		rules = append(rules, RuleObservation{Name: r.Name, InConfig: true, Live: live})
		if !live {
			continue
		}
		liveRules = append(liveRules, r)
		key := "configFile.allow[" + r.Name + "]"
		if !namesUnknown(&t.Allow[i]) {
			fields = append(fields, FieldObservation{
				Field:  key + ".cidrs",
				Config: normalizeCIDRs(r.CIDRs),
				Live:   normalizeCIDRs(append(setElements(listing, addrSetName(r.Name)), setElements(listing, v6SetName(r.Name))...)),
			})
		}
		fields = append(fields, FieldObservation{
			Field:  key + ".ports",
			Config: normalizePorts(r.Ports),
			Live:   normalizePorts(setElements(listing, portsSetName(r.Name))),
		})
	}
	rules = append(rules, liveOnlyRules(listing, append([]*Rule{&expanded.Mgmt, &expanded.Blocked, &expanded.InCluster}, liveRules...))...)
	slices.SortFunc(rules, func(a, b RuleObservation) int { return strings.Compare(a.Name, b.Name) })
	return fields, rules, false
}

// liveOnlyRules returns the allow rules the live table declares beyond known,
// the rules already matched to it: a declared set none of them derives, with
// its IPv6 companion declared too, is the address set of a rule of that name.
func liveOnlyRules(listing string, known []*Rule) []RuleObservation {
	leftover := map[string]bool{}
	for _, name := range setNames(listing) {
		leftover[name] = true
	}
	for _, r := range known {
		for _, name := range []string{addrSetName(r.Name), v6SetName(r.Name), portsSetName(r.Name)} {
			delete(leftover, name)
		}
	}
	var out []RuleObservation
	for name := range leftover {
		if leftover[v6SetName(name)] {
			out = append(out, RuleObservation{Name: name, Live: true})
		}
	}
	return out
}

// cachedAddresses maps each name in t to the addresses the DNS cache holds for
// it, as the last apply rendered them, without resolving anything. A name the
// cache does not hold was never applied, so nothing live stands for it: it maps
// to itself and shows as the config's own. The second result reports whether
// the cache could be read; without it every name maps to nothing.
func (m *Manager) cachedAddresses(t *Table) (map[string][]string, bool) {
	names := t.fqdnEntries()
	if len(names) == 0 {
		return nil, true
	}
	cache, ok := readDNSCache(m.dnsCachePath)
	byName := make(map[string][]string, len(names))
	for _, name := range names {
		entry, cached := cache[name]
		switch {
		case !ok:
			byName[name] = nil
		case !cached:
			logx.As().Debug().Str("fqdn", name).Msg("host firewall name never applied: no cached addresses")
			byName[name] = []string{name}
		default:
			byName[name] = entry.elements()
		}
	}
	return byName, ok
}

// readDNSCache reads the cache as loadDNSCache does, but reports whether it
// could, and logs nothing: a read-only caller must not mistake a lost cache for
// names that were never applied.
func readDNSCache(path string) (dnsCache, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var c dnsCache
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, false
	}
	return c, true
}

// configPresent reports whether load() would find a config, without loading
// it: load() logs on every fallback to the rendered ruleset.
func (m *Manager) configPresent() (bool, error) {
	for _, path := range []string{m.configPath, m.nftPath} {
		_, err := os.Stat(path)
		switch {
		case err == nil:
			return true, nil
		case !errors.Is(err, fs.ErrNotExist):
			return false, err
		}
	}
	return false, nil
}

// listLiveTable lists the live table with the system nft binary.
func listLiveTable(ctx context.Context) (string, bool, error) {
	bin, _ := nftexec.Binary()
	return nftexec.ListTable(ctx, bin, TableName)
}

// lockIfPresent takes the apply lock at path, waiting up to wait for a holder
// to let go. It never creates the lock file or its directory, so a host where
// nothing has applied a firewall is read unlocked.
func lockIfPresent(ctx context.Context, path string, wait, poll time.Duration) (func(), error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return func() {}, nil
	}
	if err != nil {
		return nil, lockOpenError(path, err)
	}
	fd := int(f.Fd())
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	tick := time.NewTicker(poll)
	defer tick.Stop()
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(fd, syscall.LOCK_UN)
				_ = f.Close()
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			_ = f.Close()
			return nil, errorx.ExternalError.Wrap(err, "failed to lock %s", path)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, errorx.TimeoutElapsed.Wrap(ctx.Err(), "gave up waiting for %s", path)
		case <-deadline.C:
			_ = f.Close()
			return nil, errorx.TimeoutElapsed.New("%s held for over %s", path, wait)
		case <-tick.C:
		}
	}
}

// lockOpenError explains a lock file that could not be opened. Every locker
// creates it root-only, so a non-root read is refused here before nft is run.
func lockOpenError(path string, err error) error {
	if errors.Is(err, fs.ErrPermission) {
		return errorx.ExternalError.Wrap(err, "not permitted to take the apply lock %s (needs root)", path)
	}
	return errorx.ExternalError.Wrap(err, "failed to open %s", path)
}
