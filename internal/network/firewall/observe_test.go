// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// observeManager wires a Manager to temp paths and a fake live listing.
func observeManager(t *testing.T, live string, present bool, listErr error) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	m := NewManagerWithConfig(Config{
		NftPath:    filepath.Join(dir, "fw.nft"),
		ConfigPath: filepath.Join(dir, "fw.yaml"),
		LockPath:   filepath.Join(dir, "run", ".applying"),
	})
	m.listTable = func(context.Context) (string, bool, error) { return live, present, listErr }
	return m, dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func TestObserveDrift_ConfigAndTablePresent(t *testing.T) {
	m, _ := observeManager(t, liveListing(t, "live-listing.golden.txt"), true, nil)
	writeFile(t, m.configPath, goldenConfig)

	got := m.ObserveDrift(context.Background())

	assert.True(t, got.TablePresent)
	assert.True(t, got.ConfigPresent)
	assert.False(t, got.Unobserved)
	assert.False(t, got.ConfigUnreadable)
}

func TestObserveDrift_RenderedRulesetAloneCountsAsAConfig(t *testing.T) {
	// load() falls back to the rendered ruleset when the YAML is gone, so a
	// table is still expected.
	m, _ := observeManager(t, "", false, nil)
	writeFile(t, m.nftPath, "table inet weaver-host-firewall {\n}\n")

	got := m.ObserveDrift(context.Background())

	assert.Equal(t, Observation{ConfigPresent: true}, got)
}

func TestObserveDrift_NeitherConfigNorTable(t *testing.T) {
	m, _ := observeManager(t, "", false, nil)

	assert.Equal(t, Observation{}, m.ObserveDrift(context.Background()))
}

func TestObserveDrift_FailedListingIsUnobservedNotAbsent(t *testing.T) {
	m, _ := observeManager(t, "", false, errorx.ExternalError.New("nft list table failed"))
	writeFile(t, m.configPath, "version: 1\n")

	got := m.ObserveDrift(context.Background())

	// The config was read, so the producer knows a table is expected.
	assert.Equal(t, Observation{Unobserved: true, ConfigPresent: true}, got)
}

func TestObserveDrift_StalledListingIsUnobserved(t *testing.T) {
	// A wedged nft must not hold the command's component locks forever.
	m, _ := observeManager(t, "", false, nil)
	m.observeTimeout = 50 * time.Millisecond
	m.listTable = func(ctx context.Context) (string, bool, error) {
		<-ctx.Done()
		return "", false, ctx.Err()
	}

	got := m.ObserveDrift(context.Background())

	assert.True(t, got.Unobserved)
}

// goldenConfig is the YAML the golden ruleset (testdata/*.golden.nft) renders from.
const goldenConfig = `version: 1
mgmt:
  cidrs: [10.0.0.0/8, 192.168.0.0/16]
  ports: ["22"]
blocked:
  cidrs: [203.0.113.0/24]
in_cluster:
  cidrs: [10.4.0.0/24]
  ports: ["4244", "6443", "7472", "10250"]
`

// liveListing is a real `nft list table` of the golden ruleset (nft 1.1.3); the
// edited one has 198.51.100.1-198.51.100.6, 203.0.113.7 and 2001:db8::/32
// added to mgmt by hand, and port 23, which nft merged into 22-23.
func liveListing(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(data)
}

func fieldsByName(o Observation) map[string]FieldObservation {
	out := map[string]FieldObservation{}
	for _, f := range o.Fields {
		out[f.Field] = f
	}
	return out
}

func TestObserveDrift_UnchangedHostHasEqualFields(t *testing.T) {
	m, _ := observeManager(t, liveListing(t, "live-listing.golden.txt"), true, nil)
	writeFile(t, m.configPath, goldenConfig)

	got := m.ObserveDrift(context.Background())

	assert.Equal(t, []FieldObservation{
		{Field: "configFile.mgmt.cidrs", Config: []string{"10.0.0.0/8", "192.168.0.0/16"}, Live: []string{"10.0.0.0/8", "192.168.0.0/16"}},
		{Field: "configFile.mgmt.ports", Config: []string{"22"}, Live: []string{"22"}},
		{Field: "configFile.blocked.cidrs", Config: []string{"203.0.113.0/24"}, Live: []string{"203.0.113.0/24"}},
		{Field: "configFile.in_cluster.cidrs", Config: []string{"10.4.0.0/24"}, Live: []string{"10.4.0.0/24"}},
		{Field: "configFile.in_cluster.ports", Config: []string{"4244", "6443", "7472", "10250"}, Live: []string{"4244", "6443", "7472", "10250"}},
	}, got.Fields)
}

func TestObserveDrift_HandEditsShowOnTheLiveSide(t *testing.T) {
	m, _ := observeManager(t, liveListing(t, "live-listing-edited.txt"), true, nil)
	writeFile(t, m.configPath, goldenConfig)

	fields := fieldsByName(m.ObserveDrift(context.Background()))

	// The hand-added range comes back as the fewest prefixes, IPv6 in the same
	// field, and the merged 22-23 as one range.
	assert.Equal(t, []string{
		"10.0.0.0/8", "192.168.0.0/16",
		"198.51.100.1/32", "198.51.100.2/31", "198.51.100.4/31", "198.51.100.6/32",
		"203.0.113.7/32", "2001:db8::/32",
	}, fields["configFile.mgmt.cidrs"].Live)
	assert.Equal(t, []string{"22-23"}, fields["configFile.mgmt.ports"].Live)
	assert.Equal(t, fields["configFile.blocked.cidrs"].Config, fields["configFile.blocked.cidrs"].Live)
}

func TestObserveDrift_NormalizesBothSidesAlike(t *testing.T) {
	// What the config writes as separate entries nft merges; both sides must
	// read the same once normalized.
	m, _ := observeManager(t, liveListing(t, "live-listing-edited.txt"), true, nil)
	writeFile(t, m.configPath, `version: 1
mgmt:
  cidrs: [10.0.0.0/8, 192.168.0.0/16, 198.51.100.1/32, 198.51.100.2/32, 198.51.100.3/32, 198.51.100.4/31, 198.51.100.6/32, 203.0.113.7/32, "2001:db8::/32"]
  ports: ["23", "22"]
blocked:
  cidrs: [203.0.113.0/24]
in_cluster:
  cidrs: [10.4.0.0/24]
  ports: ["4244", "6443", "7472", "10250"]
`)

	for _, f := range m.ObserveDrift(context.Background()).Fields {
		assert.Equal(t, f.Config, f.Live, f.Field)
	}
}

func TestObserveDrift_NamesExpandFromTheDNSCache(t *testing.T) {
	m, _ := observeManager(t, liveListing(t, "live-listing-edited.txt"), true, nil)
	writeFile(t, m.configPath, `version: 1
mgmt:
  cidrs: [10.0.0.0/8, 192.168.0.0/16, bastion.example.com, gone.example.com]
  ports: ["22"]
blocked:
  cidrs: [203.0.113.0/24]
in_cluster:
  cidrs: [10.4.0.0/24]
  ports: ["4244", "6443", "7472", "10250"]
`)
	// gone.example.com has no cache entry: it was never applied, so nothing
	// live stands for it and it shows as the config's own.
	writeFile(t, m.dnsCachePath, `{"bastion.example.com": {"addresses": {"203.0.113.7": "2026-11-19T00:00:00Z"}}}`)

	got := fieldsByName(m.ObserveDrift(context.Background()))["configFile.mgmt.cidrs"]

	assert.Equal(t, []string{"10.0.0.0/8", "192.168.0.0/16", "203.0.113.7/32", "gone.example.com"}, got.Config)
}

func TestObserveDrift_WithoutAReadableCacheNamesAreNotCompared(t *testing.T) {
	// A lost cache must not make every name look never applied.
	m, _ := observeManager(t, liveListing(t, "live-listing-allow.golden.txt"), true, nil)
	writeFile(t, m.configPath, strings.Replace(allowConfig,
		"cidrs: [10.0.0.0/24]\n    ports: [\"8472\"]", "cidrs: [10.0.0.0/24, partner.example.com]\n    ports: [\"8472\"]", 1))

	fields := fieldsByName(m.ObserveDrift(context.Background()))

	assert.NotContains(t, fields, "configFile.allow[cilium-vxlan].cidrs")
	assert.Contains(t, fields, "configFile.allow[cilium-vxlan].ports")
	assert.Contains(t, fields, "configFile.mgmt.cidrs")
}

func TestObserveDrift_ConfigRuleNamedLikeALiveRulesV6Set(t *testing.T) {
	// The YAML renames rule web to web6. web's v6 set is called web6, so web6
	// must not read as live, and web must still show as live only.
	m, _ := observeManager(t, liveListing(t, "live-listing-rule-web.txt"), true, nil)
	writeFile(t, m.configPath, goldenConfig+"allow:\n  - name: web6\n    cidrs: [\"2001:db8::/64\"]\n")

	got := m.ObserveDrift(context.Background())

	assert.Equal(t, []RuleObservation{
		{Name: "web", Live: true},
		{Name: "web6", InConfig: true},
	}, got.Rules)
}

func TestObserveDrift_LiveRuleNamedLikeAConfigRulesPortsSet(t *testing.T) {
	m, _ := observeManager(t, liveListing(t, "live-listing-rule-x_ports.txt"), true, nil)
	writeFile(t, m.configPath, goldenConfig+"allow:\n  - name: x\n    cidrs: [192.0.2.0/24]\n    ports: [\"443\"]\n")

	got := m.ObserveDrift(context.Background())

	assert.Equal(t, []RuleObservation{
		{Name: "x", InConfig: true},
		{Name: "x_ports", Live: true},
	}, got.Rules)
}

func TestObserveDrift_UnsetInClusterCIDRsAreNotCompared(t *testing.T) {
	// Left out, the apply detects the pod CIDR itself, so the config has no
	// expected value.
	m, _ := observeManager(t, liveListing(t, "live-listing.golden.txt"), true, nil)
	writeFile(t, m.configPath, `version: 1
mgmt:
  cidrs: [10.0.0.0/8, 192.168.0.0/16]
blocked:
  cidrs: [203.0.113.0/24]
in_cluster:
  ports: ["4244", "6443", "7472", "10250"]
`)

	fields := fieldsByName(m.ObserveDrift(context.Background()))

	assert.NotContains(t, fields, "configFile.in_cluster.cidrs")
	assert.Contains(t, fields, "configFile.in_cluster.ports")
}

func TestObserveDrift_NoContentWithoutTheYAMLConfig(t *testing.T) {
	// The rendered ruleset alone still expects a table, but it is not the
	// baseline for content.
	logs := captureLogs(t)
	m, _ := observeManager(t, liveListing(t, "live-listing.golden.txt"), true, nil)
	writeFile(t, m.nftPath, "table inet weaver-host-firewall {\n}\n")

	got := m.ObserveDrift(context.Background())

	assert.True(t, got.ConfigPresent)
	assert.Empty(t, got.Fields)
	assert.Contains(t, logs.String(), "host firewall content not compared: no YAML config, only the rendered ruleset")
}

func TestObserveDrift_UnparseableConfigKeepsPresence(t *testing.T) {
	logs := captureLogs(t)
	m, _ := observeManager(t, liveListing(t, "live-listing.golden.txt"), true, nil)
	writeFile(t, m.configPath, "mgmt: [not, a, block\n")

	got := m.ObserveDrift(context.Background())

	assert.Equal(t, Observation{TablePresent: true, ConfigPresent: true, ConfigUnreadable: true}, got)
	// The WARN names the file to fix.
	assert.Regexp(t, `"level":"warn".*"path":"`+regexp.QuoteMeta(m.configPath)+`".*config does not parse`, logs.String())
}

// withSet returns the golden listing with set name's elements replaced by elements,
// written as nft prints them.
func withSet(t *testing.T, name, elements string) string {
	t.Helper()
	listing := liveListing(t, "live-listing.golden.txt")
	header := "\tset " + name + " {\n"
	start := strings.Index(listing, header)
	require.NotEqual(t, -1, start, name)
	end := start + strings.Index(listing[start:], "\n\t}\n")
	body := listing[start:end]
	if i := strings.Index(body, "\t\telements = "); i != -1 {
		body = body[:i-1]
	}
	return listing[:start] + body + "\n\t\telements = { " + elements + " }" + listing[end:]
}

// mgmtConfig is goldenConfig with mgmt's address list replaced by cidrs.
func mgmtConfig(cidrs string) string {
	return strings.Replace(goldenConfig, "cidrs: [10.0.0.0/8, 192.168.0.0/16]", "cidrs: ["+cidrs+"]", 1)
}

func TestObserveDrift_MappedRangeEndOnAHealthyHost(t *testing.T) {
	// A valid config whose two v6 entries nft merges and prints with an
	// IPv4-mapped top end (nft 1.0.2, 1.0.9, 1.1.3).
	m, _ := observeManager(t, withSet(t, "mgmt_addrs6", "::fffd:ffff:ffff-::ffff:255.255.255.255"), true, nil)
	writeFile(t, m.configPath, mgmtConfig(`10.0.0.0/8, 192.168.0.0/16, "::fffd:ffff:ffff/128", "::fffe:0:0/95"`))

	got := fieldsByName(m.ObserveDrift(context.Background()))["configFile.mgmt.cidrs"]

	assert.Equal(t, got.Config, got.Live)
}

func TestObserveDrift_RangeAcrossFamiliesIsReadNotFatal(t *testing.T) {
	m, _ := observeManager(t, withSet(t, "mgmt_addrs6", "::ffff:255.255.255.0-::1:0:0:0"), true, nil)
	writeFile(t, m.configPath, goldenConfig)

	got := fieldsByName(m.ObserveDrift(context.Background()))["configFile.mgmt.cidrs"]

	assert.NotEqual(t, got.Config, got.Live)
}

func TestObserveDrift_MappedRangeInTheV6SetIsNotHiddenByAV4Entry(t *testing.T) {
	m, _ := observeManager(t, withSet(t, "mgmt_addrs6", "::ffff:10.0.0.1-::ffff:10.0.0.3"), true, nil)
	writeFile(t, m.configPath, goldenConfig)

	got := fieldsByName(m.ObserveDrift(context.Background()))["configFile.mgmt.cidrs"]

	assert.NotEqual(t, got.Config, got.Live)
}

func TestObserveDrift_ElementCommentsDoNotCutTheList(t *testing.T) {
	// As nft 1.1.3 prints an element added with a comment.
	m, _ := observeManager(t, withSet(t, "blocked_addrs",
		"10.9.9.9,\n\t\t\t     198.51.100.7 comment \"x, y }\",\n\t\t\t     203.0.113.0/24"), true, nil)
	writeFile(t, m.configPath, goldenConfig)

	got := fieldsByName(m.ObserveDrift(context.Background()))["configFile.blocked.cidrs"]

	assert.Equal(t, []string{"10.9.9.9/32", "198.51.100.7/32", "203.0.113.0/24"}, got.Live)
}

// allowConfig is the YAML the allow golden ruleset renders from.
const allowConfig = `version: 1
mgmt:
  cidrs: [10.0.0.0/8, "2001:db8:a11::/48"]
  ports: ["22"]
blocked:
  cidrs: [203.0.113.0/24, "2001:db8:bad::/48"]
in_cluster:
  cidrs: [10.4.0.0/24, "2001:db8:c0de::/64"]
  ports: ["4244", "6443", "7472", "10250"]
allow:
  - name: admin
    cidrs: [203.0.113.5/32, "2001:db8:5e5::/64"]
    ports: ["22"]
    icmp_echo: true
  - name: cilium-vxlan
    cidrs: [10.0.0.0/24]
    ports: ["8472"]
    proto: udp
  - name: k8s-node
    cidrs: [10.0.0.0/24]
    ports: ["6443", "2379-2380", "10250", "10256-10259"]
`

// withoutSets drops the named sets from a listing, as if removed by hand.
func withoutSets(t *testing.T, listing string, names ...string) string {
	t.Helper()
	for _, name := range names {
		start := strings.Index(listing, "\tset "+name+" {\n")
		require.NotEqual(t, -1, start, name)
		end := start + strings.Index(listing[start:], "\n\t}\n") + len("\n\t}\n")
		listing = listing[:start] + listing[end:]
	}
	return listing
}

func TestObserveDrift_AllowRulesComparedByName(t *testing.T) {
	m, _ := observeManager(t, liveListing(t, "live-listing-allow.golden.txt"), true, nil)
	writeFile(t, m.configPath, allowConfig)

	got := m.ObserveDrift(context.Background())

	assert.Equal(t, []RuleObservation{
		{Name: "admin", InConfig: true, Live: true},
		{Name: "cilium-vxlan", InConfig: true, Live: true},
		{Name: "k8s-node", InConfig: true, Live: true},
	}, got.Rules)
	fields := fieldsByName(got)
	assert.Equal(t, []string{"203.0.113.5/32", "2001:db8:5e5::/64"}, fields["configFile.allow[admin].cidrs"].Live)
	assert.Equal(t, []string{"2379-2380", "6443", "10250", "10256-10259"}, fields["configFile.allow[k8s-node].ports"].Live)
	for _, f := range got.Fields {
		assert.Equal(t, f.Config, f.Live, f.Field)
	}
}

func TestObserveDrift_AllowRuleRemovedByHand(t *testing.T) {
	listing := withoutSets(t, liveListing(t, "live-listing-allow.golden.txt"), "admin", "admin6", "admin_ports")
	m, _ := observeManager(t, listing, true, nil)
	writeFile(t, m.configPath, allowConfig)

	got := m.ObserveDrift(context.Background())

	assert.Contains(t, got.Rules, RuleObservation{Name: "admin", InConfig: true, Live: false})
	assert.NotContains(t, fieldsByName(got), "configFile.allow[admin].cidrs")
	assert.NotContains(t, fieldsByName(got), "configFile.allow[admin].ports")
}

func TestObserveDrift_AllowRuleDeletedFromTheConfigButLive(t *testing.T) {
	m, _ := observeManager(t, liveListing(t, "live-listing-allow.golden.txt"), true, nil)
	config, _, found := strings.Cut(allowConfig, "  - name: admin")
	require.True(t, found)
	_, rest, _ := strings.Cut(allowConfig, "    icmp_echo: true\n")
	writeFile(t, m.configPath, config+rest)

	got := m.ObserveDrift(context.Background())

	assert.Equal(t, []RuleObservation{
		{Name: "admin", InConfig: false, Live: true},
		{Name: "cilium-vxlan", InConfig: true, Live: true},
		{Name: "k8s-node", InConfig: true, Live: true},
	}, got.Rules)
}

// holdLock takes the apply lock at path from another descriptor until the test ends.
func holdLock(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	require.NoError(t, err)
	require.NoError(t, syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	t.Cleanup(func() { _ = f.Close() })
}

func TestObserveDrift_HeldApplyLockIsUnobserved(t *testing.T) {
	// A mutation in progress: what the read would see is half applied.
	m, _ := observeManager(t, liveListing(t, "live-listing.golden.txt"), true, nil)
	writeFile(t, m.configPath, goldenConfig)
	holdLock(t, m.lockPath)
	m.lockWait = 100 * time.Millisecond

	got := m.ObserveDrift(context.Background())

	assert.Equal(t, Observation{Unobserved: true, ConfigPresent: true}, got)
}

func TestObserveDrift_FreeApplyLockIsTakenAndReleased(t *testing.T) {
	m, _ := observeManager(t, liveListing(t, "live-listing.golden.txt"), true, nil)
	writeFile(t, m.configPath, goldenConfig)
	require.NoError(t, os.MkdirAll(filepath.Dir(m.lockPath), 0o755))
	writeFile(t, m.lockPath, "")

	got := m.ObserveDrift(context.Background())

	assert.False(t, got.Unobserved)
	holdLock(t, m.lockPath) // released: another holder can take it
}

func TestObserveDrift_LogsWhatItReadInFull(t *testing.T) {
	// The drift lines are capped; the debug log is where the rest is.
	logs := captureLogs(t)
	m, _ := observeManager(t, liveListing(t, "live-listing-edited.txt"), true, nil)
	writeFile(t, m.configPath, goldenConfig)

	m.ObserveDrift(context.Background())

	out := logs.String()
	assert.Contains(t, out, `"component":"firewall","field":"configFile.mgmt.cidrs","persisted":"10.0.0.0/8, 192.168.0.0/16","live":"10.0.0.0/8, 192.168.0.0/16, 198.51.100.1/32, 198.51.100.2/31, 198.51.100.4/31, 198.51.100.6/32, 203.0.113.7/32, 2001:db8::/32"`)
	assert.Contains(t, out, `"field":"configFile","persisted":"present","live":"present"`)
}

func TestObserveDrift_FailedReadIsAWarningWhenATableIsExpected(t *testing.T) {
	logs := captureLogs(t)
	m, _ := observeManager(t, "", false, errorx.ExternalError.New("nft list table failed"))
	writeFile(t, m.configPath, goldenConfig)

	m.ObserveDrift(context.Background())

	assert.Regexp(t, `"level":"warn","error":"[^"]*nft list table failed","component":"firewall"`, logs.String())
}

func TestLockOpenError_NamesMissingRootRights(t *testing.T) {
	// The lock file is root's, so a non-root read fails here before nft.
	err := lockOpenError("/run/solo-provisioner/network/.applying", &fs.PathError{Op: "open", Err: fs.ErrPermission})

	assert.Contains(t, err.Error(), "needs root")
}

// configNotCompared lists the firewall config keys drift detection leaves out,
// with the reason.
var configNotCompared = map[string]string{
	"version":         "the file format version, not firewall content",
	"blocked.ports":   "always empty: Validate rejects ports on the block list",
	"allow.proto":     "not read back: the protocol lives in rule bodies, which are not compared",
	"allow.icmp_echo": "not read back: echo rules live in chain bodies, which are not compared",
}

// configKeys returns the dotted yaml key of every leaf of a config type, a
// list of rules counting as one block.
func configKeys(t reflect.Type, prefix string) []string {
	var keys []string
	for i := range t.NumField() {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if name == "-" || name == "" {
			continue
		}
		if prefix != "" {
			name = prefix + "." + name
		}
		ft := f.Type
		for ft.Kind() == reflect.Pointer || ft.Kind() == reflect.Slice {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct {
			keys = append(keys, configKeys(ft, name)...)
			continue
		}
		keys = append(keys, name)
	}
	return keys
}

// Fails when a key is added to the firewall config without deciding whether
// drift detection compares it. What is compared is taken from a read of a
// config that sets every key.
func TestObserveDrift_EveryConfigKeyIsComparedOrExplicitlyExcluded(t *testing.T) {
	m, _ := observeManager(t, liveListing(t, "live-listing-allow.golden.txt"), true, nil)
	writeFile(t, m.configPath, allowConfig)
	got := m.ObserveDrift(context.Background())

	compared := map[string]bool{}
	for _, f := range got.Fields {
		key := strings.TrimPrefix(f.Field, "configFile.")
		if before, after, ok := strings.Cut(key, "["); ok {
			_, rest, _ := strings.Cut(after, "]")
			key = before + rest
		}
		compared[key] = true
	}
	if len(got.Rules) > 0 {
		compared["allow.name"] = true
	}

	for _, key := range configKeys(reflect.TypeFor[FileConfig](), "") {
		_, excluded := configNotCompared[key]
		require.Truef(t, compared[key] != excluded,
			"firewall config key %q must be either compared by ObserveDrift or listed in configNotCompared", key)
	}
}

func TestObserveDrift_WritesNothing(t *testing.T) {
	// The diagnose commands will call this read, so it must leave the host as
	// it found it: no lock directory, no cache, no config.
	m, dir := observeManager(t, "table inet weaver-host-firewall {\n}\n", true, nil)
	writeFile(t, m.configPath, "version: 1\n")
	before := listDir(t, dir)

	m.ObserveDrift(context.Background())

	assert.Equal(t, before, listDir(t, dir))
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	var names []string
	require.NoError(t, filepath.WalkDir(dir, func(path string, _ os.DirEntry, err error) error {
		names = append(names, path)
		return err
	}))
	return names
}
