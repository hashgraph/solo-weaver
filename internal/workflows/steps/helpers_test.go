package steps

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/internal/core"
)

func sudo(cmd *exec.Cmd) *exec.Cmd {
	if os.Geteuid() == 0 {
		return cmd
	}

	// Prepend sudo to the command
	sudoCmd := exec.Command("sudo", append([]string{cmd.Path}, cmd.Args[1:]...)...)
	sudoCmd.Stdout = cmd.Stdout
	sudoCmd.Stderr = cmd.Stderr
	sudoCmd.Stdin = cmd.Stdin

	return sudoCmd
}

func TestRunCmd_Success(t *testing.T) {
	out, err := runCmd("echo hello")
	require.NoError(t, err)
	require.Equal(t, "hello", out)
}

func TestRunCmd_Error(t *testing.T) {
	_, err := runCmd("exit 42")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to execute bash command")
}

func cleanUpTempDir(t *testing.T) {
	t.Helper()

	err := os.RemoveAll(core.Paths().TempDir)
	require.NoError(t, err)
}
