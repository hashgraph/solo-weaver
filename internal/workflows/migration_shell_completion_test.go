// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShellCompletionMigration_Metadata(t *testing.T) {
	m := NewShellCompletionMigration()
	assert.Equal(t, "shell-completion-loaders", m.ID())
	assert.Contains(t, m.Description(), "bash completion")
}

// stubCompletionHost scripts the host state Applies sees and pins the caller as root.
func stubCompletionHost(t *testing.T, needsWrite, writable bool) {
	t.Helper()

	origNeeds := shellCompletionNeedsWrite
	origWritable := shellCompletionWritable
	t.Cleanup(func() {
		shellCompletionNeedsWrite = origNeeds
		shellCompletionWritable = origWritable
	})

	shellCompletionNeedsWrite = func() bool { return needsWrite }
	shellCompletionWritable = func() bool { return writable }

	stubCompletionEuid(t, 0)
}

// stubCompletionEuid pins the effective uid Applies sees.
func stubCompletionEuid(t *testing.T, euid int) {
	t.Helper()

	orig := shellCompletionGeteuid
	t.Cleanup(func() { shellCompletionGeteuid = orig })
	shellCompletionGeteuid = func() int { return euid }
}

func TestShellCompletionMigration_Applies(t *testing.T) {
	tests := []struct {
		name       string
		needsWrite bool
		writable   bool
		want       bool
	}{
		{"host that already has the loaders is skipped", false, true, false},
		{"host missing a loader is picked up", true, true, true},
		// A host that can never be written must not report work outstanding on
		// every invocation.
		{"missing loader on a read-only filesystem is skipped", true, false, false},
		{"nothing to do on a read-only filesystem is still skipped", false, false, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stubCompletionHost(t, tc.needsWrite, tc.writable)

			got, err := NewShellCompletionMigration().Applies(newMctx("0.29.0", "0.30.0"))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestShellCompletionMigration_Applies_NonRootIsSkipped(t *testing.T) {
	stubCompletionEuid(t, 1000)

	// The probes must not even run for an unprivileged caller.
	origNeeds := shellCompletionNeedsWrite
	origWritable := shellCompletionWritable
	t.Cleanup(func() {
		shellCompletionNeedsWrite = origNeeds
		shellCompletionWritable = origWritable
	})
	shellCompletionNeedsWrite = func() bool {
		t.Fatal("probe must not run for a non-root caller")
		return false
	}
	shellCompletionWritable = func() bool {
		t.Fatal("writability probe must not run for a non-root caller")
		return false
	}

	got, err := NewShellCompletionMigration().Applies(newMctx("0.29.0", "0.30.0"))
	require.NoError(t, err)
	assert.False(t, got)
}

func TestShellCompletionMigration_Execute(t *testing.T) {
	tests := []struct {
		name     string
		writeErr error
	}{
		{"writes the loaders", nil},
		// An error here would propagate out of RunStartupMigrations and fail
		// every CLI command on the host.
		{"write failure is warned about, not returned", errorx.ExternalError.New("read-only")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			orig := reconfigureShellCompletion
			t.Cleanup(func() { reconfigureShellCompletion = orig })

			called := false
			reconfigureShellCompletion = func() error {
				called = true
				return tc.writeErr
			}

			err := NewShellCompletionMigration().Execute(context.Background(), newMctx("0.29.0", "0.30.0"))
			require.NoError(t, err)
			assert.True(t, called)
		})
	}
}

func TestShellCompletionMigration_Rollback(t *testing.T) {
	err := NewShellCompletionMigration().Rollback(context.Background(), newMctx("0.29.0", "0.30.0"))
	require.NoError(t, err)
}
