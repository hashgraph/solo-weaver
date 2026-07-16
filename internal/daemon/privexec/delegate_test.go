// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package privexec

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"

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
