// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"strconv"
	"strings"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/ui/prompt"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

// Host-firewall flag names, shared by every block-node command that can apply
// the node-level host firewall (install, reconfigure, upgrade). Kept in one
// place so the flag registration and the resolver never drift.
const (
	FlagNameFirewallEnabled = "firewall-enabled"
	FlagNameMgmtCIDRs       = "mgmt-cidrs"
	FlagNameBlockedCIDRs    = "blocked-cidrs"
	FlagNameMgmtPorts       = "mgmt-ports"
	FlagNamePodCIDR         = "pod-cidr"
	FlagNameInClusterPorts  = "in-cluster-ports"
)

// ValidateHostFirewallFlags validates format-sensitive host-firewall flags that
// are only caught in ResolveHostFirewallConfig — which runs after the interactive
// wizard. Call this at the top of RunE, before prepareBlocknodeInputs, so
// operators get immediate feedback for invalid CLI inputs.
//
// Only flags explicitly Changed on the CLI are checked; un-set flags fall back
// to config / built-in defaults and are validated later inside
// ResolveHostFirewallConfig as always.
func ValidateHostFirewallFlags(cmd *cobra.Command) error {
	if cmd.Flags().Changed(FlagNameMgmtPorts) {
		ports, _ := cmd.Flags().GetIntSlice(FlagNameMgmtPorts)
		for _, p := range ports {
			if err := sanity.ValidatePort(strconv.Itoa(p)); err != nil {
				return errorx.IllegalArgument.Wrap(err, "invalid --%s %d", FlagNameMgmtPorts, p)
			}
		}
	}
	if cmd.Flags().Changed(FlagNameInClusterPorts) {
		ports, _ := cmd.Flags().GetIntSlice(FlagNameInClusterPorts)
		for _, p := range ports {
			if err := sanity.ValidatePort(strconv.Itoa(p)); err != nil {
				return errorx.IllegalArgument.Wrap(err, "invalid --%s %d", FlagNameInClusterPorts, p)
			}
		}
	}
	if cmd.Flags().Changed(FlagNameMgmtCIDRs) {
		cidrs, _ := cmd.Flags().GetStringSlice(FlagNameMgmtCIDRs)
		for _, cidr := range normalizeCIDRs(cidrs) {
			// The management allowlist is the one list that also takes domain
			// names; every other CIDR flag here stays literal-only.
			if err := sanity.ValidateMgmtEntry(cidr); err != nil {
				return errorx.IllegalArgument.Wrap(err, "invalid --%s %q", FlagNameMgmtCIDRs, cidr)
			}
		}
	}
	if cmd.Flags().Changed(FlagNameBlockedCIDRs) {
		cidrs, _ := cmd.Flags().GetStringSlice(FlagNameBlockedCIDRs)
		for _, cidr := range normalizeCIDRs(cidrs) {
			if err := sanity.ValidateIPv4CIDR(cidr); err != nil {
				return errorx.IllegalArgument.Wrap(err, "invalid --%s %q", FlagNameBlockedCIDRs, cidr)
			}
		}
	}
	if cmd.Flags().Changed(FlagNamePodCIDR) {
		podCIDR, _ := cmd.Flags().GetString(FlagNamePodCIDR)
		if err := sanity.ValidateIPv4CIDR(strings.TrimSpace(podCIDR)); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid --%s %q", FlagNamePodCIDR, podCIDR)
		}
	}
	return nil
}

// RegisterHostFirewallFlags registers the node-level host firewall flags on cmd.
// The values are read back by name in ResolveHostFirewallConfig (no bound vars),
// so the same registration works for any command that provisions a host.
func RegisterHostFirewallFlags(cmd *cobra.Command) {
	cmd.Flags().Bool(FlagNameFirewallEnabled, false,
		"Apply the node-level host firewall (inet weaver-host-firewall table: SSH/mgmt allowlist, ICMP policy, in-cluster ports). "+
			"Opt-in (default: false) so existing non-interactive callers are unaffected; enable explicitly for "+
			"hosts you want this tool to manage the firewall on.")
	cmd.Flags().StringSlice(FlagNameMgmtCIDRs, nil,
		"SSH/management allowlist for the node host firewall: IPv4 CIDRs and/or FQDNs (comma-separated or repeated). "+
			"An FQDN is resolved to its A records and re-resolved on a timer. Empty skips the host firewall.")
	cmd.Flags().StringSlice(FlagNameBlockedCIDRs, nil,
		"Operator-curated block list CIDRs for the node host firewall, dropped before any other rule including "+
			"established connections (comma-separated or repeated). Distinct from the BN workload plane's "+
			"bn-restricted set, which the traffic-shaper daemon manages automatically.")
	cmd.Flags().IntSlice(FlagNameMgmtPorts, []int{firewall.DefaultSSHPort},
		"SSH/management TCP port(s) allowed from --mgmt-cidrs by the node host firewall (comma-separated or repeated)")
	cmd.Flags().String(FlagNamePodCIDR, models.DefaultClusterPodCIDR,
		"Pod CIDR allowed to reach the in-cluster host-service ports (defaults to the cluster pod subnet)")
	cmd.Flags().IntSlice(FlagNameInClusterPorts, firewall.DefaultInClusterPorts,
		"Host-service ports reachable from the pod CIDR by the node host firewall (comma-separated)")
}

// hostFirewallFeature describes the host firewall as a gated network feature for
// resolveFeatureGate: the --firewall-enabled opt-in gate plus the content flags
// that are meaningless without it.
func hostFirewallFeature() gatedFeature {
	return gatedFeature{
		GateFlag:    FlagNameFirewallEnabled,
		Noun:        "the host firewall",
		PromptTitle: "Enable host firewall?",
		PromptDesc: "Apply the node-level inet weaver-host-firewall firewall (SSH/mgmt allowlist, ICMP policy, in-cluster ports). " +
			"Opt-in, default No — choose Yes to have this tool manage the host firewall.",
		ContentFlags: []string{FlagNameMgmtCIDRs, FlagNameBlockedCIDRs, FlagNameMgmtPorts, FlagNamePodCIDR, FlagNameInClusterPorts},
	}
}

// ResolveHostFirewallConfig determines the effective host firewall configuration
// (enabled, management CIDR allowlist, SSH port, pod CIDR, in-cluster
// host-service ports) and applies it to the global config so the
// NetworkFirewallCreate step (wired into the block-node install/reconfigure/
// upgrade workflows) can render the inet weaver-host-firewall table. Precedence per value:
// CLI flag > interactive prompt > config file > live host firewall > persisted
// state (MachineState.Firewall) > built-in default. When the
// session is interactive, any value not supplied on the CLI is presented as a
// pre-filled prompt the operator can confirm with Enter. An empty management
// allowlist is allowed — the step then skips firewall creation rather than
// rendering a lock-out (default-drop) ruleset.
//
// When cv is non-nil the prompted values are recorded into it and no separate
// summary is printed — the caller is responsible for printing the unified
// summary after all prompt sections complete. When cv is nil a local collector
// is used and printed as "Host Firewall" immediately.
//
// seedEnabled is the default the enable/disable choice falls back to when neither
// the flag nor an interactive prompt decides it. `install` passes false (opt-in —
// a fresh install without the flag installs no firewall), while `reconfigure`
// passes ResolveFirewallSeed's answer, so a no-flag / default-accept reconfigure
// keeps the last-chosen state rather than silently tearing an established
// firewall down. It is intentionally NOT derived from cfg.Disabled: config.yaml's
// zero value cannot distinguish "enabled" from "never configured".
//
// It requires RegisterHostFirewallFlags to have been called on cmd.
func ResolveHostFirewallConfig(cmd *cobra.Command, args []string, cv *prompt.ChosenValues, seedEnabled bool) error {
	force, err := FlagForce().Value(cmd, args)
	if err != nil {
		return errorx.IllegalArgument.Wrap(err, "failed to get %s flag", FlagForce().Name)
	}

	cfg := config.Get().Host

	// Resolve the enable/disable gate through the shared gated-feature resolver
	// (flag > confirm-prompt > seed, plus the content-flag-without-gate guard),
	// identical to how traffic shaping is gated.
	firewallEnabled, err := resolveFeatureGate(cmd, args, hostFirewallFeature(), seedEnabled)
	if err != nil {
		return err
	}

	// An explicit opt-out skips resolving/prompting for the allowlist/port
	// fields entirely — there's nothing to ask once the firewall itself is
	// disabled. Other fields are preserved (not wiped) so re-enabling later
	// doesn't require re-entering them.
	if !firewallEnabled {
		hostCfg := cfg
		hostCfg.Disabled = true
		config.OverrideHostConfig(hostCfg)
		return nil
	}

	// Fall back to the last-persisted firewall allowlist for any field the operator
	// did not supply via --config, so a reconfigure that re-enables the firewall
	// without re-passing --mgmt-cidrs restores the last-known-good allowlist instead
	// of skipping with the SSH-lockout guard (issue #932). A CLI flag, checked inside
	// the effective* helpers below, still wins over both tiers. On a fresh host with
	// neither a live firewall nor persisted state this is a no-op.
	//
	// The live firewall is consulted first because it is always at least as fresh as
	// machine state: every path that writes machine state also re-renders the
	// firewall, but the standalone `network firewall` verbs write only the firewall.
	// Without this tier a reconfigure's force re-render would revert an urgent
	// `network firewall add --name mgmt --cidr …` back to the allowlist recorded at
	// install time (issue #1003).
	cfg = mergeLiveHostFirewall(cmd.Context(), cfg)
	cfg = mergeHostFirewallFromState(cfg)

	// Seed each prompt target with the effective value: the CLI flag when the
	// operator set it, else the config value, else the built-in default. The
	// prompt layer skips any flag already set on the CLI, leaving these seeds
	// intact, so the same strings are parsed whether or not a prompt ran.
	mgmtStr := effectiveCSV(cmd, FlagNameMgmtCIDRs, cfg.ManagementCIDRs)
	blockedStr := effectiveCSV(cmd, FlagNameBlockedCIDRs, cfg.BlockedCIDRs)
	mgmtPortsStr := effectiveIntCSV(cmd, FlagNameMgmtPorts, cfg.MgmtPorts, []int{firewall.DefaultSSHPort})
	portsStr := effectiveIntCSV(cmd, FlagNameInClusterPorts, cfg.InClusterPorts, firewall.DefaultInClusterPorts)
	podStr := effectiveStr(cmd, FlagNamePodCIDR, cfg.PodCIDR, models.DefaultClusterPodCIDR)

	if prompt.ShouldPrompt(force) {
		localCV := cv
		if localCV == nil {
			localCV = prompt.NewChosenValues()
		}
		if err := prompt.RunInputPrompts(cmd, []prompt.InputPrompt{
			prompt.MgmtCIDRsInputPrompt(mgmtStr, &mgmtStr),
			prompt.BlockedCIDRsInputPrompt(blockedStr, &blockedStr),
			prompt.MgmtPortsInputPrompt(mgmtPortsStr, &mgmtPortsStr),
			prompt.PodCIDRInputPrompt(podStr, &podStr),
			prompt.InClusterPortsInputPrompt(portsStr, &portsStr),
		}, localCV); err != nil {
			return err
		}
		if cv == nil {
			localCV.Print("Host Firewall")
		}
	}

	mgmtPorts, err := prompt.ParsePortList(mgmtPortsStr)
	if err != nil {
		return errorx.IllegalArgument.Wrap(err, "invalid --%s", FlagNameMgmtPorts)
	}
	ports, err := prompt.ParsePortList(portsStr)
	if err != nil {
		return errorx.IllegalArgument.Wrap(err, "invalid --%s", FlagNameInClusterPorts)
	}

	hostCfg := models.HostConfig{
		ManagementCIDRs: normalizeCIDRs(strings.Split(mgmtStr, ",")),
		BlockedCIDRs:    normalizeCIDRs(strings.Split(blockedStr, ",")),
		MgmtPorts:       mgmtPorts,
		PodCIDR:         strings.TrimSpace(podStr),
		InClusterPorts:  ports,
		Disabled:        false,
	}
	if err := hostCfg.Validate(); err != nil {
		return errorx.IllegalArgument.Wrap(err, "invalid host firewall configuration")
	}
	config.OverrideHostConfig(hostCfg)
	return nil
}

// effectiveCSV returns the effective comma-joined value for a StringSlice flag:
// the flag when explicitly set, else the config value.
func effectiveCSV(cmd *cobra.Command, name string, cfgVal []string) string {
	if cmd.Flags().Changed(name) {
		v, _ := cmd.Flags().GetStringSlice(name)
		return strings.Join(v, ",")
	}
	return strings.Join(cfgVal, ",")
}

// effectiveStr returns the effective value for a string flag: the flag when set,
// else the config value when non-empty, else the built-in default.
func effectiveStr(cmd *cobra.Command, name, cfgVal, def string) string {
	if cmd.Flags().Changed(name) {
		v, _ := cmd.Flags().GetString(name)
		return v
	}
	if cfgVal != "" {
		return cfgVal
	}
	return def
}

// effectiveIntCSV returns the effective comma-joined value for an IntSlice flag:
// the flag when set, else the config value when non-empty, else the default.
func effectiveIntCSV(cmd *cobra.Command, name string, cfgVal, def []int) string {
	switch {
	case cmd.Flags().Changed(name):
		v, _ := cmd.Flags().GetIntSlice(name)
		return joinInts(v)
	case len(cfgVal) > 0:
		return joinInts(cfgVal)
	default:
		return joinInts(def)
	}
}

func joinInts(in []int) string {
	parts := make([]string, len(in))
	for i, n := range in {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}

// newHostFirewallManager is the seam over the production firewall manager so
// unit tests can substitute one wired to a fake nft runner and temp paths (the
// production manager probes the live kernel, which is Linux-only).
var newHostFirewallManager = func() *firewall.Manager { return firewall.NewManager() }

// ResolveFirewallSeed answers "did this host want a host firewall?" for
// reconfigure's enable/disable seed — the value used when neither
// --firewall-enabled nor an interactive prompt decides it.
//
// persisted is the block node's recorded decision (MachineState.Firewall), nil
// when nothing was ever recorded. Nil is NOT "disabled": until issue #1003 the
// standalone `network firewall` verbs wrote no state at all, so a firewall
// created with `network firewall create` looked identical to a host that never
// had one — and a no-flag reconfigure resolved that to disabled and deleted it.
//
// So a live inet weaver-host-firewall table always seeds enabled, whatever state
// records. Removing an active host firewall is then only reachable through an
// explicit --firewall-enabled=false or an interactive decline, never as the
// default outcome of an unrelated reconfigure. The converse is left alone: when
// no table is live the recorded decision stands, so a reconfigure still
// re-asserts a firewall that state says should be there.
func ResolveFirewallSeed(ctx context.Context, persisted *models.HostConfig) bool {
	recorded := persisted != nil && !persisted.Disabled
	if recorded {
		return true
	}

	active, err := newHostFirewallManager().IsActive(ctx)
	if err != nil {
		// Not fatal: an unreadable probe just means we fall back to what state
		// recorded, which is the pre-#1003 behaviour.
		logx.As().Debug().Err(err).Msg("could not probe the live host firewall; seeding from persisted state only")
		return recorded
	}
	if active {
		logx.As().Info().Msg(
			"a host firewall (inet weaver-host-firewall) is active but the block node has no record of enabling it; " +
				"keeping it enabled — pass --firewall-enabled=false to remove it deliberately")
		return true
	}
	return recorded
}

// mergeLiveHostFirewall fills any host-firewall content field left empty by the
// config file with the corresponding value from the firewall's own persisted
// table (/etc/solo-provisioner/network-weaver-host-firewall.yaml). It is the
// "live firewall" tier of the flag > config > live > state > default precedence.
// Returns cfg unchanged when no firewall is configured on this host.
//
// Only the three reserved blocks map onto models.HostConfig; named allow rules
// have no field there and are instead carried across inside NetworkFirewallCreate,
// which re-reads them from the same table.
func mergeLiveHostFirewall(ctx context.Context, cfg models.HostConfig) models.HostConfig {
	t, err := newHostFirewallManager().Table(ctx)
	if err != nil {
		logx.As().Debug().Err(err).Msg("no live host firewall to seed from; falling back to persisted state")
		return cfg
	}
	live := hostConfigFromTable(t)
	return applyPersistedFirewallContent(cfg, &live)
}

// hostConfigFromTable projects a firewall Table's reserved blocks onto the
// flag-shaped models.HostConfig.
//
// The projection is narrower than the table in three places, and each warns
// rather than dropping silently: a Rule's CIDR list may mix address families
// while HostConfig is IPv4-only (HostConfig.Validate rejects an IPv6 CIDR
// outright, which would turn a reconfigure into a hard error); HostConfig.PodCIDR
// is a single string where InCluster.CIDRs is a list; and the port fields are
// []int where a Rule holds port specs, which may be inclusive ranges
// ("2379-2380"). A value that cannot be carried is left out, so the tier below
// (persisted state, then the built-in default) supplies that field.
func hostConfigFromTable(t *firewall.Table) models.HostConfig {
	cfg := models.HostConfig{
		ManagementCIDRs: ipv4OrFQDN(t.Mgmt.CIDRs, ruleDescMgmt),
		BlockedCIDRs:    ipv4Only(t.Blocked.CIDRs, ruleDescBlocked),
		InClusterPorts:  plainPorts(t.InCluster.Ports, ruleDescInCluster),
	}
	if mgmtPorts := plainPorts(t.Mgmt.Ports, ruleDescMgmt); len(mgmtPorts) > 0 {
		cfg.MgmtPorts = mgmtPorts
	}
	if pod := ipv4Only(t.InCluster.CIDRs, ruleDescInCluster); len(pod) > 0 {
		cfg.PodCIDR = pod[0]
		if len(pod) > 1 {
			logx.As().Warn().Strs("cidrs", pod).Msg(
				"the live host firewall's in-cluster block holds several pod CIDRs but --pod-cidr carries only one; " +
					"only the first is seeded — pass --pod-cidr, or re-apply the full set with " +
					"`network firewall set --name in_cluster --cidrs`, if the rest still apply")
		}
	}
	return cfg
}

// Rule descriptions naming the offending block in the projection warnings.
const (
	ruleDescMgmt      = "management"
	ruleDescBlocked   = "block-list"
	ruleDescInCluster = "in-cluster"
)

// ipv4OrFQDN is ipv4Only for the management block, which additionally carries
// domain names.
//
// It exists because ipv4Only would drop them, and dropping them here is not a
// failure to seed — it is data loss. The seeded list flows into
// ResolveHostFirewallConfig, then into NetworkFirewallCreate, which assigns it
// over t.Mgmt.CIDRs and persists the result: one `block node reconfigure` would
// rewrite the operator's names out of the source of truth. An all-FQDN list
// fares worse still, seeding empty and making the firewall step skip entirely.
func ipv4OrFQDN(cidrs []string, desc string) []string {
	var out, skipped []string
	for _, c := range cidrs {
		if err := sanity.ValidateMgmtEntry(strings.TrimSpace(c)); err != nil {
			skipped = append(skipped, c)
			continue
		}
		out = append(out, c)
	}
	if len(skipped) > 0 {
		logx.As().Warn().Strs("cidrs", skipped).Msg(
			"the live host firewall's " + desc + " block holds entries the flag-shaped config cannot express " +
				"(IPv6 addresses); they are not seeded and a re-render would drop them — re-apply them afterwards " +
				"with `network firewall set --name mgmt --cidrs ...`")
	}
	return out
}

// ipv4Only keeps the CIDRs models.HostConfig can hold and warns about the rest.
// The flag-shaped config validates every CIDR as IPv4, so carrying an IPv6
// member across would fail HostConfig.Validate and abort the whole reconfigure —
// the one outcome worse than not seeding the value at all.
func ipv4Only(cidrs []string, desc string) []string {
	var out, skipped []string
	for _, c := range cidrs {
		if err := sanity.ValidateIPv4CIDR(strings.TrimSpace(c)); err != nil {
			skipped = append(skipped, c)
			continue
		}
		out = append(out, c)
	}
	if len(skipped) > 0 {
		logx.As().Warn().Strs("cidrs", skipped).Msg(
			"the live host firewall's " + desc + " block holds non-IPv4 addresses, which the flag-shaped config " +
				"cannot express; they are not seeded and a re-render would drop them — re-apply them afterwards " +
				"with `network firewall set --name <rule> --cidrs ...`")
	}
	return out
}

// plainPorts keeps the port specs expressible as a single int and warns about
// any it had to leave behind, so an inclusive range authored through
// `network firewall set --ports` is never silently lost when the flag-shaped
// config is re-rendered.
func plainPorts(specs []string, desc string) []int {
	var out []int
	var skipped []string
	for _, spec := range specs {
		p, err := strconv.Atoi(strings.TrimSpace(spec))
		if err != nil {
			skipped = append(skipped, spec)
			continue
		}
		out = append(out, p)
	}
	if len(skipped) > 0 {
		logx.As().Warn().Strs("ports", skipped).Msg(
			"the live host firewall's " + desc + " block holds port ranges, which the flag-shaped config cannot " +
				"express; they are not seeded and a re-render would drop them — re-apply them afterwards with " +
				"`network firewall set --ports`")
	}
	return out
}

// mergeHostFirewallFromState fills any host-firewall content field left empty by
// the config file with the last value persisted in state (machineState.firewall).
// It is the "state" tier of the flag > config > live firewall > state > default
// precedence for the firewall allowlist: the returned config seeds the effective*
// helpers, where a CLI flag still takes priority. Only the allowlist content is merged — the
// enable/disable decision is resolved separately (flag / prompt / persisted
// state), so the persisted Firewall.Disabled is intentionally ignored here. Returns cfg
// unchanged when no firewall was ever persisted or the state read fails.
func mergeHostFirewallFromState(cfg models.HostConfig) models.HostConfig {
	defaults, err := state.ReadPromptDefaultsFromDisk()
	if err != nil {
		logx.As().Debug().Err(err).Msg("could not read persisted firewall config; not seeding from state")
		return cfg
	}
	return applyPersistedFirewallContent(cfg, defaults.Firewall)
}

// applyPersistedFirewallContent fills any empty host-firewall content field in cfg
// with the corresponding value from the persisted firewall fw, leaving fields the
// operator already supplied (via --config) untouched — this is what enforces the
// config > state precedence. The enable/disable decision (fw.Disabled) is
// deliberately not touched here; callers resolve it separately. Returns cfg
// unchanged when fw is nil.
func applyPersistedFirewallContent(cfg models.HostConfig, fw *models.HostConfig) models.HostConfig {
	if fw == nil {
		return cfg
	}
	if len(cfg.ManagementCIDRs) == 0 {
		cfg.ManagementCIDRs = fw.ManagementCIDRs
	}
	if len(cfg.BlockedCIDRs) == 0 {
		cfg.BlockedCIDRs = fw.BlockedCIDRs
	}
	if len(cfg.MgmtPorts) == 0 {
		cfg.MgmtPorts = fw.MgmtPorts
	}
	if cfg.PodCIDR == "" {
		cfg.PodCIDR = fw.PodCIDR
	}
	if len(cfg.InClusterPorts) == 0 {
		cfg.InClusterPorts = fw.InClusterPorts
	}
	return cfg
}

// SeedHostFirewallFromState seeds the global host-firewall config from the
// last-persisted state (machineState.firewall) for commands — namely `block node
// upgrade` — that re-assert the firewall without prompting or exposing firewall
// flags. An operator-supplied --config firewall section always wins (config >
// state, issue #932 AC4): the config file's values are kept, and only the fields
// it left empty are filled from persisted state.
//
// The enable/disable decision is resolved to honor config first: when the config
// file specified any firewall content, its (config-authored) decision stands;
// otherwise the persisted decision governs — when the firewall was enabled, the
// recorded allowlist re-asserts (NetworkFirewallCreate is create-if-missing) and
// when it was disabled, config.Host.Disabled makes the step skip (upgrade never
// tears the firewall down — allowTeardown=false). It is a no-op when no firewall
// was ever persisted and no config was supplied, leaving the empty default that
// makes the step skip.
func SeedHostFirewallFromState() error {
	defaults, err := state.ReadPromptDefaultsFromDisk()
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to read persisted firewall config")
	}
	if defaults.Firewall == nil {
		logx.As().Debug().Msg("no persisted host-firewall config; leaving firewall unconfigured for this run")
		return nil
	}

	// Detect whether the operator authored a firewall section in --config before
	// merging state in. HostConfig.Disabled's zero value can't distinguish
	// "enabled" from "never configured", so the presence of any content field is
	// what tells us the config file is authoritative for the enable/disable
	// decision (same reasoning as ResolveHostFirewallConfig's seedEnabled).
	cfg := config.Get().Host
	configHasContent := len(cfg.ManagementCIDRs) > 0 ||
		len(cfg.BlockedCIDRs) > 0 ||
		len(cfg.MgmtPorts) > 0 ||
		cfg.PodCIDR != "" ||
		len(cfg.InClusterPorts) > 0

	cfg = applyPersistedFirewallContent(cfg, defaults.Firewall)

	// Only the persisted enable/disable decision fills in when config said nothing
	// about the firewall; an operator who configured it via --config keeps their
	// own decision.
	if !configHasContent {
		cfg.Disabled = defaults.Firewall.Disabled
	}

	config.OverrideHostConfig(cfg)
	logx.As().Debug().
		Bool("disabled", cfg.Disabled).
		Bool("configHasContent", configHasContent).
		Int("managementCidrs", len(cfg.ManagementCIDRs)).
		Msg("Seeded host-firewall config from persisted state")
	return nil
}

// normalizeCIDRs trims whitespace and drops empty entries from a CIDR list so
// a trailing comma or blank prompt entry does not produce an invalid "" CIDR.
func normalizeCIDRs(in []string) []string {
	out := make([]string, 0, len(in))
	for _, c := range in {
		c = strings.TrimSpace(c)
		if c != "" {
			out = append(out, c)
		}
	}
	return out
}
