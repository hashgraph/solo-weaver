package steps

import (
	"context"
	"github.com/automa-saga/automa"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

// mockStep implements automa.Step for testing
type mockStep struct {
	id    string
	state automa.StateBag
}

func (m *mockStep) Prepare(ctx context.Context) (context.Context, error) {
	return ctx, nil
}

func (m *mockStep) Execute(ctx context.Context) *automa.Report {
	return automa.SuccessReport(m)
}

func (m *mockStep) Rollback(ctx context.Context) *automa.Report {
	return automa.SuccessReport(m)
}

func (m *mockStep) State() automa.StateBag {
	if m.state == nil {
		m.state = &automa.SyncStateBag{}
	}

	return m.state
}

func (m *mockStep) Id() string { return m.id }

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
