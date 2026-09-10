// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package privexec

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/automa-saga/daemonkit"
	"github.com/stretchr/testify/require"
)

// recordedCall captures one invocation of the exec seam.
type recordedCall struct {
	name string
	args []string
}

// fakeDelegator builds an execDelegator whose seams are fully in-memory: stat
// treats existPaths as present, executable returns exePath, and output records
// the call and returns the canned stdout/err. It never touches the host.
func fakeDelegator(existPaths []string, exePath string, stdout []byte, runErr error) (*execDelegator, *recordedCall) {
	exist := map[string]bool{}
	for _, p := range existPaths {
		exist[p] = true
	}
	call := &recordedCall{}
	d := &execDelegator{
		executable: func() (string, error) {
			if exePath == "" {
				return "", errors.New("no executable")
			}
			return exePath, nil
		},
		stat: func(p string) (os.FileInfo, error) {
			if exist[p] {
				return nil, nil
			}
			return nil, os.ErrNotExist
		},
		output: func(_ context.Context, name string, args ...string) ([]byte, error) {
			call.name = name
			call.args = args
			return stdout, runErr
		},
	}
	return d, call
}

func TestNetworkPolicySet_BuildsSudoArgv(t *testing.T) {
	// CLI resolves to the daemon's sibling; sudo to its first candidate.
	d, call := fakeDelegator(
		[]string{"/usr/bin/sudo", "/opt/solo/weaver/bin/solo-provisioner"},
		"/opt/solo/weaver/bin/solo-provisioner-daemon",
		nil, nil,
	)

	err := d.NetworkPolicySet(context.Background(), "bn-publisher", []string{"10.1.0.2/32", "10.1.0.10/32"})
	require.NoError(t, err)

	require.Equal(t, "/usr/bin/sudo", call.name)
	require.Equal(t, []string{
		"-n",
		"/opt/solo/weaver/bin/solo-provisioner",
		"network", "policy", "set",
		"--name", "bn-publisher",
		"--cidrs", "10.1.0.2/32,10.1.0.10/32",
	}, call.args)
}

func TestNetworkPolicySet_EmptyCIDRsOmitsFlag(t *testing.T) {
	d, call := fakeDelegator(
		[]string{"/usr/bin/sudo", "/opt/solo/weaver/bin/solo-provisioner"},
		"/opt/solo/weaver/bin/solo-provisioner-daemon",
		nil, nil,
	)

	require.NoError(t, d.NetworkPolicySet(context.Background(), "bn-partner", nil))
	require.Equal(t, []string{
		"-n",
		"/opt/solo/weaver/bin/solo-provisioner",
		"network", "policy", "set", "--name", "bn-partner",
	}, call.args)
	require.NotContains(t, call.args, "--cidrs")
}

func TestNetworkPolicySet_EmptyNameIsGuarded(t *testing.T) {
	d, call := fakeDelegator([]string{"/usr/bin/sudo", "/usr/local/bin/solo-provisioner"}, "", nil, nil)

	err := d.NetworkPolicySet(context.Background(), "   ", []string{"10.0.0.1/32"})
	require.Error(t, err)
	var pe *daemonkit.ProbeError
	require.ErrorAs(t, err, &pe)
	require.Equal(t, "PolicyNameEmpty", pe.Reason)
	require.Empty(t, call.name, "exec must not run when the name is invalid")
}

func TestTCAttach_BuildsSudoArgv(t *testing.T) {
	d, call := fakeDelegator(
		[]string{"/usr/bin/sudo", "/opt/solo/weaver/bin/solo-provisioner"},
		"/opt/solo/weaver/bin/solo-provisioner-daemon",
		nil, nil,
	)

	require.NoError(t, d.TCAttach(context.Background(), "lxc1a2b3c"))
	require.Equal(t, "/usr/bin/sudo", call.name)
	require.Equal(t, []string{
		"-n",
		"/opt/solo/weaver/bin/solo-provisioner",
		"block", "node", "tc-attach", "--veth", "lxc1a2b3c",
	}, call.args)
}

func TestTCDetach_AppendsDetachFlag(t *testing.T) {
	d, call := fakeDelegator(
		[]string{"/usr/bin/sudo", "/opt/solo/weaver/bin/solo-provisioner"},
		"/opt/solo/weaver/bin/solo-provisioner-daemon",
		nil, nil,
	)

	require.NoError(t, d.TCDetach(context.Background(), "lxc1a2b3c"))
	require.Equal(t, []string{
		"-n",
		"/opt/solo/weaver/bin/solo-provisioner",
		"block", "node", "tc-attach", "--veth", "lxc1a2b3c", "--detach",
	}, call.args)
}

func TestTCAttach_EmptyVethIsGuarded(t *testing.T) {
	d, call := fakeDelegator([]string{"/usr/bin/sudo", "/usr/local/bin/solo-provisioner"}, "", nil, nil)

	err := d.TCAttach(context.Background(), "  ")
	require.Error(t, err)
	var pe *daemonkit.ProbeError
	require.ErrorAs(t, err, &pe)
	require.Equal(t, "VethNameEmpty", pe.Reason)
	require.Empty(t, call.name, "exec must not run when the veth name is empty")
}

func TestReconcileShaper_BuildsSudoArgv(t *testing.T) {
	d, call := fakeDelegator(
		[]string{"/usr/bin/sudo", "/opt/solo/weaver/bin/solo-provisioner"},
		"/opt/solo/weaver/bin/solo-provisioner-daemon",
		nil, nil,
	)

	require.NoError(t, d.ReconcileShaper(context.Background(), "http://127.0.0.1:8080"))
	require.Equal(t, "/usr/bin/sudo", call.name)
	require.Equal(t, []string{
		"-n",
		"/opt/solo/weaver/bin/solo-provisioner",
		"block", "node", "reconcile-shaper", "--statusz-url", "http://127.0.0.1:8080",
	}, call.args)
}

func TestReconcileShaper_EmptyURLIsGuarded(t *testing.T) {
	d, call := fakeDelegator([]string{"/usr/bin/sudo", "/usr/local/bin/solo-provisioner"}, "", nil, nil)

	err := d.ReconcileShaper(context.Background(), "  ")
	require.Error(t, err)
	var pe *daemonkit.ProbeError
	require.ErrorAs(t, err, &pe)
	require.Equal(t, "StatuszURLEmpty", pe.Reason)
	require.Empty(t, call.name, "exec must not run when the statusz URL is empty")
}

func TestReconcileShaperCheck_BuildsUnprivilegedArgvAndParsesDigest(t *testing.T) {
	// Deliberately omit sudo from the existing paths: the --check probe must
	// resolve and exec the CLI directly, never sudo.
	d, call := fakeDelegator(
		[]string{"/opt/solo/weaver/bin/solo-provisioner"},
		"/opt/solo/weaver/bin/solo-provisioner-daemon",
		[]byte(`{"desired-digest":"abc123","desired":{}}`), nil,
	)

	digest, err := d.ReconcileShaperCheck(context.Background(), "http://127.0.0.1:8080")
	require.NoError(t, err)
	require.Equal(t, "abc123", digest)

	require.Equal(t, "/opt/solo/weaver/bin/solo-provisioner", call.name,
		"the check probe execs the CLI directly, not sudo")
	require.Equal(t, []string{
		"block", "node", "reconcile-shaper",
		"--statusz-url", "http://127.0.0.1:8080",
		"--check", "--output", "json",
	}, call.args)
	require.NotContains(t, call.args, "-n", "no sudo non-interactive flag on the unprivileged path")
}

func TestReconcileShaperCheck_EmptyURLIsGuarded(t *testing.T) {
	d, call := fakeDelegator([]string{"/usr/local/bin/solo-provisioner"}, "", nil, nil)

	_, err := d.ReconcileShaperCheck(context.Background(), "")
	require.Error(t, err)
	var pe *daemonkit.ProbeError
	require.ErrorAs(t, err, &pe)
	require.Equal(t, "StatuszURLEmpty", pe.Reason)
	require.Empty(t, call.name, "exec must not run when the statusz URL is empty")
}

func TestReconcileShaperCheck_BadJSONReportsParseError(t *testing.T) {
	d, _ := fakeDelegator(
		[]string{"/opt/solo/weaver/bin/solo-provisioner"},
		"/opt/solo/weaver/bin/solo-provisioner-daemon",
		[]byte("not json at all"), nil,
	)

	_, err := d.ReconcileShaperCheck(context.Background(), "http://127.0.0.1:8080")
	require.Error(t, err)
	var pe *daemonkit.ProbeError
	require.ErrorAs(t, err, &pe)
	require.Equal(t, "ReconcileShaperCheckParseFailed", pe.Reason)
}

func TestReconcileShaperCheck_EmptyDigestReportsContractError(t *testing.T) {
	// Well-formed JSON but no (or empty) desired-digest — the --check contract
	// drifted. Must fail fast rather than return "".
	d, _ := fakeDelegator(
		[]string{"/opt/solo/weaver/bin/solo-provisioner"},
		"/opt/solo/weaver/bin/solo-provisioner-daemon",
		[]byte(`{"desired":{}}`), nil,
	)

	_, err := d.ReconcileShaperCheck(context.Background(), "http://127.0.0.1:8080")
	require.Error(t, err)
	var pe *daemonkit.ProbeError
	require.ErrorAs(t, err, &pe)
	require.Equal(t, "ReconcileShaperCheckEmptyDigest", pe.Reason)
}

func TestReconcileShaperCheck_ExecFailureReportsProbeError(t *testing.T) {
	exitErr := runFailingCommand(t)
	exitErr.Stderr = []byte("statusz unreachable\n")

	d, _ := fakeDelegator(
		[]string{"/opt/solo/weaver/bin/solo-provisioner"},
		"/opt/solo/weaver/bin/solo-provisioner-daemon",
		nil, exitErr,
	)

	_, err := d.ReconcileShaperCheck(context.Background(), "http://127.0.0.1:8080")
	require.Error(t, err)
	var pe *daemonkit.ProbeError
	require.ErrorAs(t, err, &pe)
	require.Equal(t, "ReconcileShaperCheckFailed", pe.Reason)
	require.Contains(t, pe.Message, "statusz unreachable")
	require.ErrorIs(t, err, exitErr, "underlying exec error must remain unwrappable")
}

func TestResolveCLI_PrefersDaemonSibling(t *testing.T) {
	// Both the sibling and a granted path exist; the sibling wins.
	d, _ := fakeDelegator(
		[]string{"/custom/install/solo-provisioner", "/usr/local/bin/solo-provisioner"},
		"/custom/install/solo-provisioner-daemon",
		nil, nil,
	)
	bin, err := d.resolveCLI()
	require.NoError(t, err)
	require.Equal(t, "/custom/install/solo-provisioner", bin)
}

func TestResolveCLI_FallsBackToGrantedPath(t *testing.T) {
	// No sibling on disk; resolution falls back to the sudoers-granted path.
	d, _ := fakeDelegator(
		[]string{"/usr/local/bin/solo-provisioner"},
		"/custom/install/solo-provisioner-daemon",
		nil, nil,
	)
	bin, err := d.resolveCLI()
	require.NoError(t, err)
	require.Equal(t, "/usr/local/bin/solo-provisioner", bin)
}

func TestRun_CLINotFoundReportsResolution(t *testing.T) {
	// sudo exists but no CLI binary anywhere.
	d, call := fakeDelegator([]string{"/usr/bin/sudo"}, "", nil, nil)

	_, err := d.Run(context.Background(), "network", "policy", "set", "--name", "bn-publisher")
	require.Error(t, err)
	var pe *daemonkit.ProbeError
	require.ErrorAs(t, err, &pe)
	require.Equal(t, "CLIBinaryNotFound", pe.Reason)
	require.Contains(t, pe.Resolution, "reinstall the solo-provisioner CLI")
	require.Empty(t, call.name, "exec must not run when the CLI binary is missing")
}

func TestRun_SudoNotFoundReportsResolution(t *testing.T) {
	d, _ := fakeDelegator([]string{"/opt/solo/weaver/bin/solo-provisioner"}, "", nil, nil)

	_, err := d.Run(context.Background(), "version")
	require.Error(t, err)
	var pe *daemonkit.ProbeError
	require.ErrorAs(t, err, &pe)
	require.Equal(t, "SudoBinaryNotFound", pe.Reason)
}

func TestRun_NonZeroExitMapsToProbeErrorWithStderr(t *testing.T) {
	// A real *exec.ExitError so the Stderr-extraction path is exercised.
	exitErr := runFailingCommand(t)
	exitErr.Stderr = []byte("sudo: a password is required\n")

	d, _ := fakeDelegator(
		[]string{"/usr/bin/sudo", "/opt/solo/weaver/bin/solo-provisioner"},
		"/opt/solo/weaver/bin/solo-provisioner-daemon",
		nil, exitErr,
	)

	_, err := d.Run(context.Background(), "network", "policy", "set", "--name", "bn-publisher")
	require.Error(t, err)
	var pe *daemonkit.ProbeError
	require.ErrorAs(t, err, &pe)
	require.Equal(t, "PrivilegedExecFailed", pe.Reason)
	require.Contains(t, pe.Message, "sudo: a password is required")
	require.Contains(t, pe.Resolution, "/etc/sudoers.d/solo-provisioner")
	require.ErrorIs(t, err, exitErr, "underlying exec error must remain unwrappable")
}

func TestRun_NoArgsIsGuarded(t *testing.T) {
	d, call := fakeDelegator([]string{"/usr/bin/sudo", "/usr/local/bin/solo-provisioner"}, "", nil, nil)
	_, err := d.Run(context.Background())
	require.Error(t, err)
	var pe *daemonkit.ProbeError
	require.ErrorAs(t, err, &pe)
	require.Equal(t, "PrivilegedExecNoArgs", pe.Reason)
	require.Empty(t, call.name)
}

// runFailingCommand runs a command guaranteed to exit non-zero so the test can
// obtain a genuine *exec.ExitError (whose Stderr field it then overrides).
func runFailingCommand(t *testing.T) *exec.ExitError {
	t.Helper()
	err := exec.Command("false").Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	return exitErr
}

func TestExecMessage_BoundsChattyChildStderr(t *testing.T) {
	// The cause leads stderr and must survive truncation.
	exitErr := runFailingCommand(t)
	cause := "Error: common.external_error: fetch statusz statusz/inbound: context deadline exceeded"
	exitErr.Stderr = []byte(cause + "\nUsage:\n" + strings.Repeat("      --some-flag string   help text\n", 100))

	msg := execMessage("sudo /opt/solo/weaver/bin/solo-provisioner",
		[]string{"block", "node", "reconcile-shaper", "--statusz-url", "http://10.0.0.1:40983"}, exitErr)
	require.Contains(t, msg, cause, "the leading cause must survive truncation")
	require.Contains(t, msg, "stderr truncated")
	require.Less(t, len(msg), 1024, "a chatty child must not produce multi-KB messages")
}

func TestTruncateStderr(t *testing.T) {
	require.Equal(t, "short", truncateStderr("short", 512))

	exact := strings.Repeat("a", 512)
	require.Equal(t, exact, truncateStderr(exact, 512), "exact fit stays untouched")

	out := truncateStderr(strings.Repeat("a", 513), 512)
	require.True(t, strings.HasPrefix(out, exact))
	require.Contains(t, out, "[stderr truncated at 512/513 bytes]")

	// A multi-byte rune straddling the limit is dropped whole, never split.
	out = truncateStderr(strings.Repeat("a", 511)+"é", 512)
	require.True(t, utf8.ValidString(out))
	require.True(t, strings.HasPrefix(out, strings.Repeat("a", 511)+" ..."))
	require.Contains(t, out, "[stderr truncated at 511/513 bytes]")

	// Binary (non-UTF-8) stderr has no rune boundary; it cuts at the limit
	// instead of walking back and dropping the whole payload.
	out = truncateStderr(strings.Repeat("\x80", 600), 512)
	require.True(t, strings.HasPrefix(out, strings.Repeat("\x80", 512)))
	require.Contains(t, out, "[stderr truncated at 512/600 bytes]")
}

// fullReassertReport is the shape every happy-path case starts from: the three
// known artifacts, once each. Anything less is rejected as contract drift.
const fullReassertReport = `{"type":"reassert","artifacts":[` +
	`{"artifact":"host-firewall","expected":true,"present":true},` +
	`{"artifact":"workload-policy","expected":true,"present":true},` +
	`{"artifact":"egress-qdisc","expected":true,"present":true}]}`

func TestNetworkReassert_BuildsSudoArgv(t *testing.T) {
	d, call := fakeDelegator(
		[]string{"/usr/bin/sudo", "/opt/solo/weaver/bin/solo-provisioner"},
		"/opt/solo/weaver/bin/solo-provisioner-daemon",
		[]byte(fullReassertReport), nil,
	)

	_, err := d.NetworkReassert(context.Background())
	require.NoError(t, err)

	require.Equal(t, "/usr/bin/sudo", call.name)
	require.Equal(t, []string{
		"-n",
		"/opt/solo/weaver/bin/solo-provisioner",
		"network", "reassert", "--output", "json",
	}, call.args)
}

func TestNetworkReassert_ParsesEveryReportedField(t *testing.T) {
	// One compact line, as the CLI emits it.
	stdout := []byte(`{"type":"reassert","artifacts":[` +
		`{"artifact":"host-firewall","expected":true,"present":false,"reasserted":true,"recovered":true,"detail":"nic=eth0"},` +
		`{"artifact":"workload-policy","skipped":true,"detail":"skipped: an apply is in progress"},` +
		`{"artifact":"egress-qdisc","expected":true,"present":false,"probe_failed":true,"detail":"cannot determine"}]}` + "\n")
	d, _ := fakeDelegator(
		[]string{"/usr/bin/sudo", "/opt/solo/weaver/bin/solo-provisioner"},
		"/opt/solo/weaver/bin/solo-provisioner-daemon", stdout, nil,
	)

	res, err := d.NetworkReassert(context.Background())
	require.NoError(t, err)
	require.Len(t, res.Artifacts, 3)

	require.Equal(t, "host-firewall", res.Artifacts[0].Artifact)
	require.True(t, res.Artifacts[0].Expected)
	require.False(t, res.Artifacts[0].Present)
	require.True(t, res.Artifacts[0].Reasserted)
	require.True(t, res.Artifacts[0].Recovered)
	require.Equal(t, "nic=eth0", res.Artifacts[0].Detail)

	require.True(t, res.Artifacts[1].Skipped)
	require.Equal(t, "skipped: an apply is in progress", res.Artifacts[1].Detail)

	require.True(t, res.Artifacts[2].ProbeFailed)
}

// TestNetworkReassert_SelectsTheTaggedLineAmongLogEvents pins the transport:
// the report is picked by its tag, so log lines merged onto stdout cannot displace it.
func TestNetworkReassert_SelectsTheTaggedLineAmongLogEvents(t *testing.T) {
	stdout := []byte(`{"level":"debug","time":"2026-09-04T10:00:00Z","message":"resolving nft binary"}
{"type":"reassert","artifacts":[{"artifact":"host-firewall","expected":true,"present":true,"reasserted":true,"recovered":true},{"artifact":"workload-policy","expected":true,"present":true},{"artifact":"egress-qdisc","expected":true,"present":true}]}
{"level":"warn","restored":["host-firewall"],"message":"weaver network state was missing and has been re-asserted"}
`)
	d, _ := fakeDelegator(
		[]string{"/usr/bin/sudo", "/opt/solo/weaver/bin/solo-provisioner"},
		"/opt/solo/weaver/bin/solo-provisioner-daemon", stdout, nil,
	)

	res, err := d.NetworkReassert(context.Background())
	require.NoError(t, err)
	require.Len(t, res.Artifacts, 3)
	require.True(t, res.Artifacts[0].Recovered)
}

func TestNetworkReassert_UntaggedReportIsAContractError(t *testing.T) {
	// A well-formed document without the tag is not the report: accepting it
	// would let an unrelated JSON line on stdout stand in for the worker's answer.
	d, _ := fakeDelegator(
		[]string{"/usr/bin/sudo", "/opt/solo/weaver/bin/solo-provisioner"},
		"/opt/solo/weaver/bin/solo-provisioner-daemon",
		[]byte(`{"artifacts":[{"artifact":"host-firewall","expected":true,"present":true}]}`), nil,
	)

	_, err := d.NetworkReassert(context.Background())

	var pe *daemonkit.ProbeError
	require.ErrorAs(t, err, &pe)
	require.Equal(t, "NetworkReassertParseFailed", pe.Reason)
}

func TestNetworkReassert_UnparseableOutputIsAContractError(t *testing.T) {
	d, _ := fakeDelegator(
		[]string{"/usr/bin/sudo", "/opt/solo/weaver/bin/solo-provisioner"},
		"/opt/solo/weaver/bin/solo-provisioner-daemon",
		[]byte("not json"), nil,
	)

	_, err := d.NetworkReassert(context.Background())

	var pe *daemonkit.ProbeError
	require.ErrorAs(t, err, &pe)
	require.Equal(t, "NetworkReassertParseFailed", pe.Reason)
}

// TestNetworkReassert_MalformedArtifactSetIsAContractError pins a reason code per
// malformed shape. Unchecked, a duplicate or a missing name would read as healthy.
func TestNetworkReassert_MalformedArtifactSetIsAContractError(t *testing.T) {
	for name, tc := range map[string]struct {
		artifacts  string
		wantReason string
	}{
		"one artifact missing": {
			`{"artifact":"host-firewall"},{"artifact":"workload-policy"}`,
			"NetworkReassertArtifactCountMismatch",
		},
		"one artifact twice": {
			`{"artifact":"host-firewall"},{"artifact":"host-firewall"},{"artifact":"egress-qdisc"}`,
			"NetworkReassertDuplicateArtifact",
		},
		"an artifact the daemon does not know": {
			`{"artifact":"host-firewall"},{"artifact":"workload-policy"},{"artifact":"ingress-qdisc"}`,
			"NetworkReassertUnknownArtifact",
		},
		"an extra artifact": {
			`{"artifact":"host-firewall"},{"artifact":"workload-policy"},` +
				`{"artifact":"egress-qdisc"},{"artifact":"egress-qdisc"}`,
			"NetworkReassertArtifactCountMismatch",
		},
	} {
		t.Run(name, func(t *testing.T) {
			d, _ := fakeDelegator(
				[]string{"/usr/bin/sudo", "/opt/solo/weaver/bin/solo-provisioner"},
				"/opt/solo/weaver/bin/solo-provisioner-daemon",
				[]byte(`{"type":"reassert","artifacts":[`+tc.artifacts+`]}`), nil,
			)

			_, err := d.NetworkReassert(context.Background())

			var pe *daemonkit.ProbeError
			require.ErrorAs(t, err, &pe)
			require.Equal(t, tc.wantReason, pe.Reason)
		})
	}
}

func TestNetworkReassert_EmptyArtifactListIsAContractError(t *testing.T) {
	// The worker always reports all three artifacts, so an empty list means the
	// output shape drifted. Accepting it would read as "nothing to do" forever.
	d, _ := fakeDelegator(
		[]string{"/usr/bin/sudo", "/opt/solo/weaver/bin/solo-provisioner"},
		"/opt/solo/weaver/bin/solo-provisioner-daemon",
		[]byte(`{"type":"reassert","artifacts":[]}`), nil,
	)

	_, err := d.NetworkReassert(context.Background())

	var pe *daemonkit.ProbeError
	require.ErrorAs(t, err, &pe)
	require.Equal(t, "NetworkReassertEmptyReport", pe.Reason)
}
