// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/joomcode/errorx"
)

// ComponentBlockNode is the component name for block node migrations.
const ComponentBlockNode = "block-node"

// RegisterMigrations registers all block node migrations.
// Called once at startup from root.go.
func RegisterMigrations() {
	migration.Register(ComponentBlockNode, NewVerificationStorageMigration())
}

// GetMigrationWorkflow returns an automa workflow for executing applicable migrations.
// Returns nil if no migrations are needed.
func GetMigrationWorkflow(manager *Manager, profile, valuesFile string, reuseValues bool) (*automa.WorkflowBuilder, error) {
	installedVersion, err := manager.GetInstalledVersion()
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to get installed version")
	}

	if installedVersion == "" {
		return nil, nil
	}

	// Build context
	mctx := &migration.Context{
		Component: ComponentBlockNode,
		Logger:    manager.logger,
		Data:      make(map[string]interface{}),
	}
	mctx.Set(migration.CtxKeyInstalledVersion, installedVersion)
	mctx.Set(migration.CtxKeyTargetVersion, manager.blockConfig.Version)

	migrations, err := migration.GetApplicableMigrations(ComponentBlockNode, mctx)
	if err != nil {
		return nil, err
	}

	if len(migrations) == 0 {
		return nil, nil
	}

	// Capture release values if needed
	var capturedValues map[string]interface{}
	if reuseValues && valuesFile == "" {
		capturedValues, err = manager.GetReleaseValues()
		if err != nil {
			return nil, errorx.IllegalState.Wrap(err, "failed to capture release values")
		}
	}

	// Add remaining context data
	mctx.Set(ctxKeyManager, manager)
	mctx.Set(ctxKeyProfile, profile)
	mctx.Set(ctxKeyValuesFile, valuesFile)
	mctx.Set(ctxKeyReuseValues, reuseValues)
	if capturedValues != nil {
		mctx.Set(ctxKeyCapturedValues, capturedValues)
	}

	return migration.ToWorkflow(migrations, mctx), nil
}
