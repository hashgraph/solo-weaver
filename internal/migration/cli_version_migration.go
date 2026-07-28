// SPDX-License-Identifier: Apache-2.0

package migration

import (
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

// ResolveInstalledCLIVersion maps an absent (empty) version to BaselineCLIVersion;
// non-empty is returned as-is.
func ResolveInstalledCLIVersion(raw string) string {
	if raw == "" {
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
		return false, errorx.IllegalArgument.New("current CLI version not provided in context")
	}

	installed, err := semver.NewSemver(installedCLIVersion)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "invalid installed CLI version %q", installedCLIVersion)
	}

	current, err := semver.NewSemver(currentCLIVersion)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "invalid current CLI version %q", currentCLIVersion)
	}

	minVer, err := semver.NewSemver(v.minVersion)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "invalid min version %q", v.minVersion)
	}

	return installed.LessThan(minVer) && !current.LessThan(minVer), nil
}
