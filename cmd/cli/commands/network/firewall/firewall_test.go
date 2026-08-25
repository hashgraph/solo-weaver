// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	fw "github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

// captureRunner satisfies the Runner interface without touching the kernel.
// Apply is intentionally absent — live rule application goes through
// applyViaService (file write + service restart), not the Runner.
type captureRunner struct {
	exists bool
}

func (c *captureRunner) List(_ context.Context) (string, error) { return "", nil }
func (c *captureRunner) Delete(_ context.Context) error         { c.exists = false; return nil }
func (c *captureRunner) Exists(_ context.Context) (bool, error) { return c.exists, nil }

// Check accepts every document: these tests exercise the CLI's flag handling,
// not nft's verdict on the rendered ruleset (that lives in internal/network/firewall).
func (c *captureRunner) Check(_ context.Context, _ string) error { return nil }

func TestFirewallCmd_Structure(t *testing.T) {
	cmd := GetCmd()
	require.Equal(t, "firewall", cmd.Use)

	want := map[string]bool{"create": false, "create-allow-rule": false, "add": false, "remove": false, "set": false, "show": false, "delete": false}
	for _, sub := range cmd.Commands() {
		if _, ok := want[sub.Use]; ok {
			want[sub.Use] = true
		}
	}
	for verb, found := range want {
		require.True(t, found, "verb %q not registered under firewall", verb)
	}
}

func TestCreateCmd_Flags(t *testing.T) {
	for _, name := range []string{"mgmt-cidrs", "blocked-cidrs", "in-cluster-ports", "mgmt-ports", "pod-cidr", "from-file"} {
		require.NotNil(t, createCmd.Flags().Lookup(name), "create is missing --%s", name)
	}
	// Defaults must match the firewall package defaults.
	require.Equal(t, "[22]", createCmd.Flags().Lookup("mgmt-ports").DefValue)
	// ICMP is a static ruleset apart from the per-rule icmp_echo grant, which is
	// a config-file field: there must be no icmp toggles here.
	require.Nil(t, createCmd.Flags().Lookup("icmp-mgmt"), "icmp-mgmt flag should be removed")
	require.Nil(t, createCmd.Flags().Lookup("icmp-public"), "icmp-public flag should be removed")
}

// TestVerbs_NameAddressedFlags pins the name-addressed surface added alongside
// the per-block flags.
func TestVerbs_NameAddressedFlags(t *testing.T) {
	for _, tc := range []struct {
		cmd   *cobra.Command
		verb  string
		flags []string
	}{
		{addCmd, "add", []string{"name", "cidr", "port"}},
		{removeCmd, "remove", []string{"name", "cidr", "port"}},
		{setCmd, "set", []string{"name", "cidrs", "cidrs-file", "ports", "proto", "icmp-echo"}},
		{createAllowRuleCmd, "create-allow-rule", []string{"name", "proto", "icmp-echo"}},
		{showCmd, "show", []string{"name", "output"}},
		{deleteCmd, "delete", []string{"name", "all"}},
	} {
		for _, f := range tc.flags {
			require.NotNil(t, tc.cmd.Flags().Lookup(f), "%s is missing --%s", tc.verb, f)
		}
	}
	require.Equal(t, "nft", showCmd.Flags().Lookup("output").DefValue)
}

// stubManager points the CLI at a Manager backed by temp paths and a fake runner,
// returning the artifact paths so a test can assert on what a verb rendered.
func stubManager(t *testing.T) (nftPath, configPath string) {
	t.Helper()
	r := &captureRunner{}
	dir := t.TempDir()
	nftPath = filepath.Join(dir, "network-weaver-host-firewall.nft")
	configPath = filepath.Join(dir, "network-weaver-host-firewall.yaml")

	// The mutating verbs also record the enable decision into machine state
	// (issue #1003), so every stubbed verb needs a state file under t.TempDir()
	// too — otherwise a unit test would write to the host's real state.
	stubStateManager(t)

	origMgr, origDetect := newManager, detectPodCIDR
	newManager = func() *fw.Manager {
		return fw.NewManagerWithConfig(fw.Config{
			Runner:     r,
			NftPath:    nftPath,
			ConfigPath: configPath,
			LockPath:   filepath.Join(dir, ".applying"),
			ApplyViaService: func(context.Context) error {
				r.exists = true
				return nil
			},
		})
	}
	detectPodCIDR = func(context.Context) (string, error) { return "", errors.New("no cluster") }
	t.Cleanup(func() { newManager, detectPodCIDR = origMgr, origDetect })
	return nftPath, configPath
}

// run executes one `firewall …` invocation through a fresh root command, the way
// the real CLI would.
func run(t *testing.T, args ...string) error {
	t.Helper()
	_, err := runOut(t, args...)
	return err
}

// runOut is run() with stdout captured, for the verbs that print.
func runOut(t *testing.T, args ...string) (string, error) {
	t.Helper()
	resetFlagState(t)

	var out bytes.Buffer
	root := &cobra.Command{Use: "test"}
	root.PersistentFlags().Bool("force", false, "force")
	root.AddCommand(GetCmd())
	root.SetArgs(append([]string{"firewall"}, args...))
	root.SetOut(&out)
	root.SetErr(io.Discard)
	err := root.Execute()
	return out.String(), err
}

// resetFlagState clears the flag state left behind by a previous invocation.
// GetCmd returns package-level cobra commands, so pflag's per-flag Changed
// survives from one Execute to the next within a test binary — which would make
// mutual-exclusion checks fire on a flag an earlier test set, and would let a
// value leak into a later verb. A real CLI process runs one command and never
// sees this.
func resetFlagState(t *testing.T) {
	t.Helper()
	for _, sub := range GetCmd().Commands() {
		sub.Flags().VisitAll(func(f *pflag.Flag) { f.Changed = false })
	}
	// The shared binding variables are read directly (not via Changed) in a
	// couple of places, so zero them too.
	flagName, flagCIDRsFile, flagFromFile, flagOutput, flagAll = "", "", "", outputNft, false
	flagCIDRs, flagPorts, flagPodCIDR = nil, nil, nil
	flagMgmtCIDRs, flagBlockedCIDRs, flagInClusterPorts = nil, nil, nil
	flagMgmtCIDR, flagBlockedCIDR = "", ""
	flagInClusterPort, flagMgmtPorts = 0, nil
	flagProto, flagICMPEcho = "", false
}

// TestBackwardCompatibleInvocations is the regression gate the generalisation
// rests on: every `network firewall` invocation that worked before --name existed
// must still behave identically. The per-block flags are shorthands now, but
// nothing about them changed for a caller.
func TestBackwardCompatibleInvocations(t *testing.T) {
	nftPath, _ := stubManager(t)

	require.NoError(t, run(t, "create", "--mgmt-cidrs", "10.0.0.0/8", "--blocked-cidrs", "203.0.113.0/24",
		"--mgmt-ports", "2222", "--in-cluster-ports", "6443,10250", "--pod-cidr", "10.4.0.0/24"))
	doc := readFile(t, nftPath)
	require.Contains(t, doc, "elements = { 10.0.0.0/8 }")
	require.Contains(t, doc, "elements = { 203.0.113.0/24 }")
	require.Contains(t, doc, "set mgmt_ports { type inet_service; flags interval; auto-merge; elements = { 2222 }; }",
		"--mgmt-ports must still work, as a one-element port list")
	require.Contains(t, doc, "elements = { 6443, 10250 }")
	require.Contains(t, doc, "set in_cluster_addrs { type ipv4_addr; flags interval; auto-merge; elements = { 10.4.0.0/24 }; }")

	require.NoError(t, run(t, "add", "--mgmt-cidr", "192.168.1.0/24"))
	require.Contains(t, readFile(t, nftPath), "192.168.1.0/24")

	require.NoError(t, run(t, "add", "--blocked-cidr", "198.51.100.0/24"))
	require.Contains(t, readFile(t, nftPath), "198.51.100.0/24")

	require.NoError(t, run(t, "add", "--in-cluster-port", "9100"))
	require.Contains(t, readFile(t, nftPath), "9100")

	require.NoError(t, run(t, "remove", "--mgmt-cidr", "192.168.1.0/24"))
	require.NotContains(t, readFile(t, nftPath), "192.168.1.0/24")

	require.NoError(t, run(t, "remove", "--in-cluster-port", "9100"))
	require.NotContains(t, readFile(t, nftPath), "9100")

	// The multi-block form of `set`: three reserved blocks replaced in one call,
	// which must stay a single apply.
	require.NoError(t, run(t, "set", "--mgmt-cidrs", "172.16.0.0/12",
		"--blocked-cidrs", "203.0.113.9/32", "--in-cluster-ports", "6443"))
	doc = readFile(t, nftPath)
	require.Contains(t, doc, "172.16.0.0/12")
	require.Contains(t, doc, "203.0.113.9/32")
	require.Contains(t, doc, "set in_cluster_ports { type inet_service; flags interval; auto-merge; elements = { 6443 }; }")
	require.NotContains(t, doc, "10250")

	// Bare `delete` still tears the whole table down. Non-interactive, so the new
	// confirmation prompt does not fire.
	require.NoError(t, run(t, "delete"))
	require.NoFileExists(t, nftPath)
}

// TestCreateCmd_MgmtPortsAcceptsMultipleValues is the point of #1080: --mgmt-ports
// takes more than one port in a single invocation, matching --mgmt-cidrs.
func TestCreateCmd_MgmtPortsAcceptsMultipleValues(t *testing.T) {
	nftPath, _ := stubManager(t)

	require.NoError(t, run(t, "create", "--mgmt-cidrs", "10.0.0.0/8", "--mgmt-ports", "22,2222"))
	doc := readFile(t, nftPath)
	require.Contains(t, doc, "set mgmt_ports { type inet_service; flags interval; auto-merge; elements = { 22, 2222 }; }")
}

// TestCreateAllowRuleCmd is the end-to-end shape #1009 exists to deliver: a
// named allow rule declared, populated and deleted with no config file anywhere.
func TestCreateAllowRuleCmd(t *testing.T) {
	nftPath, _ := stubManager(t)
	require.NoError(t, run(t, "create", "--mgmt-cidrs", "10.0.0.0/8"))

	// Declared, but rendering nothing yet — the operator has opened no access by
	// running only the first half of the sequence.
	require.NoError(t, run(t, "create-allow-rule", "--name", "rudder_server", "--proto", "udp", "--icmp-echo"))
	doc := readFile(t, nftPath)
	require.NotContains(t, doc, "@rudder_server udp dport")

	// One add carries every CIDR and port, so the rule goes live in a single
	// atomic apply rather than one per element.
	require.NoError(t, run(t, "add", "--name", "rudder_server",
		"--cidr", "200.201.203.205/32,10.1.0.0/16", "--port", "5309,8443,9000-9100"))
	doc = readFile(t, nftPath)
	require.Contains(t, doc, "ip saddr @rudder_server udp dport @rudder_server_ports accept",
		"--proto udp must reach the rendered rule")
	require.Contains(t, doc, "ip saddr @rudder_server icmp type echo-request accept",
		"--icmp-echo must reach the rendered rule")
	require.Contains(t, doc, "elements = { 10.1.0.0/16, 200.201.203.205/32 }")
	require.Contains(t, doc, "elements = { 5309, 8443, 9000-9100 }",
		"a port range must survive as one element, and the list must be sorted")

	// Deletion needs no new verb.
	require.NoError(t, run(t, "delete", "--name", "rudder_server"))
	require.NotContains(t, readFile(t, nftPath), "@rudder_server")
}

func TestCreateAllowRuleCmd_Rejections(t *testing.T) {
	stubManager(t)
	require.NoError(t, run(t, "create", "--mgmt-cidrs", "10.0.0.0/8"))

	require.ErrorContains(t, run(t, "create-allow-rule"), "--name is required")

	// A reserved block is not an allow rule. It also already exists, so this must
	// not be mistaken for the create-if-missing no-op path.
	for _, name := range fw.ReservedNames {
		require.ErrorContains(t, run(t, "create-allow-rule", "--name", name), "reserved name", name)
	}

	require.Error(t, run(t, "create-allow-rule", "--name", "bad name"))
	require.Error(t, run(t, "create-allow-rule", "--name", "x", "--proto", "sctp"))
	// Would silently claim the mgmt block's nft set.
	require.ErrorContains(t, run(t, "create-allow-rule", "--name", "mgmt_addrs"), "derive the nft set name")
}

// TestCreateAllowRuleCmd_ForceRedeclares pins create-if-missing, matching the
// table-level `create`.
func TestCreateAllowRuleCmd_ForceRedeclares(t *testing.T) {
	nftPath, _ := stubManager(t)
	require.NoError(t, run(t, "create", "--mgmt-cidrs", "10.0.0.0/8"))
	require.NoError(t, run(t, "create-allow-rule", "--name", "svc"))
	require.NoError(t, run(t, "add", "--name", "svc", "--cidr", "203.0.113.5/32", "--port", "9000"))
	require.Contains(t, readFile(t, nftPath), "ip saddr @svc tcp dport @svc_ports accept")

	// Without --force the existing rule and its membership survive.
	require.NoError(t, run(t, "create-allow-rule", "--name", "svc", "--proto", "udp"))
	require.Contains(t, readFile(t, nftPath), "ip saddr @svc tcp dport @svc_ports accept")

	// With --force the declaration replaces it, membership included.
	require.NoError(t, run(t, "create-allow-rule", "--name", "svc", "--proto", "udp", "--force"))
	require.NotContains(t, readFile(t, nftPath), "@svc tcp dport")
	require.NotContains(t, readFile(t, nftPath), "@svc udp dport")
}

// TestSetCmd_ProtoAndICMPEcho covers the other half of "every Rule field is
// reachable from the CLI": the two fields must be editable after declaration,
// not only settable at declaration time.
func TestSetCmd_ProtoAndICMPEcho(t *testing.T) {
	nftPath, _ := stubManager(t)
	require.NoError(t, run(t, "create", "--mgmt-cidrs", "10.0.0.0/8"))
	require.NoError(t, run(t, "create-allow-rule", "--name", "svc"))
	require.NoError(t, run(t, "add", "--name", "svc", "--cidr", "203.0.113.5/32", "--port", "9000"))

	require.NoError(t, run(t, "set", "--name", "svc", "--proto", "udp", "--icmp-echo"))
	doc := readFile(t, nftPath)
	require.Contains(t, doc, "ip saddr @svc udp dport @svc_ports accept")
	require.Contains(t, doc, "ip saddr @svc icmp type echo-request accept")

	// Revoking echo is expressible.
	require.NoError(t, run(t, "set", "--name", "svc", "--icmp-echo=false"))
	require.NotContains(t, readFile(t, nftPath), "@svc icmp type")

	// The reserved blocks render a fixed shape and reject both — including the
	// proto value that happens to match what they already render.
	require.Error(t, run(t, "set", "--name", "mgmt", "--proto", "udp"))
	require.Error(t, run(t, "set", "--name", "mgmt", "--proto", "tcp"))
	require.Error(t, run(t, "set", "--name", "in_cluster", "--proto", "tcp"))
	require.Error(t, run(t, "set", "--name", "in_cluster", "--icmp-echo"))

	// --name with none of the value flags is still rejected.
	require.ErrorContains(t, run(t, "set", "--name", "svc"), "at least one of")
}

// TestUnknownRuleNameNeverDeclares is the AC that keeps a typo from creating a
// second rule alongside the intended one.
func TestUnknownRuleNameNeverDeclares(t *testing.T) {
	nftPath, _ := stubManager(t)
	require.NoError(t, run(t, "create", "--mgmt-cidrs", "10.0.0.0/8"))
	require.NoError(t, run(t, "create-allow-rule", "--name", "rudder_server"))

	require.ErrorContains(t, run(t, "add", "--name", "rudder_sever", "--cidr", "10.0.0.1/32"), "no rule named")
	require.ErrorContains(t, run(t, "remove", "--name", "rudder_sever", "--cidr", "10.0.0.1/32"), "no rule named")
	require.ErrorContains(t, run(t, "set", "--name", "rudder_sever", "--cidrs", "10.0.0.1/32"), "no rule named")
	require.NotContains(t, readFile(t, nftPath), "rudder_sever")
}

func TestCreateCmd_FromFile(t *testing.T) {
	nftPath, _ := stubManager(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`version: 1
mgmt:
  cidrs: ["192.168.68.0/24"]
  ports: ["22", "1024"]
blocked:
  cidrs: []
in_cluster:
  cidrs: ["10.4.0.0/14"]
  ports: ["4244", "6443"]
allow:
  - name: k8s-node
    cidrs: ["10.0.0.0/24"]
    ports: ["6443", "2379-2380"]
    proto: tcp
  - name: cilium-vxlan
    cidrs: ["10.0.0.0/24"]
    ports: ["8472"]
    proto: udp
  - name: admin
    cidrs: ["203.0.113.5/32"]
    ports: ["22"]
    icmp_echo: true
`), 0o600))

	require.NoError(t, run(t, "create", "--from-file", path))
	doc := readFile(t, nftPath)
	require.Contains(t, doc, "set mgmt_ports { type inet_service; flags interval; auto-merge; elements = { 22, 1024 }; }")
	require.Contains(t, doc, "ip saddr @k8s-node tcp dport @k8s-node_ports accept")
	require.Contains(t, doc, "ip saddr @cilium-vxlan udp dport @cilium-vxlan_ports accept")
	require.Contains(t, doc, "ip saddr @admin icmp type echo-request accept")
	require.Contains(t, doc, "elements = { 2379-2380, 6443 }")

	// --from-file and the individual flags are mutually exclusive: a file states
	// the whole table, so the precedence between the two would be guesswork.
	require.Error(t, run(t, "create", "--from-file", path, "--mgmt-cidrs", "10.0.0.0/8"))
}

// TestCreateCmd_FromFileRequiresReservedBlocks is the operator-facing half of the
// required-block rule: a file that omits mgmt must fail before anything is
// rendered, rather than applying a default-drop table with an empty management
// allowlist and reporting success.
func TestCreateCmd_FromFileRequiresReservedBlocks(t *testing.T) {
	nftPath, _ := stubManager(t)
	dir := t.TempDir()

	partial := filepath.Join(dir, "partial.yaml")
	require.NoError(t, os.WriteFile(partial, []byte(`version: 1
blocked:
  cidrs: []
in_cluster:
  cidrs: []
allow:
  - name: k8s-node
    cidrs: ["10.0.0.0/24"]
    ports: ["6443"]
`), 0o600))

	err := run(t, "create", "--from-file", partial)
	require.Error(t, err)
	require.Contains(t, err.Error(), "mgmt")
	require.NoFileExists(t, nftPath, "a rejected config must not render a ruleset")
}

func TestShowCmd_YAMLRoundTrips(t *testing.T) {
	nftPath, _ := stubManager(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`version: 1
mgmt:
  cidrs: ["192.168.68.0/24"]
  ports: ["22"]
blocked:
  cidrs: []
in_cluster:
  cidrs: []
allow:
  - name: k8s-node
    cidrs: ["10.0.0.0/24"]
    ports: ["6443", "2379-2380"]
`), 0o600))
	require.NoError(t, run(t, "create", "--from-file", path))
	firstDoc := readFile(t, nftPath)

	shown, err := runOut(t, "show", "--output", "yaml")
	require.NoError(t, err)
	require.Contains(t, shown, "version: 1")
	require.Contains(t, shown, "name: k8s-node")

	// Feeding the shown config back in changes nothing — the acceptance criterion
	// that makes `show --output yaml` safe to keep in version control.
	back := filepath.Join(dir, "shown.yaml")
	require.NoError(t, os.WriteFile(back, []byte(shown), 0o600))
	require.NoError(t, run(t, "create", "--from-file", back, "--force"))
	require.Equal(t, firstDoc, readFile(t, nftPath))

	// An explicitly empty in_cluster block survives the round-trip as empty
	// rather than reverting to the auto-detected pod CIDR.
	require.NotContains(t, readFile(t, nftPath), "tcp dport @in_cluster_ports accept")

	// --name narrows to one rule.
	one, err := runOut(t, "show", "--name", "k8s-node")
	require.NoError(t, err)
	require.Contains(t, one, "name: k8s-node")
	require.NotContains(t, one, "mgmt")

	_, err = runOut(t, "show", "--name", "nope")
	require.Error(t, err)

	_, err = runOut(t, "show", "--output", "json")
	require.Error(t, err, "--output must reject a format it does not render")
}

func TestDeleteCmd_ByName(t *testing.T) {
	nftPath, _ := stubManager(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`version: 1
mgmt:
  cidrs: ["192.168.68.0/24"]
blocked:
  cidrs: []
in_cluster:
  cidrs: []
allow:
  - name: k8s-node
    cidrs: ["10.0.0.0/24"]
    ports: ["6443"]
`), 0o600))
	require.NoError(t, run(t, "create", "--from-file", path))
	require.Contains(t, readFile(t, nftPath), "@k8s-node")

	require.NoError(t, run(t, "delete", "--name", "k8s-node"))
	require.NotContains(t, readFile(t, nftPath), "@k8s-node")
	require.FileExists(t, nftPath, "deleting one rule must not tear the table down")

	// The reserved blocks are structural and cannot be deleted individually.
	require.Error(t, run(t, "delete", "--name", "mgmt"))
	require.Error(t, run(t, "delete", "--name", "k8s-node", "--all"))
}

func TestElementVerbs_RequireATarget(t *testing.T) {
	stubManager(t)
	require.NoError(t, run(t, "create", "--mgmt-cidrs", "10.0.0.0/8"))

	// --name is required once no per-block shorthand names the rule.
	require.Error(t, run(t, "add", "--cidr", "10.1.0.0/16"))
	require.Error(t, run(t, "remove", "--cidr", "10.1.0.0/16"))
	require.Error(t, run(t, "set", "--cidrs", "10.1.0.0/16"))
	// --name with no values to apply is a no-op worth rejecting.
	require.Error(t, run(t, "add", "--name", "mgmt"))
	require.Error(t, run(t, "set", "--name", "mgmt"))
	// Mixing the two forms leaves it ambiguous which rule --cidr belongs to.
	require.Error(t, run(t, "add", "--mgmt-cidr", "10.1.0.0/16", "--name", "blocked"))
	require.Error(t, run(t, "set", "--mgmt-cidrs", "10.1.0.0/16", "--name", "blocked", "--cidrs", "10.2.0.0/16"))
	// --cidrs and --cidrs-file are alternatives, not a merge.
	require.Error(t, run(t, "set", "--name", "mgmt", "--cidrs", "10.1.0.0/16", "--cidrs-file", "/nonexistent"))
}

func TestSetCmd_CIDRsFile(t *testing.T) {
	nftPath, _ := stubManager(t)
	require.NoError(t, run(t, "create", "--mgmt-cidrs", "10.0.0.0/8"))

	dir := t.TempDir()
	path := filepath.Join(dir, "cidrs.txt")
	// The flat list format `network policy --cidrs-file` already uses: newlines
	// and/or commas, with `#` comments.
	require.NoError(t, os.WriteFile(path, []byte("# management\n192.168.68.0/24\n10.9.0.0/16, 172.16.0.0/12\n"), 0o600))

	require.NoError(t, run(t, "set", "--name", "mgmt", "--cidrs-file", path))
	doc := readFile(t, nftPath)
	require.Contains(t, doc, "elements = { 10.9.0.0/16, 172.16.0.0/12, 192.168.68.0/24 }")
	require.NotContains(t, doc, "10.0.0.0/8")
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func TestCreateCmd_DefaultsInClusterPortsWhenNotPassed(t *testing.T) {
	// Regression: `create` (even with --force) without --in-cluster-ports must
	// render the stack port set. The flag-binding var is shared with `set`
	// (nil default), which clobbers create's default in the shared variable —
	// so create must source the default from NewTable(), gated on Changed().
	// This executes the real command so the shared-var registration is exercised.
	resetFlagState(t)

	r := &captureRunner{}
	dir := t.TempDir()
	nftPath := filepath.Join(dir, "network-weaver-host-firewall.nft")

	origMgr, origDetect := newManager, detectPodCIDR
	newManager = func() *fw.Manager {
		return fw.NewManagerWithConfig(fw.Config{
			Runner:  r,
			NftPath: nftPath,
			// Without this the config falls back to the production
			// /etc/solo-provisioner path, so the test writes to the real host
			// and only passes as root.
			ConfigPath: filepath.Join(dir, "network-weaver-host-firewall.yaml"),
			LockPath:   filepath.Join(dir, ".applying"),
			ApplyViaService: func(context.Context) error {
				r.exists = true
				return nil
			},
		})
	}
	// No cluster reachable → pod rule omitted; the in_cluster_ports *set* still
	// renders its elements, which is what we assert on.
	detectPodCIDR = func(context.Context) (string, error) { return "", errors.New("no cluster") }
	defer func() { newManager, detectPodCIDR = origMgr, origDetect }()

	root := &cobra.Command{Use: "test"}
	root.PersistentFlags().Bool("force", false, "force")
	root.AddCommand(GetCmd())
	root.SetArgs([]string{"firewall", "create", "--mgmt-cidrs", "10.0.0.0/8"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	require.NoError(t, root.Execute())

	data, err := os.ReadFile(nftPath)
	require.NoError(t, err, "create should have written the nft file")
	require.Contains(t, string(data), "elements = { 4244, 6443, 7472, 10250 }",
		"create without --in-cluster-ports must default to the stack port set")
}

func TestAddRemoveCmd_Flags(t *testing.T) {
	for _, c := range []string{"add", "remove"} {
		var cmd = addCmd
		if c == "remove" {
			cmd = removeCmd
		}
		require.NotNil(t, cmd.Flags().Lookup("mgmt-cidr"), "%s missing --mgmt-cidr", c)
		require.NotNil(t, cmd.Flags().Lookup("blocked-cidr"), "%s missing --blocked-cidr", c)
		require.NotNil(t, cmd.Flags().Lookup("in-cluster-port"), "%s missing --in-cluster-port", c)
	}
}
