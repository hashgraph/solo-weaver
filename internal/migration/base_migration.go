// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"context"

	"github.com/hashgraph/solo-weaver/pkg/semver"
	"github.com/joomcode/errorx"
)

// BaseMigration provides a default implementation of version-based migration detection.
// Embed this in concrete migrations to get default Applies() behavior.
type BaseMigration struct {
	id          string
	description string
	minVersion  string
}

// NewBaseMigration creates a new base migration with the given parameters.
func NewBaseMigration(id, description, minVersion string) BaseMigration {
	return BaseMigration{
		id:          id,
		description: description,
		minVersion:  minVersion,
	}
}

func (b *BaseMigration) ID() string          { return b.id }
func (b *BaseMigration) Description() string { return b.description }
func (b *BaseMigration) MinVersion() string  { return b.minVersion }

// Applies implements the default version boundary check:
// Returns true if installedVersion < minVersion AND targetVersion >= minVersion
func (b *BaseMigration) Applies(mctx *Context) (bool, error) {
	if mctx.InstalledVersion == "" {
		return false, nil // Not installed, no migration needed
	}

	installed, err := semver.NewSemver(mctx.InstalledVersion)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err,
			"cannot parse installed version %q", mctx.InstalledVersion)
	}

	target, err := semver.NewSemver(mctx.TargetVersion)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err,
			"cannot parse target version %q", mctx.TargetVersion)
	}

	minVersion, err := semver.NewSemver(b.minVersion)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err,
			"cannot parse migration min version %q", b.minVersion)
	}

	// Migration applies if upgrading across the version boundary
	return installed.LessThan(minVersion) && !target.LessThan(minVersion), nil
}

// Execute must be overridden by concrete implementations.
func (b *BaseMigration) Execute(ctx context.Context, mctx *Context) error {
	return errorx.NotImplemented.New("Execute not implemented for base migration")
}

// Rollback returns nil by default (no rollback). Override for custom rollback logic.
func (b *BaseMigration) Rollback(ctx context.Context, mctx *Context) error {
	return nil
}
