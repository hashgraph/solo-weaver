// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"strings"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
)

// fakeCapsuleClient is an in-memory CapsuleKubeClient for testing the config-CR
// apply policy without a cluster. `existing` maps CR name -> deployed content;
// `applied` records the CR names passed to ApplyTyped, in order.
type fakeCapsuleClient struct {
	existing map[string]string
	applied  []string
}

func (f *fakeCapsuleClient) ResourceExists(_ context.Context, _, _, _, name string) (bool, error) {
	_, ok := f.existing[name]
	return ok, nil
}

func (f *fakeCapsuleClient) GetResourceNestedString(_ context.Context, _, _, _, name string, _ ...string) (string, error) {
	return f.existing[name], nil
}

func (f *fakeCapsuleClient) ApplyTyped(_ context.Context, obj runtime.Object) error {
	name := obj.(interface{ GetName() string }).GetName()
	f.applied = append(f.applied, name)
	return nil
}

func (f *fakeCapsuleClient) provider() CapsuleKubeProvider {
	return func(context.Context) (CapsuleKubeClient, error) { return f, nil }
}

// consensusInputsWithConfigs returns inputs whose 11 config bodies are all set
// to content and whose per-file source is all set to source.
func consensusInputsWithConfigs(content, source string) models.ConsensusNodeInputs {
	in := models.ConsensusNodeInputs{Namespace: "ns", OrbitName: "orbit", NodeId: 0}
	in.ConfigLog4j2 = content
	in.ConfigSettings = content
	in.ConfigAppProperties = content
	in.ConfigAppOverrideProperties = content
	in.ConfigApiPermission = content
	in.ConfigBootstrap = content
	in.ConfigNodeProperties = content
	in.ConfigFeeSchedules = content
	in.ConfigSimpleFeesSchedules = content
	in.ConfigThrottles = content
	in.ConfigBlockNodes = content
	in.ConfigSources = map[string]string{
		models.ConfigKeyLog4j2:              source,
		models.ConfigKeySettings:            source,
		models.ConfigKeyAppProperties:       source,
		models.ConfigKeyAppOverride:         source,
		models.ConfigKeyApiPermission:       source,
		models.ConfigKeyBootstrap:           source,
		models.ConfigKeyNodeProperties:      source,
		models.ConfigKeyFeeSchedules:        source,
		models.ConfigKeySimpleFeesSchedules: source,
		models.ConfigKeyThrottles:           source,
		models.ConfigKeyBlockNodes:          source,
	}
	return in
}

func runEnsureConfigCRs(t *testing.T, in models.ConsensusNodeInputs, force bool, fake *fakeCapsuleClient) *automa.Report {
	t.Helper()
	step, err := EnsureConfigCRs(in, force, fake.provider()).Build()
	require.NoError(t, err)
	return step.Execute(context.Background())
}

// Fresh install: no CRs exist, so all 11 are created.
func TestEnsureConfigCRs_FreshInstall_CreatesAll(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{}}
	in := consensusInputsWithConfigs("v1", models.ConfigSourceEmbedded)

	report := runEnsureConfigCRs(t, in, false, fake)

	require.Equal(t, automa.StatusSuccess, report.Status, report.Error)
	assert.Len(t, fake.applied, 11, "every config CR should be created on a fresh install")
}

// Idempotent re-run: CRs exist with identical content, so nothing is applied.
func TestEnsureConfigCRs_Unchanged_NoApply(t *testing.T) {
	fresh := &fakeCapsuleClient{existing: map[string]string{}}
	in := consensusInputsWithConfigs("v1", models.ConfigSourceEmbedded)
	runEnsureConfigCRs(t, in, false, fresh) // seed the "deployed" set

	deployed := &fakeCapsuleClient{existing: map[string]string{}}
	for _, name := range fresh.applied {
		deployed.existing[name] = "v1"
	}

	report := runEnsureConfigCRs(t, in, false, deployed)

	require.Equal(t, automa.StatusSuccess, report.Status, report.Error)
	assert.Empty(t, deployed.applied, "unchanged config must not be re-applied")
}

// Re-run without a package (embedded) whose content differs from the deployed
// value must be refused — the embedded default may not silently overwrite.
func TestEnsureConfigCRs_EmbeddedDrift_RefusedWithoutForce(t *testing.T) {
	fresh := &fakeCapsuleClient{existing: map[string]string{}}
	runEnsureConfigCRs(t, consensusInputsWithConfigs("from-package", models.ConfigSourcePackage), false, fresh)

	deployed := &fakeCapsuleClient{existing: map[string]string{}}
	for _, name := range fresh.applied {
		deployed.existing[name] = "from-package"
	}

	// Now re-run with embedded defaults (no package) that differ.
	in := consensusInputsWithConfigs("embedded-default", models.ConfigSourceEmbedded)
	report := runEnsureConfigCRs(t, in, false, deployed)

	require.Equal(t, automa.StatusFailed, report.Status)
	require.Error(t, report.Error)
	assert.Contains(t, report.Error.Error(), "refusing to overwrite deployed config")
	assert.Empty(t, deployed.applied, "no CR should be overwritten when the change is refused")
}

// Re-run with a deployment package whose content differs is authoritative and
// updates every changed CR.
func TestEnsureConfigCRs_PackageUpdate_Applies(t *testing.T) {
	fresh := &fakeCapsuleClient{existing: map[string]string{}}
	runEnsureConfigCRs(t, consensusInputsWithConfigs("old", models.ConfigSourceEmbedded), false, fresh)

	deployed := &fakeCapsuleClient{existing: map[string]string{}}
	for _, name := range fresh.applied {
		deployed.existing[name] = "old"
	}

	in := consensusInputsWithConfigs("from-package", models.ConfigSourcePackage)
	report := runEnsureConfigCRs(t, in, false, deployed)

	require.Equal(t, automa.StatusSuccess, report.Status, report.Error)
	assert.Len(t, deployed.applied, 11, "a package update must apply to every changed CR")
}

// With --force, an embedded default that differs resets the deployed CR.
func TestEnsureConfigCRs_EmbeddedDrift_ForceResets(t *testing.T) {
	fresh := &fakeCapsuleClient{existing: map[string]string{}}
	runEnsureConfigCRs(t, consensusInputsWithConfigs("from-package", models.ConfigSourcePackage), false, fresh)

	deployed := &fakeCapsuleClient{existing: map[string]string{}}
	for _, name := range fresh.applied {
		deployed.existing[name] = "from-package"
	}

	in := consensusInputsWithConfigs("embedded-default", models.ConfigSourceEmbedded)
	report := runEnsureConfigCRs(t, in, true, deployed)

	require.Equal(t, automa.StatusSuccess, report.Status, report.Error)
	assert.Len(t, deployed.applied, 11, "--force must reset every CR to embedded defaults")
}

// Empty resolved content is a hard failure regardless of policy.
func TestEnsureConfigCRs_EmptyContent_Fails(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{}}
	in := consensusInputsWithConfigs("v1", models.ConfigSourceEmbedded)
	in.ConfigThrottles = "" // one empty file

	report := runEnsureConfigCRs(t, in, false, fake)

	require.Equal(t, automa.StatusFailed, report.Status)
	require.Error(t, report.Error)
	assert.Contains(t, strings.ToLower(report.Error.Error()), "empty")
}
