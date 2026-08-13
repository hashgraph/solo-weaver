// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRender_AllowRules(t *testing.T) {
	doc, err := allowTable().Render()
	require.NoError(t, err)

	// Each rule gets its own per-family address sets and one port set. An allow
	// rule's address set is its bare name; only the reserved blocks carry the
	// `_addrs` suffix they shipped with.
	require.Contains(t, doc, "set k8s-node { type ipv4_addr; flags interval; elements = { 10.0.0.0/24 }; }")
	require.Contains(t, doc, "set k8s-node6 { type ipv6_addr; flags interval; }")

	// Port ranges are single elements of an interval set, ordered by lower bound
	// rather than lexically (10250 after 6443).
	require.Contains(t, doc, "set k8s-node_ports { type inet_service; flags interval; auto-merge; elements = { 2379-2380, 6443, 10250, 10256-10259 }; }")

	// UDP renders as its own rule; nft has no combined tcp/udp dport match.
	require.Contains(t, doc, "ip saddr @cilium-vxlan udp dport @cilium-vxlan_ports accept")
	require.Contains(t, doc, "ip saddr @k8s-node tcp dport @k8s-node_ports accept")

	// A dual-family source list renders in both chains, against its own family's
	// set each time.
	require.Contains(t, doc, "ip saddr @admin tcp dport @admin_ports accept")
	require.Contains(t, doc, "ip6 saddr @admin6 tcp dport @admin_ports accept")

	// Rules are emitted in name order, so the document does not churn with the
	// order the operator happened to author them in.
	v4 := chainBody(t, doc, "input_ipv4")
	require.Less(t, strings.Index(v4, "@admin "), strings.Index(v4, "@cilium-vxlan "))
	require.Less(t, strings.Index(v4, "@cilium-vxlan "), strings.Index(v4, "@k8s-node "))
}

// TestRender_AllowRuleFamilyScoping pins that a rule whose sources are all one
// family emits no rule in the other family's chain. A dead `ip6 saddr @foo6`
// against an empty set would match nothing, but it would also make the rendered
// document lie about which families a rule covers.
func TestRender_AllowRuleFamilyScoping(t *testing.T) {
	doc, err := allowTable().Render()
	require.NoError(t, err)

	v6 := chainBody(t, doc, "input_ipv6")
	// k8s-node and cilium-vxlan are IPv4-only in the fixture.
	require.NotContains(t, v6, "@k8s-node6")
	require.NotContains(t, v6, "@cilium-vxlan6")
	// admin is dual-family, so it is present.
	require.Contains(t, v6, "@admin6")

	// The sets themselves are always declared for both families, so adding a v6
	// address to an existing rule needs no structural change.
	require.Contains(t, doc, "set k8s-node6 { type ipv6_addr; flags interval; }")
}

// TestRender_ICMPEchoPrecedesRateMeter is the ordering pin the meter inversion
// makes necessary: the meter drops over-budget echo outright, so a named accept
// placed after it would never be reached under a flood — precisely when an
// operator needs their own ping to still work.
func TestRender_ICMPEchoPrecedesRateMeter(t *testing.T) {
	doc, err := allowTable().Render()
	require.NoError(t, err)

	for _, tc := range []struct {
		chain, accept, mgmt, meter string
	}{
		{
			chain:  "input_icmp_ipv4",
			accept: "ip saddr @admin icmp type echo-request accept",
			mgmt:   "ip saddr @mgmt_addrs icmp type {",
			meter:  "icmp type echo-request limit rate over 10/second drop",
		},
		{
			chain:  "input_icmp_ipv6",
			accept: "ip6 saddr @admin6 icmpv6 type echo-request accept",
			mgmt:   "ip6 saddr @mgmt_addrs6 icmpv6 type {",
			meter:  "icmpv6 type echo-request limit rate over 10/second drop",
		},
	} {
		t.Run(tc.chain, func(t *testing.T) {
			body := chainBody(t, doc, tc.chain)
			acceptIdx := strings.Index(body, tc.accept)
			mgmtIdx := strings.Index(body, tc.mgmt)
			meterIdx := strings.Index(body, tc.meter)
			require.Greater(t, acceptIdx, -1, "icmp_echo rule must render an accept in %s", tc.chain)
			require.Greater(t, mgmtIdx, -1)
			require.Greater(t, meterIdx, -1)
			require.Less(t, mgmtIdx, acceptIdx, "the named echo accept must follow the mgmt accept")
			require.Less(t, acceptIdx, meterIdx, "the named echo accept must precede the rate meter")
		})
	}

	// A rule without icmp_echo gets no ICMP accept at all.
	require.NotContains(t, doc, "@k8s-node icmp type")
}

// TestRender_ICMPEchoWithoutPorts covers an echo-only grant: a rule may exist to
// permit ping and nothing else, which renders no transport rule.
func TestRender_ICMPEchoWithoutPorts(t *testing.T) {
	tbl := sampleTable()
	require.NoError(t, tbl.UpsertAllow(Rule{Name: "ping-probe", CIDRs: []string{"198.51.100.7/32"}, ICMPEcho: true}))

	doc, err := tbl.Render()
	require.NoError(t, err)
	require.Contains(t, doc, "ip saddr @ping-probe icmp type echo-request accept")
	require.NotContains(t, doc, "@ping-probe tcp dport")
	// No ports means no port set is declared for it.
	require.NotContains(t, doc, "set ping-probe_ports")
}

func TestTable_ValidateRejects(t *testing.T) {
	cases := map[string]func(*Table) error{
		"reserved name for an allow rule": func(tbl *Table) error {
			return tbl.UpsertAllow(Rule{Name: RuleMgmt, CIDRs: []string{"10.0.0.0/8"}, Ports: []string{"22"}})
		},
		"blocked with ports": func(tbl *Table) error {
			tbl.Blocked.Ports = []string{"22"}
			return tbl.Validate()
		},
		"blocked with proto": func(tbl *Table) error {
			tbl.Blocked.Proto = ProtoTCP
			return tbl.Validate()
		},
		"mgmt with icmp_echo": func(tbl *Table) error {
			tbl.Mgmt.ICMPEcho = true
			return tbl.Validate()
		},
		"mgmt with udp": func(tbl *Table) error {
			tbl.Mgmt.Proto = ProtoUDP
			return tbl.Validate()
		},
		"unknown proto": func(tbl *Table) error {
			return tbl.UpsertAllow(Rule{Name: "x", CIDRs: []string{"10.0.0.0/8"}, Ports: []string{"22"}, Proto: "sctp"})
		},
		"allow rule with no cidrs": func(tbl *Table) error {
			return tbl.UpsertAllow(Rule{Name: "x", Ports: []string{"22"}})
		},
		"allow rule with no ports and no echo": func(tbl *Table) error {
			return tbl.UpsertAllow(Rule{Name: "x", CIDRs: []string{"10.0.0.0/8"}})
		},
		"name with nft metacharacters": func(tbl *Table) error {
			return tbl.UpsertAllow(Rule{Name: "a b", CIDRs: []string{"10.0.0.0/8"}, Ports: []string{"22"}})
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			require.Error(t, mutate(sampleTable()))
		})
	}
}

// TestTable_RejectsSetNameCollision covers the trap in deriving set names by
// suffix: two distinct rule names can claim the same nft set, and nft would
// accept the duplicate declaration and silently merge their membership.
func TestTable_RejectsSetNameCollision(t *testing.T) {
	// "mgmt_addrs" as an allow rule would claim the mgmt block's address set.
	shadowsReserved := sampleTable()
	require.NoError(t, shadowsReserved.UpsertAllow(Rule{Name: "mgmt_addrs", CIDRs: []string{"10.0.0.0/8"}, Ports: []string{"22"}}))
	require.ErrorContains(t, shadowsReserved.Validate(), "derive the nft set name")

	// "k8s6" would claim the v6 set of a rule named "k8s".
	shadowsV6 := sampleTable()
	require.NoError(t, shadowsV6.UpsertAllow(Rule{Name: "k8s", CIDRs: []string{"10.0.0.0/8"}, Ports: []string{"22"}}))
	require.NoError(t, shadowsV6.UpsertAllow(Rule{Name: "k8s6", CIDRs: []string{"10.0.0.0/8"}, Ports: []string{"22"}}))
	require.ErrorContains(t, shadowsV6.Validate(), "derive the nft set name")
}

func TestTable_DeleteRule(t *testing.T) {
	tbl := allowTable()
	require.NoError(t, tbl.DeleteRule("cilium-vxlan"))
	_, ok := tbl.Rule("cilium-vxlan")
	require.False(t, ok)

	require.Error(t, tbl.DeleteRule("cilium-vxlan"), "deleting an absent rule is an error, not a silent no-op")

	// The reserved blocks are structural: deleting mgmt would leave a
	// default-drop input chain with no way in.
	for _, name := range ReservedNames {
		require.ErrorContains(t, tbl.DeleteRule(name), "reserved block")
	}
}

func TestPortSpec(t *testing.T) {
	for _, ok := range []string{"1", "22", "65535", "2379-2380", "10256-10259", "1-65535"} {
		require.NoError(t, validatePortSpec(ok), "%q should be a valid port spec", ok)
	}
	for _, bad := range []string{"", "0", "65536", "-1", "22-", "-22", "2380-2379", "22-70000", "http", "22,23", "2379..2380"} {
		require.Error(t, validatePortSpec(bad), "%q should be rejected", bad)
	}
}

// TestRemovePortsIsExact pins that removal does not split a range. Removing 2379
// from a rule holding 2379-2380 leaves the range intact: an nft range is one set
// element, and silently rewriting it into 2380 would be a surprising way for a
// firewall to change.
func TestRemovePortsIsExact(t *testing.T) {
	r := Rule{Name: "x", CIDRs: []string{"10.0.0.0/8"}, Ports: []string{"2379-2380", "6443"}}
	r.RemovePorts([]string{"2379"})
	require.Equal(t, []string{"2379-2380", "6443"}, r.Ports)

	r.RemovePorts([]string{"2379-2380"})
	require.Equal(t, []string{"6443"}, r.Ports)
}

// mgmtBlockedYAML is the required-block preamble a config must carry before it
// can say anything else: every reserved block has to be stated, so a fixture
// exercising one field still has to write the other blocks down. Callers append
// their own `in_cluster:` section; allReservedYAML closes it off for the cases
// that do not care about in-cluster at all.
const (
	mgmtBlockedYAML = "version: 1\n" +
		"mgmt:\n  cidrs: [\"10.0.0.0/8\"]\n" +
		"blocked:\n  cidrs: []\n"
	allReservedYAML = mgmtBlockedYAML + "in_cluster:\n  cidrs: []\n"
)

func TestConfig_RoundTrip(t *testing.T) {
	first, err := FileConfigFromTable(allowTable()).Marshal()
	require.NoError(t, err)

	cfg, err := ParseConfig(first)
	require.NoError(t, err)
	tbl, err := cfg.Table()
	require.NoError(t, err)

	second, err := FileConfigFromTable(tbl).Marshal()
	require.NoError(t, err)
	require.Equal(t, string(first), string(second), "config→YAML→config must be the identity")

	// And the ruleset the reloaded config renders is byte-identical.
	wantDoc, err := allowTable().Render()
	require.NoError(t, err)
	gotDoc, err := tbl.Render()
	require.NoError(t, err)
	require.Equal(t, wantDoc, gotDoc)
}

// TestConfig_OmittedVsEmptyInClusterCIDRs pins the distinction the in-cluster
// semantics rest on, which is carried by nil-vs-empty on a decoded slice: an
// omitted `cidrs` is auto-detected, while an explicitly empty one renders no
// rule. If the YAML decoder ever stopped distinguishing the two,
// `in_cluster: {cidrs: []}` would silently start auto-detecting the pod CIDR
// again.
func TestConfig_OmittedVsEmptyInClusterCIDRs(t *testing.T) {
	omitted, err := ParseConfig([]byte(mgmtBlockedYAML + "in_cluster:\n  ports: [\"6443\"]\n"))
	require.NoError(t, err)
	require.True(t, omitted.InClusterCIDRsUnset(), "an omitted in_cluster cidrs list must be reported as unset")

	present, err := ParseConfig([]byte(mgmtBlockedYAML + "in_cluster:\n  cidrs: []\n"))
	require.NoError(t, err)
	require.False(t, present.InClusterCIDRsUnset(), "an explicitly empty cidrs list must be reported as set")

	// The explicitly-empty form renders no in-cluster rule; the omitted form
	// leaves the caller to fill the addresses in.
	tbl, err := present.Table()
	require.NoError(t, err)
	doc, err := tbl.Render()
	require.NoError(t, err)
	require.NotContains(t, doc, "tcp dport @in_cluster_ports accept")

	// Ports omitted still means "the stack default set", not "no ports".
	require.Equal(t, PortStrings(DefaultInClusterPorts), tbl.InCluster.Ports)
}

func TestConfig_Rejects(t *testing.T) {
	cases := map[string]string{
		"unknown top-level key": allReservedYAML + "allowed:\n  - name: x\n",
		"unknown rule key":      allReservedYAML + "allow:\n  - name: x\n    cidrs: [\"10.0.0.0/8\"]\n    ports: [\"22\"]\n    protocol: tcp\n",
		"future version":        "version: 99\nmgmt:\n  cidrs: []\nblocked:\n  cidrs: []\nin_cluster:\n  cidrs: []\n",
		"reserved allow name":   allReservedYAML + "allow:\n  - name: mgmt\n    cidrs: [\"10.0.0.0/8\"]\n    ports: [\"22\"]\n",
		"duplicate allow name":  allReservedYAML + "allow:\n  - name: x\n    cidrs: [\"10.0.0.0/8\"]\n    ports: [\"22\"]\n  - name: x\n    cidrs: [\"10.1.0.0/16\"]\n    ports: [\"80\"]\n",
		"bad cidr":              "version: 1\nmgmt:\n  cidrs: [\"10.0.0.0\"]\nblocked:\n  cidrs: []\nin_cluster:\n  cidrs: []\n",
		"bad port range":        allReservedYAML + "allow:\n  - name: x\n    cidrs: [\"10.0.0.0/8\"]\n    ports: [\"2380-2379\"]\n",
		"blocked with ports":    "version: 1\nmgmt:\n  cidrs: [\"10.0.0.0/8\"]\nblocked:\n  cidrs: []\n  ports: [\"22\"]\nin_cluster:\n  cidrs: []\n",

		// The reserved blocks are structural: a file that omits one is refused
		// rather than quietly rendered against a weaver default the operator never
		// wrote down. mgmt is the dangerous one — its default is an empty
		// allowlist under a default-drop input chain — but all three are required
		// so the file alone tells you the whole posture.
		"missing mgmt block":      "version: 1\nblocked:\n  cidrs: []\nin_cluster:\n  cidrs: []\n",
		"missing blocked block":   "version: 1\nmgmt:\n  cidrs: [\"10.0.0.0/8\"]\nin_cluster:\n  cidrs: []\n",
		"missing in_cluster":      mgmtBlockedYAML,
		"empty file":              "version: 1\n",
		"null mgmt block":         "version: 1\nmgmt:\nblocked:\n  cidrs: []\nin_cluster:\n  cidrs: []\n",
		"mgmt without cidrs":      "version: 1\nmgmt:\n  ports: [\"22\"]\nblocked:\n  cidrs: []\nin_cluster:\n  cidrs: []\n",
		"blocked without cidrs":   "version: 1\nmgmt:\n  cidrs: [\"10.0.0.0/8\"]\nblocked: {}\nin_cluster:\n  cidrs: []\n",
		"allow-only partial file": "version: 1\nallow:\n  - name: x\n    cidrs: [\"10.0.0.0/8\"]\n    ports: [\"22\"]\n",
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseConfig([]byte(doc))
			require.Error(t, err)
		})
	}
}

// TestConfig_MissingVersionIsAccepted keeps a hand-written file that forgot
// `version:` working, while a version this build does not know is still refused
// (covered above) — ignoring a field a newer weaver understands could leave the
// host with a firewall that does not match the file. `version` is the one
// top-level key that may be omitted; the reserved blocks may not.
func TestConfig_MissingVersionIsAccepted(t *testing.T) {
	cfg, err := ParseConfig([]byte("mgmt:\n  cidrs: [\"10.0.0.0/8\"]\nblocked:\n  cidrs: []\nin_cluster:\n  cidrs: []\n"))
	require.NoError(t, err)
	tbl, err := cfg.Table()
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.0/8"}, tbl.Mgmt.CIDRs)
}

// TestManager_ApplyIsDeclarativeForAllowOnly pins that an allow rule absent from
// an applied config is removed. The reserved blocks cannot go missing from a file
// at all — they are required — so the only thing a config can leave to a default
// is a field inside a block it stated, which is checked here for in_cluster's
// port list.
func TestManager_ApplyIsDeclarativeForAllowOnly(t *testing.T) {
	r := &fakeRunner{}
	applyCount := 0
	m, nftPath := newTestManager(t, r, &applyCount)
	ctx := context.Background()

	require.NoError(t, m.Apply(ctx, allowTable()))
	require.Contains(t, readNft(t, nftPath), "@cilium-vxlan")

	// A config naming only one allow rule drops the others.
	cfg, err := ParseConfig([]byte(
		"version: 1\n" +
			"mgmt:\n  cidrs: [\"10.0.0.0/8\"]\n  ports: [\"22\"]\n" +
			"blocked:\n  cidrs: []\n" +
			"in_cluster:\n  cidrs: [\"10.4.0.0/14\"]\n" +
			"allow:\n  - name: k8s-node\n    cidrs: [\"10.0.0.0/24\"]\n    ports: [\"6443\"]\n"))
	require.NoError(t, err)
	tbl, err := cfg.Table()
	require.NoError(t, err)
	require.NoError(t, m.Apply(ctx, tbl))

	doc := readNft(t, nftPath)
	require.Contains(t, doc, "@k8s-node")
	require.NotContains(t, doc, "@cilium-vxlan")
	require.NotContains(t, doc, "@admin")

	// in_cluster's ports were omitted inside a block that was stated, so they came
	// back as the default port set rather than as nothing.
	require.Contains(t, doc, "set in_cluster_ports { type inet_service; flags interval; auto-merge; elements = { 4244, 6443, 7472, 10250 }; }")
}

// TestManager_ConfigRoundTripsThroughDisk is the operator-facing round-trip:
// `show --output yaml` piped back into `create --from-file` changes nothing.
func TestManager_ConfigRoundTripsThroughDisk(t *testing.T) {
	r := &fakeRunner{}
	applyCount := 0
	m, nftPath := newTestManager(t, r, &applyCount)
	ctx := context.Background()

	require.NoError(t, m.Apply(ctx, allowTable()))
	firstDoc := readNft(t, nftPath)

	shown, err := m.Config(ctx)
	require.NoError(t, err)
	data, err := shown.Marshal()
	require.NoError(t, err)

	reloaded, err := ParseConfig(data)
	require.NoError(t, err)
	tbl, err := reloaded.Table()
	require.NoError(t, err)
	require.NoError(t, m.Apply(ctx, tbl))

	require.Equal(t, firstDoc, readNft(t, nftPath), "re-applying the shown config must be a no-op")
}

// TestManager_LoadsFromLegacyNftWhenConfigMissing covers the upgrade path: a host
// provisioned before the config file existed must still be mutable, recovering
// its management allowlist from the rendered ruleset rather than erroring out.
func TestManager_LoadsFromLegacyNftWhenConfigMissing(t *testing.T) {
	r := &fakeRunner{exists: true}
	applyCount := 0
	m, nftPath := newTestManager(t, r, &applyCount)
	ctx := context.Background()

	// The pre-allow-rules rendering: the management port and the pod CIDR were
	// rule literals rather than sets, and neither in_cluster_addrs nor mgmt_ports
	// existed.
	legacy := `add table inet weaver-host-firewall
delete table inet weaver-host-firewall
add table inet weaver-host-firewall
table inet weaver-host-firewall {
	set mgmt_addrs { type ipv4_addr; flags interval; elements = { 192.168.68.0/24 }; }
	set mgmt_addrs6 { type ipv6_addr; flags interval; }
	set blocked_addrs { type ipv4_addr; flags interval; elements = { 203.0.113.0/24 }; }
	set blocked_addrs6 { type ipv6_addr; flags interval; }
	set in_cluster_ports { type inet_service; elements = { 6443 }; }

	chain input_ipv4 {
		ip saddr @mgmt_addrs tcp dport 2222 accept
		ip saddr 10.4.0.0/24 tcp dport @in_cluster_ports accept
	}
}
`
	require.NoError(t, os.WriteFile(nftPath, []byte(legacy), 0o644))

	tbl, err := m.Table(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"192.168.68.0/24"}, tbl.Mgmt.CIDRs)
	require.Equal(t, []string{"2222"}, tbl.Mgmt.Ports, "the literal SSH port must be recovered as a one-element list")
	require.Equal(t, []string{"203.0.113.0/24"}, tbl.Blocked.CIDRs)
	require.Equal(t, []string{"10.4.0.0/24"}, tbl.InCluster.CIDRs, "the literal pod CIDR must be recovered into the set")
	require.Equal(t, []string{"6443"}, tbl.InCluster.Ports)

	// And a mutation now works, writing the config file so the fallback is not
	// needed again.
	require.NoError(t, m.Add(ctx, RuleMgmt, []string{"198.51.100.4/32"}, nil))
	doc := readNft(t, nftPath)
	require.Contains(t, doc, "198.51.100.4/32")
	require.Contains(t, doc, "192.168.68.0/24", "the recovered allowlist must survive the mutation")
	require.Contains(t, doc, "tcp dport @mgmt_ports accept", "the mutation re-renders in the current form")
}

// TestParse_RecoversReservedBlocksOnly states the fallback's limit outright: it
// is not a general nft parser, and named allow rules are not recovered from a
// ruleset. Losing the config file loses the allow rules, which are re-appliable;
// it must never lose management access, which is not.
func TestParse_RecoversReservedBlocksOnly(t *testing.T) {
	doc, err := allowTable().Render()
	require.NoError(t, err)

	parsed, err := Parse(doc)
	require.NoError(t, err)
	require.Equal(t, allowTable().Mgmt.CIDRs, parsed.Mgmt.CIDRs)
	require.Empty(t, parsed.Allow, "allow rules are deliberately not reverse-engineered from the ruleset")
}

func TestManager_DeleteRemovesConfigFile(t *testing.T) {
	r := &fakeRunner{}
	applyCount := 0
	dir := t.TempDir()
	nftPath := filepath.Join(dir, "network-weaver-host-firewall.nft")
	configPath := filepath.Join(dir, "network-weaver-host-firewall.yaml")
	m := NewManagerWithConfig(Config{
		Runner:     r,
		NftPath:    nftPath,
		ConfigPath: configPath,
		LockPath:   filepath.Join(dir, ".applying"),
		ApplyViaService: func(context.Context) error {
			applyCount++
			r.exists = true
			return nil
		},
	})
	ctx := context.Background()

	require.NoError(t, m.Apply(ctx, allowTable()))
	require.FileExists(t, configPath)

	require.NoError(t, m.Delete(ctx))
	require.NoFileExists(t, nftPath)
	require.NoFileExists(t, configPath)
}

func TestRender_AllowGoldenStable(t *testing.T) {
	goldenPath := filepath.Join("testdata", "network-weaver-host-firewall-allow.golden.nft")
	doc, err := allowTable().Render()
	require.NoError(t, err)

	if *update {
		require.NoError(t, os.WriteFile(goldenPath, []byte(doc), 0o644))
	}

	want, err := os.ReadFile(goldenPath)
	require.NoError(t, err)
	require.Equal(t, strings.TrimSpace(string(want)), strings.TrimSpace(doc))
}
