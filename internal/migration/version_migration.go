// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"github.com/hashgraph/solo-weaver/pkg/semver"
	"github.com/joomcode/errorx"
)

// Well-known context keys
const (
	CtxKeyInstalledVersion = "installedVersion"
	CtxKeyTargetVersion    = "targetVersion"
)

// VersionMigration provides a base implementation for version-boundary migrations.
// Embed this in concrete migrations to get ID(), Description(), and Applies() behavior.
// Concrete migrations must implement Execute() and Rollback() themselves.
type VersionMigration struct {
	id          string
	description string
	minVersion  string
}

// NewVersionMigration creates a new version-based migration.
// minVersion is the version boundary - applies when upgrading from < minVersion to >= minVersion.
func NewVersionMigration(id, description, minVersion string) VersionMigration {
	return VersionMigration{id: id, description: description, minVersion: minVersion}
}

func (v *VersionMigration) ID() string          { return v.id }
func (v *VersionMigration) Description() string { return v.description }

// Applies returns true if upgrading across the version boundary.
func (v *VersionMigration) Applies(mctx *Context) (bool, error) {
	var installedVersion string
	if v, ok := mctx.Data.String(CtxKeyInstalledVersion); ok {
		installedVersion = v
	}

	if installedVersion == "" {
		return false, nil
	}

	var targetVersion string
	if v, ok := mctx.Data.String(CtxKeyTargetVersion); ok {
		targetVersion = v
	}

	if targetVersion == "" {
		return false, errorx.IllegalArgument.New("target version not provided in context")
	}

	installed, err := semver.NewSemver(installedVersion)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "invalid installed version %q", installedVersion)
	}

	target, err := semver.NewSemver(targetVersion)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "invalid target version %q", targetVersion)
	}

	minVer, err := semver.NewSemver(v.minVersion)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "invalid min version %q", v.minVersion)
	}

	return installed.LessThan(minVer) && !target.LessThan(minVer), nil
}
