// SPDX-License-Identifier: Apache-2.0

//go:build integration

package steps

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/automa-saga/automa"
	operatorv1alpha1 "github.com/hashgraph/solo-operator/api/v1alpha1"
	"github.com/hashgraph/solo-weaver/internal/blocknode"
does 	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/testutil"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/release"
	"sigs.k8s.io/yaml"
)

const legacyOnlyAnnotation = "legacy.test/adopted"

func runSteps(t *testing.T, id string, builders ...automa.Builder) {
	t.Helper()
	wf, err := automa.NewWorkflowBuilder().WithId(id).Steps(builders...).Build()
	require.NoError(t, err)
	rpt := wf.Execute(context.Background())
	require.NoError(t, rpt.Error, "%s failed", id)
	require.Equal(t, automa.StatusSuccess, rpt.Status, "%s failed", id)
}

// removeBlockNode uninstalls any block node left by earlier tests and waits until
// both the HelmCapsule and the Helm release are gone.
func removeBlockNode(t *testing.T, inputs models.BlockNodeInputs, hm helm.Manager, kc *kube.Client) {
	t.Helper()
	runSteps(t, "remove-existing-block-node", UninstallBlockNode(inputs))

	require.Eventually(t, func() bool {
		capsule, _ := kc.ResourceExists(context.Background(), kube.SoloOperatorAPIVersion, string(kube.KindHelmCapsule), inputs.Namespace, blockNodeCapsuleName(inputs))
		installed, _ := hm.IsInstalled(inputs.Release, inputs.Namespace)
		return !capsule && !installed
	}, 5*time.Minute, 5*time.Second, "block node was not removed")
}

// installLegacyBlockNode installs the block node the way pre-operator weaver did:
// a direct Helm install of the rendered values, plus a value only the legacy release has.
func installLegacyBlockNode(t *testing.T, inputs models.BlockNodeInputs, hm helm.Manager) *release.Release {
	t.Helper()
	manager, err := blocknode.NewManager(inputs)
	require.NoError(t, err)

	rendered, err := manager.ComputeValues(context.Background(), inputs.Profile, "")
	require.NoError(t, err)
	vals := map[string]any{}
	require.NoError(t, yaml.Unmarshal(rendered, &vals))
	annotations, _ := vals["podAnnotations"].(map[string]any)
	if annotations == nil {
		annotations = map[string]any{}
	}
	annotations[legacyOnlyAnnotation] = "kept"
	vals["podAnnotations"] = annotations

	out, err := yaml.Marshal(vals)
	require.NoError(t, err)
	valuesFile := filepath.Join(t.TempDir(), "legacy-values.yaml")
	require.NoError(t, os.WriteFile(valuesFile, out, 0o600))

	rel, err := hm.InstallChart(context.Background(), inputs.Release, inputs.Chart, inputs.ChartVersion, inputs.Namespace, helm.InstallChartOptions{
		ValueOpts: &values.Options{ValueFiles: []string{valuesFile}},
		Atomic:    true,
		Wait:      true,
		Timeout:   10 * time.Minute,
	})
	require.NoError(t, err)
	return rel
}

func pvcUIDs(t *testing.T, kc *kube.Client, namespace string) map[string]string {
	t.Helper()
	list, err := kc.List(context.Background(), kube.KindPVC, namespace, kube.WaitOptions{})
	require.NoError(t, err)
	uids := map[string]string{}
	for _, item := range list.Items {
		uids[item.GetName()] = string(item.GetUID())
	}
	return uids
}

func TestUpgradeBlockNode_AdoptsLegacyHelmRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping heavy cluster-creation test in short mode")
	}

	//
	// Given - a legacy, weaver-helm-managed block node and a running solo-operator
	//

	testutil.Reset(t)
	SetupPrerequisitesToLevel(t, SetupMetalLBLevel)
	runSteps(t, "install-solo-operator", InstallSoloOperator("", false))

	inputs := newTestBlockNodeInputs(models.ProfileLocal)
	inputs.ReuseValues = true
	ctx := context.Background()

	hm, err := newHelmManager()
	require.NoError(t, err)
	kc, err := kube.NewClient()
	require.NoError(t, err)

	removeBlockNode(t, inputs, hm, kc)

	getManager := newBlockNodeManagerProvider(inputs)
	runSteps(t, "legacy-block-node-prereqs",
		EnsureHederaOwnerStep(),
		setupBlockNodeStorage(getManager),
		createBlockNodeNamespace(getManager),
		createBlockNodePVs(getManager),
	)
	before := installLegacyBlockNode(t, inputs, hm)
	pvcsBefore := pvcUIDs(t, kc, inputs.Namespace)
	require.NotEmpty(t, pvcsBefore)

	exists, err := kc.ResourceExists(ctx, kube.SoloOperatorAPIVersion, string(kube.KindHelmCapsule), inputs.Namespace, blockNodeCapsuleName(inputs))
	require.NoError(t, err)
	require.False(t, exists, "a legacy release must start without a HelmCapsule")

	//
	// When - upgrade at the same version (the reconfigure path)
	//

	wf, err := UpgradeBlockNode(inputs).Build()
	require.NoError(t, err)
	report := wf.Execute(ctx)

	//
	// Then - the operator owns the same release, upgraded in place, values and storage kept
	//

	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	name := blockNodeCapsuleName(inputs)
	phase, err := kc.GetResourceNestedString(ctx, kube.SoloOperatorAPIVersion, string(kube.KindHelmCapsule), inputs.Namespace, name, "status", "phase")
	require.NoError(t, err)
	assert.Equal(t, string(operatorv1alpha1.PhaseSucceeded), phase)

	kept, err := kc.GetResourceNestedString(ctx, kube.SoloOperatorAPIVersion, string(kube.KindHelmCapsule), inputs.Namespace, name, "spec", "values", "podAnnotations", legacyOnlyAnnotation)
	require.NoError(t, err)
	assert.Equal(t, "kept", kept, "--reuse-values must carry the legacy release's values into the capsule")

	after, err := hm.GetRelease(inputs.Release, inputs.Namespace)
	require.NoError(t, err)
	assert.Equal(t, release.StatusDeployed, after.Info.Status)
	assert.Greater(t, after.Version, before.Version, "adoption must upgrade the release, not reinstall it")
	assert.Equal(t, before.Info.FirstDeployed.Unix(), after.Info.FirstDeployed.Unix(), "adoption must not reinstall the release")
	assert.Equal(t, inputs.ChartVersion, after.Chart.Metadata.Version)

	assert.Equal(t, pvcsBefore, pvcUIDs(t, kc, inputs.Namespace), "adoption must not recreate the PVCs")
}
