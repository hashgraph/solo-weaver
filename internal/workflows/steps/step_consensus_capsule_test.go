// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"strings"
	"testing"

	"github.com/automa-saga/automa"
	operatorv1alpha1 "github.com/hashgraph/solo-operator/api/v1alpha1"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// fakeCapsuleClient is an in-memory CapsuleKubeClient for testing the config-CR
// apply policy without a cluster. `existing` maps CR name -> deployed content;
// `applied` records the CR names passed to ApplyTyped, in order.
type fakeCapsuleClient struct {
	existing    map[string]string
	nested      map[string]map[string]interface{}
	applied     []string
	appliedObjs []runtime.Object
	listResult  *unstructured.UnstructuredList
}

func (f *fakeCapsuleClient) ResourceExists(_ context.Context, _, _, _, name string) (bool, error) {
	_, ok := f.existing[name]
	return ok, nil
}

func (f *fakeCapsuleClient) GetResourceNestedString(_ context.Context, _, _, _, name string, _ ...string) (string, error) {
	return f.existing[name], nil
}

func (f *fakeCapsuleClient) GetResourceNestedMap(_ context.Context, _, _, _, name string, _ ...string) (map[string]interface{}, bool, error) {
	m, ok := f.nested[name]
	return m, ok, nil
}

func (f *fakeCapsuleClient) ApplyTyped(_ context.Context, obj runtime.Object) error {
	name := obj.(interface{ GetName() string }).GetName()
	f.applied = append(f.applied, name)
	f.appliedObjs = append(f.appliedObjs, obj)
	return nil
}

func (f *fakeCapsuleClient) List(_ context.Context, _ kube.ResourceKind, _ string, _ kube.WaitOptions) (*unstructured.UnstructuredList, error) {
	if f.listResult != nil {
		return f.listResult, nil
	}
	return &unstructured.UnstructuredList{}, nil
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

	// Explicit Java heap + resources must be set — without them the 0.74 node
	// stalls on startup. Unset inputs fall back to the ConsensusDefault* values.
	cn := capsule.Spec.PodProperties.Containers.ConsensusNode
	assert.Equal(t, models.ConsensusDefaultContainerName, cn.Name)
	assert.Equal(t, models.ConsensusDefaultJavaHeapMin, cn.JavaHeapMin)
	assert.Equal(t, models.ConsensusDefaultJavaHeapMax, cn.JavaHeapMax)
	assert.Equal(t, models.ConsensusDefaultJavaOpts, cn.JavaOpts)
	require.NotNil(t, cn.Resources)
	assert.Equal(t, models.ConsensusDefaultCPULimit, cn.Resources.Limits.Cpu().String())
	assert.Equal(t, models.ConsensusDefaultMemoryLimit, cn.Resources.Limits.Memory().String())
	assert.Equal(t, models.ConsensusDefaultCPURequest, cn.Resources.Requests.Cpu().String())
	assert.Equal(t, models.ConsensusDefaultMemoryRequest, cn.Resources.Requests.Memory().String())

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

func TestCreateConsensusCapsule_MultiRegistrySource(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{}}
	in := models.ConsensusNodeInputs{
		Namespace:          "hiero-network-1",
		OrbitName:          "hiero-network-1",
		NodeId:             0,
		AccountId:          "0.0.3",
		Weight:             500,
		ConsensusImageRepo: "gcr.io/hedera-registry/consensus-node",
		ConsensusImageTag:  "0.74.2",
		// Host-keyed selector: each registry resolves its own secret by host.
		ImagePullSecrets: models.PullSecretSelector{ByHost: map[string]string{
			"gcr.io":    "gcr-creds",
			"docker.io": "docker-creds",
		}},
		ConsensusImageSource: &models.ImageSource{
			Repositories: []models.ImageRepositoryRef{
				{Repository: "gcr.io/hedera-registry", ImageName: "consensus-node", ImageTag: "0.74.2"},
				{Repository: "docker.io/hashgraph", ImageName: "consensus-node", ImageTag: "0.74.2"},
			},
			LayerHashes: map[string][]string{
				"linux/amd64": {"sha256:aaa", "sha256:bbb"},
				"linux/arm64": {"sha256:ccc"},
			},
		},
	}

	step, err := CreateConsensusCapsule(in, fake.provider()).Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.Equal(t, automa.StatusSuccess, report.Status, report.Error)

	capsule := findCapsule(t, fake.appliedObjs)
	cn := capsule.Spec.PodProperties.Containers.ConsensusNode

	// SoftwareVersion stays populated — it is a required CRD field even when a
	// source is set (the operator merely prefers the source).
	require.NotNil(t, cn.SoftwareVersion)
	assert.Equal(t, "gcr.io/hedera-registry", cn.SoftwareVersion.Repository)
	// The single SoftwareVersion resolves its secret by host (gcr.io ⇒ gcr-creds).
	require.Len(t, cn.SoftwareVersion.ImagePullSecrets, 1)
	assert.Equal(t, "gcr-creds", cn.SoftwareVersion.ImagePullSecrets[0].Name)

	src := cn.SoftwareVersionSource
	require.NotNil(t, src)
	require.Len(t, src.ImageRepositories, 2)
	assert.Equal(t, "gcr.io/hedera-registry", src.ImageRepositories[0].Repository)
	assert.Equal(t, "consensus-node", src.ImageRepositories[0].ImageName)
	assert.Equal(t, "0.74.2", src.ImageRepositories[0].ImageTag)
	assert.Equal(t, "docker.io/hashgraph", src.ImageRepositories[1].Repository)
	// Each candidate carries its own host-resolved pull secret, not a shared one.
	require.Len(t, src.ImageRepositories[0].ImagePullSecrets, 1)
	assert.Equal(t, "gcr-creds", src.ImageRepositories[0].ImagePullSecrets[0].Name)
	require.Len(t, src.ImageRepositories[1].ImagePullSecrets, 1)
	assert.Equal(t, "docker-creds", src.ImageRepositories[1].ImagePullSecrets[0].Name)

	// Verification entries are emitted one per platform, in sorted platform order.
	require.Len(t, src.ImageVerificationSpec, 2)
	assert.Equal(t, "linux", src.ImageVerificationSpec[0].OS)
	assert.Equal(t, "amd64", src.ImageVerificationSpec[0].Architecture)
	assert.Equal(t, []string{"sha256:aaa", "sha256:bbb"}, src.ImageVerificationSpec[0].LayerHashes)
	assert.Equal(t, "arm64", src.ImageVerificationSpec[1].Architecture)
	// Left unset so the operator uses its --registry-order default.
	assert.Empty(t, src.SelectionStrategy)
}

func TestCreateConsensusCapsule_NoSource_SingleSoftwareVersion(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{}}
	in := models.ConsensusNodeInputs{
		Namespace:          "hiero-network-1",
		OrbitName:          "hiero-network-1",
		NodeId:             0,
		AccountId:          "0.0.3",
		Weight:             500,
		ConsensusImageRepo: "gcr.io/hedera-registry/consensus-node",
		ConsensusImageTag:  "0.74.2",
		// ConsensusImageSource intentionally nil.
	}

	step, err := CreateConsensusCapsule(in, fake.provider()).Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.Equal(t, automa.StatusSuccess, report.Status, report.Error)

	cn := findCapsule(t, fake.appliedObjs).Spec.PodProperties.Containers.ConsensusNode
	require.NotNil(t, cn.SoftwareVersion)
	assert.Nil(t, cn.SoftwareVersionSource, "no manifest source ⇒ single SoftwareVersion only")
}

func TestCreateConsensusCapsule_PreservesExistingSourceWhenNoManifest(t *testing.T) {
	capsuleName := models.ConsensusCapsuleName("hiero-network-1", 0)
	existing := &operatorv1alpha1.SoftwareVersionSource{
		ImageRepositories: []operatorv1alpha1.ImageRepository{
			{Repository: "gcr.io/hedera-registry", ImageName: "consensus-node", ImageTag: "0.74.2"},
			{Repository: "docker.io/hashgraph", ImageName: "consensus-node", ImageTag: "0.74.2"},
		},
		ImageVerificationSpec: []operatorv1alpha1.ImageVerificationSpec{
			{OS: "linux", Architecture: "amd64", LayerHashes: []string{"sha256:aaa"}},
		},
	}
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(existing)
	require.NoError(t, err)

	fake := &fakeCapsuleClient{
		existing: map[string]string{},
		nested:   map[string]map[string]interface{}{capsuleName: m},
	}
	in := models.ConsensusNodeInputs{
		Namespace:          "hiero-network-1",
		OrbitName:          "hiero-network-1",
		NodeId:             0,
		AccountId:          "0.0.3",
		Weight:             500,
		ConsensusImageRepo: "gcr.io/hedera-registry/consensus-node",
		ConsensusImageTag:  "0.74.2",
		// No manifest source this run, and the image was not pinned by the user.
		ConsensusImageSource: nil,
		ImagePinned:          false,
	}

	step, err := CreateConsensusCapsule(in, fake.provider()).Build()
	require.NoError(t, err)
	require.Equal(t, automa.StatusSuccess, step.Execute(context.Background()).Status)

	src := findCapsule(t, fake.appliedObjs).Spec.PodProperties.Containers.ConsensusNode.SoftwareVersionSource
	require.NotNil(t, src, "existing source must be preserved so SSA does not drop it")
	require.Len(t, src.ImageRepositories, 2)
	assert.Equal(t, "docker.io/hashgraph", src.ImageRepositories[1].Repository)
}

func TestCreateConsensusCapsule_PinnedDropsExistingSource(t *testing.T) {
	capsuleName := models.ConsensusCapsuleName("hiero-network-1", 0)
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&operatorv1alpha1.SoftwareVersionSource{
		ImageRepositories:     []operatorv1alpha1.ImageRepository{{Repository: "r", ImageName: "n", ImageTag: "t"}},
		ImageVerificationSpec: []operatorv1alpha1.ImageVerificationSpec{{OS: "linux", Architecture: "amd64", LayerHashes: []string{"sha256:aaa"}}},
	})
	require.NoError(t, err)

	fake := &fakeCapsuleClient{
		existing: map[string]string{},
		nested:   map[string]map[string]interface{}{capsuleName: m},
	}
	in := models.ConsensusNodeInputs{
		Namespace:          "hiero-network-1",
		OrbitName:          "hiero-network-1",
		NodeId:             0,
		AccountId:          "0.0.3",
		Weight:             500,
		ConsensusImageRepo: "gcr.io/hedera-registry/consensus-node",
		ConsensusImageTag:  "0.74.2",
		ImagePinned:        true, // user pinned via --image-repo/--image-tag
	}

	step, err := CreateConsensusCapsule(in, fake.provider()).Build()
	require.NoError(t, err)
	require.Equal(t, automa.StatusSuccess, step.Execute(context.Background()).Status)

	cn := findCapsule(t, fake.appliedObjs).Spec.PodProperties.Containers.ConsensusNode
	assert.Nil(t, cn.SoftwareVersionSource, "an explicit pin must drop the existing source")
}

func TestBuildSoftwareVersionSource(t *testing.T) {
	var none models.PullSecretSelector

	// nil / empty inputs yield no source.
	assert.Nil(t, buildSoftwareVersionSource(nil, none))
	assert.Nil(t, buildSoftwareVersionSource(&models.ImageSource{}, none))
	assert.Nil(t, buildSoftwareVersionSource(&models.ImageSource{
		Repositories: []models.ImageRepositoryRef{{Repository: "r", ImageName: "n", ImageTag: "t"}},
	}, none), "no layer hashes ⇒ nil")

	// A source with a platform key that has no OS/arch split is skipped; if it is
	// the only entry the whole source is nil (nothing verifiable).
	assert.Nil(t, buildSoftwareVersionSource(&models.ImageSource{
		Repositories: []models.ImageRepositoryRef{{Repository: "r", ImageName: "n", ImageTag: "t"}},
		LayerHashes:  map[string][]string{"bogus": {"sha256:x"}},
	}, none))

	// No secret selected for the host ⇒ no ImagePullSecrets on that repository.
	src := buildSoftwareVersionSource(&models.ImageSource{
		Repositories: []models.ImageRepositoryRef{{Repository: "ghcr.io/x", ImageName: "n", ImageTag: "t"}},
		LayerHashes:  map[string][]string{"linux/amd64": {"sha256:x"}},
	}, none)
	require.NotNil(t, src)
	assert.Nil(t, src.ImageRepositories[0].ImagePullSecrets)

	// The selector resolves the secret by registry host.
	src = buildSoftwareVersionSource(&models.ImageSource{
		Repositories: []models.ImageRepositoryRef{{Repository: "ghcr.io/x", ImageName: "n", ImageTag: "t"}},
		LayerHashes:  map[string][]string{"linux/amd64": {"sha256:x"}},
	}, models.PullSecretSelector{ByHost: map[string]string{"ghcr.io": "regcred"}})
	require.NotNil(t, src)
	require.Len(t, src.ImageRepositories[0].ImagePullSecrets, 1)
	assert.Equal(t, "regcred", src.ImageRepositories[0].ImagePullSecrets[0].Name)
}

// TestBuildSoftwareVersionSource_MultiRegistrySecrets checks the secret wiring for
// a source with several registries: only one host is mapped, so its secret must
// land on that repo alone and the other repo must stay secret-free. The
// coordinates are solo-operator 0.7.3, which ships the same image (index digest
// sha256:0c3ce3ca...) on both registries, so the shared layer hashes are valid.
func TestBuildSoftwareVersionSource_MultiRegistrySecrets(t *testing.T) {
	src := &models.ImageSource{
		Repositories: []models.ImageRepositoryRef{
			// Mapped host: gets the secret.
			{Repository: "ghcr.io/hashgraph/solo-operator", ImageName: "solo-operator", ImageTag: "0.7.3"},
			// Unmapped host: no secret.
			{Repository: "artifacts.hashgraph.io/solo-operator-docker-release-local", ImageName: "solo-operator", ImageTag: "0.7.3"},
		},
		// Real layer hashes for 0.7.3 (first two layers per platform).
		LayerHashes: map[string][]string{
			"linux/amd64": {
				"sha256:1c317bff3f3e33a6b7a13902b0ef7e0e700629354685f95643921e136031253b",
				"sha256:2cc7ee286bf3a9e6af5f71756d7fc8e22e23ce65fec9047d774511ffcef79fa8",
			},
			"linux/arm64": {
				"sha256:ff1b1d6ec9ee394d02ddd22a2a44618da9813780d28b83c5859eaf67a4f9fd05",
				"sha256:401d177fd1e8a1e0d7a48139788d026e9a2e919ccb122bedd110ac7cf351f53e",
			},
		},
	}
	// Map only the private host; the public host is left unmapped.
	sel := models.PullSecretSelector{ByHost: map[string]string{"ghcr.io": "private-registry-creds"}}

	out := buildSoftwareVersionSource(src, sel)
	require.NotNil(t, out)
	require.Len(t, out.ImageRepositories, 2)

	// Private registry (ghcr.io) carries the pull secret.
	require.Len(t, out.ImageRepositories[0].ImagePullSecrets, 1)
	assert.Equal(t, "private-registry-creds", out.ImageRepositories[0].ImagePullSecrets[0].Name)
	// Public registry (artifacts.hashgraph.io) carries no secret.
	assert.Nil(t, out.ImageRepositories[1].ImagePullSecrets)

	// Both platforms are verified, in sorted order.
	require.Len(t, out.ImageVerificationSpec, 2)
	assert.Equal(t, "amd64", out.ImageVerificationSpec[0].Architecture)
	assert.Equal(t, "arm64", out.ImageVerificationSpec[1].Architecture)
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
