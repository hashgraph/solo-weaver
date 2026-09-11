// SPDX-License-Identifier: Apache-2.0

// migration_shell_completion.go installs the kubectl and helm bash completion
// loaders on hosts that have the tools but not the loaders. Gated on host state
// rather than a version boundary, so it also repairs a loader that was deleted.
//
// Registered in cmd/cli/commands/root.go RegisterMigrations() under
// migration.ScopeStartup.

package workflows

import (
	"context"
	"os"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// Seams so tests can script host state without touching /usr/local/share.
var (
	shellCompletionNeedsWrite  = software.ShellCompletionNeedsWrite
	shellCompletionWritable    = software.ShellCompletionWritable
	reconfigureShellCompletion = software.ReconfigureShellCompletion
	shellCompletionGeteuid     = os.Geteuid
)

// ShellCompletionMigration writes the bash completion loaders for the CLI tools
// weaver installs.
type ShellCompletionMigration struct{}

// NewShellCompletionMigration constructs the migration.
func NewShellCompletionMigration() *ShellCompletionMigration {
	return &ShellCompletionMigration{}
}

// ID implements migration.Migration. No version suffix — not tied to a release boundary.
func (m *ShellCompletionMigration) ID() string { return "shell-completion-loaders" }

// Description implements migration.Migration.
func (m *ShellCompletionMigration) Description() string {
	return "Install the kubectl and helm bash completion loaders on hosts provisioned before " +
		"the install workflow wrote them"
}

// Applies reports whether an installed tool is missing its loader and the
// directory can be written. The writability check keeps a host that can never
// succeed from reporting work outstanding on every root invocation.
//
// It never returns an error: migration.GetApplicableMigrations aborts the whole
// migration pass when any Applies errors, which would break every command.
func (m *ShellCompletionMigration) Applies(mctx *migration.Context) (bool, error) {
	// Writing under /usr/local/share needs root; leave it to a privileged
	// invocation instead of failing for every unprivileged caller.
	if euid := shellCompletionGeteuid(); euid != 0 {
		logx.As().Debug().Int("euid", euid).
			Msg("shell completion migration: not root; leaving the loaders to a privileged invocation")
		return false, nil
	}

	if !shellCompletionNeedsWrite() {
		return false, nil
	}

	if !shellCompletionWritable() {
		logx.As().Debug().
			Msg("shell completion migration: completion directory is not writable; skipping")
		return false, nil
	}

	return true, nil
}

// Execute writes the missing loaders. A failure is only warned about: no CLI
// invocation should fail because a completion file could not be written.
func (m *ShellCompletionMigration) Execute(ctx context.Context, mctx *migration.Context) error {
	logx.As().Info().Msg("Installing kubectl and helm bash completion loaders on this host")

	if err := reconfigureShellCompletion(); err != nil {
		logx.As().Warn().Err(err).
			Msg("could not install the bash completion loaders — tab completion for kubectl and helm " +
				"will be unavailable. Check that /usr/local/share is writable, then verify with: " +
				"ls -l /usr/local/share/bash-completion/completions")
	}

	return nil
}

// Rollback is a no-op; the uninstall path already removes the loaders.
func (m *ShellCompletionMigration) Rollback(ctx context.Context, mctx *migration.Context) error {
	logx.As().Warn().Msg("Rollback for the shell completion migration is not supported")
	return nil
}
