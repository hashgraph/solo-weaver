// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"fmt"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/semver"
	"github.com/joomcode/errorx"
)

// Well-known CLI version context keys for startup-scoped migrations.
const (
	CtxKeyInstalledCLIVersion = "installedCLIVersion"
	CtxKeyCurrentCLIVersion   = "currentCLIVersion"
)

// BaselineCLIVersion is the assumed installed version when none was recorded on
// disk. It makes every version-boundary migration eligible; each migration's
// Execute() guard keeps it a no-op on a fresh machine.
const BaselineCLIVersion = "0.0.0"

// ResolveInstalledCLIVersion maps a recorded on-disk CLI version to a usable
// semver baseline. An absent (empty) version is a pre-state-tracking host; a
// non-semver value (e.g. "dev" from an unstamped build that was accidentally
// persisted — a test binary writing the real state file, or a plain `go build`)
// is corrupt input we must not let brick every subsequent CLI invocation. Both
// resolve to BaselineCLIVersion so version-boundary migrations still evaluate
// (and, being below every boundary, stay no-ops) rather than failing to parse.
func ResolveInstalledCLIVersion(raw string) string {
	if raw == "" {
		return BaselineCLIVersion
	}
	if _, err := semver.NewSemver(raw); err != nil {
		logx.As().Warn().
			Str("recordedVersion", raw).
			Msg("Recorded on-disk CLI version is not valid semver; treating it as the baseline. This usually means an unstamped build (version \"dev\") persisted the state file. No action needed — the running version is re-recorded on this run; use a stamped build (task build) to avoid persisting \"dev\" again.")
		return BaselineCLIVersion
	}
	return raw
}

// CLIVersionMigration provides a base implementation for CLI-version-boundary startup migrations.
// Embed this in concrete migrations to get ID(), Description(), and Applies() behaviour
// that gates on the CLI binary version rather than a deployed chart version.
//
// Concrete migrations must implement Execute() and Rollback() themselves.
type CLIVersionMigration struct {
	id          string
	description string
	minVersion  string
}

// NewCLIVersionMigration creates a new CLI-version-gated startup migration.
// minVersion is the CLI version boundary — applies when upgrading from < minVersion to >= minVersion.
func NewCLIVersionMigration(id, description, minVersion string) CLIVersionMigration {
	return CLIVersionMigration{id: id, description: description, minVersion: minVersion}
}

func (v *CLIVersionMigration) ID() string          { return v.id }
func (v *CLIVersionMigration) Description() string { return v.description }

// Applies returns true if the CLI is upgrading across the version boundary.
//
// An empty installedCLIVersion returns false; the startup path supplies
// BaselineCLIVersion instead, so this is only a fallback for direct callers.
func (v *CLIVersionMigration) Applies(mctx *Context) (bool, error) {
	var installedCLIVersion string
	if s, ok := mctx.Data.String(CtxKeyInstalledCLIVersion); ok {
		installedCLIVersion = s
	}

	if installedCLIVersion == "" {
		return false, nil // fresh install — no previous version to migrate from
	}

	var currentCLIVersion string
	if s, ok := mctx.Data.String(CtxKeyCurrentCLIVersion); ok {
		currentCLIVersion = s
	}

	if currentCLIVersion == "" {
		return false, errx.Decorate(
			errorx.IllegalState.New("current CLI version not provided in context"),
			reasons.Internal)
	}

	installed, err := semver.NewSemver(installedCLIVersion)
	if err != nil {
		// Unreachable from the normal startup path: RunStartupMigrations sanitizes
		// the on-disk version through ResolveInstalledCLIVersion (non-semver → baseline)
		// before populating the context, and then re-records the running version on a
		// provisioned host — so a corrupt value self-heals without operator action.
		// Reaching here means a direct caller set a raw installed version in the context.
		return false, errx.Decorate(
			errorx.IllegalState.Wrap(err, "invalid installed CLI version %q", installedCLIVersion),
			reasons.Internal)
	}

	current, err := semver.NewSemver(currentCLIVersion)
	if err != nil {
		return false, errx.Decorate(
			errorx.IllegalState.Wrap(err, "invalid current CLI version %q", currentCLIVersion),
			reasons.Internal,
			fmt.Sprintf("This binary reports version %q, which is not valid semver — it was likely built without version stamping", currentCLIVersion),
			"Rebuild and reinstall with 'task build' (which stamps VERSION via ldflags) instead of a plain 'go build'")
	}

	minVer, err := semver.NewSemver(v.minVersion)
	if err != nil {
		// A migration declaring a malformed minVersion is a solo-weaver bug, not an
		// operator error — reasons.Internal, no hints.
		return false, errx.Decorate(
			errorx.IllegalState.Wrap(err, "invalid min version %q for migration %q", v.minVersion, v.id),
			reasons.Internal)
	}

	return installed.LessThan(minVer) && !current.LessThan(minVer), nil
}
