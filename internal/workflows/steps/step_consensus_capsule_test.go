// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"strings"
	"testing"

	"github.com/automa-saga/automa"
	operatorv1alpha1 "github.com/hashgraph/solo-operator/api/v1alpha1"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
)

// fakeCapsuleClient is an in-memory CapsuleKubeClient for testing the config-CR
// apply policy without a cluster. `existing` maps CR name -> deployed content;
// `applied` records the CR names passed to ApplyTyped, in order.
type fakeCapsuleClient struct {
	existing    map[string]string
	applied     []string
	appliedObjs []runtime.Object
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
	f.appliedObjs = append(f.appliedObjs, obj)
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

// findCapsule returns the ConsensusCapsule applied by the step, failing if none.
func findCapsule(t *testing.T, objs []runtime.Object) *operatorv1alpha1.ConsensusCapsule {
	t.Helper()
	for _, o := range objs {
		if c, ok := o.(*operatorv1alpha1.ConsensusCapsule); ok {
			return c
		}
	}
	t.Fatal("no ConsensusCapsule was applied")
	return nil
}

// TestCreateConsensusCapsule_MatchesOperatorContract pins the capsule spec to the
// solo-operator contract verified against docs/example: the mandatory UC sidecar
// is enabled, the image is split into repository/imageName/tag, and every config
// *Ref uses the canonical config-CR name. These are the exact fields whose drift
// left the capsule stuck (UCSidecarRequired / ConfigMap-not-found / ImageRequired).
func TestCreateConsensusCapsule_MatchesOperatorContract(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{}}
	in := models.ConsensusNodeInputs{
		Namespace:          "hiero-network-1",
		OrbitName:          "hiero-network-1",
		NodeId:             0,
		AccountId:          "0.0.3",
		Weight:             500,
		ConsensusImageRepo: "gcr.io/hedera-registry/consensus-node",
		ConsensusImageTag:  "0.74.2",
		GrpcTlsSecret:      "node0-grpc-tls-keys",
		SigningSecret:      "node0-gossip-keys",
	}

	step, err := CreateConsensusCapsule(in, fake.provider()).Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.Equal(t, automa.StatusSuccess, report.Status, report.Error)

	capsule := findCapsule(t, fake.appliedObjs)

	// Mandatory UC sidecar (UCSidecarRequired otherwise).
	require.NotNil(t, capsule.Spec.PodProperties.Containers.UC)
	assert.True(t, capsule.Spec.PodProperties.Containers.UC.Enabled, "uc sidecar must be enabled")

	// Image must be split repository/imageName:tag (ImageRequired otherwise).
	sv := capsule.Spec.PodProperties.Containers.ConsensusNode.SoftwareVersion
	require.NotNil(t, sv)
	assert.Equal(t, "gcr.io/hedera-registry", sv.Repository)
	assert.Equal(t, "consensus-node", sv.ImageName)
	assert.Equal(t, "0.74.2", sv.ImageTag)

	// Every config *Ref must use the canonical config-CR name (ConfigMap-not-found
	// otherwise). These literals are the names from solo-operator docs/example.
	scope := models.ConsensusNodeScope(in.NodeId)
	cases := []struct {
		field string
		key   string
		want  string
		got   string
	}{
		{"Log4j2ConfigRef", models.ConfigKeyLog4j2, "node0-log4j2", capsule.Spec.Log4j2ConfigRef},
		{"SettingsConfigRef", models.ConfigKeySettings, "node0-settings", capsule.Spec.SettingsConfigRef},
		{"ApplicationPropertiesRef", models.ConfigKeyAppProperties, "node0-application-properties", capsule.Spec.ApplicationPropertiesRef},
		{"ApplicationOverridePropertiesRef", models.ConfigKeyAppOverride, "node0-application-override-properties", capsule.Spec.ApplicationOverridePropertiesRef},
		{"ApiPermissionPropertiesRef", models.ConfigKeyApiPermission, "node0-api-permission-properties", capsule.Spec.ApiPermissionPropertiesRef},
		{"BootstrapPropertiesRef", models.ConfigKeyBootstrap, "node0-bootstrap-properties", capsule.Spec.BootstrapPropertiesRef},
		{"NodePropertiesRef", models.ConfigKeyNodeProperties, "node0-node-properties", capsule.Spec.NodePropertiesRef},
		{"FeeSchedulesRef", models.ConfigKeyFeeSchedules, "node0-fee-schedules", capsule.Spec.FeeSchedulesRef},
		{"SimpleFeesSchedulesRef", models.ConfigKeySimpleFeesSchedules, "node0-simple-fee-schedules", capsule.Spec.SimpleFeesSchedulesRef},
		{"ThrottlesConfigRef", models.ConfigKeyThrottles, "node0-throttles", capsule.Spec.ThrottlesConfigRef},
		{"BlockNodesConfigRef", models.ConfigKeyBlockNodes, "node0-block-nodes", capsule.Spec.BlockNodesConfigRef},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, c.got, "%s must equal the canonical example name", c.field)
		assert.Equal(t, models.ConsensusConfigCRName(scope, c.key), c.got,
			"%s must equal ConsensusConfigCRName so EnsureConfigCRs and the capsule ref never drift", c.field)
	}
}

func TestSplitConsensusImage(t *testing.T) {
	repo, name := splitConsensusImage("gcr.io/hedera-registry/consensus-node")
	assert.Equal(t, "gcr.io/hedera-registry", repo)
	assert.Equal(t, "consensus-node", name)

	// No slash: whole value is the image name, empty repository.
	repo, name = splitConsensusImage("consensus-node")
	assert.Equal(t, "", repo)
	assert.Equal(t, "consensus-node", name)
}
