// SPDX-License-Identifier: Apache-2.0

// Guards against API-group drift between the GVR the UpgradeMonitor watches and
// the NetworkUpgradeExecute CRD solo-operator installs (#851). Tagged
// require_cluster; runs in Phase 3 of `test:integration:verbose`, after the CRDs
// are provisioned in Phase 2 by Test_ClusterSetup (internal/workflows/cluster_it_test.go),
// which enables SoloOperator so the solo-operator chart is deployed. Run standalone with:
//   go test -v -tags='require_cluster' -run '^TestWithCluster_UpgradeMonitor_GVRMatchesInstalledCRD$' ./internal/daemon/consensus/...

//go:build require_cluster

package consensus_test

import (
	"context"
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/internal/daemon/consensus"
	"github.com/hashgraph/solo-weaver/internal/kube"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// crdGVR addresses CustomResourceDefinition objects themselves.
var crdGVR = schema.GroupVersionResource{
	Group:    "apiextensions.k8s.io",
	Version:  "v1",
	Resource: "customresourcedefinitions",
}

// TestWithCluster_UpgradeMonitor_GVRMatchesInstalledCRD asserts the group,
// version, and resource in consensus.NetworkUpgradeExecuteGVR() match the CRD
// solo-operator installs. The CRD is located by
// plural name, not group, so the lookup still works when the group is wrong —
// the exact failure mode being detected.
func TestWithCluster_UpgradeMonitor_GVRMatchesInstalledCRD(t *testing.T) {
	c, err := kube.NewClient()
	if err != nil {
		t.Skipf("skipping: failed to create kube client: %v", err)
	}

	gvr := consensus.NetworkUpgradeExecuteGVR()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Preferred, deterministic lookup: the CRD name is "<plural>.<group>".
	crdName := gvr.Resource + "." + gvr.Group
	crd, err := c.Dyn.Resource(crdGVR).Get(ctx, crdName, metav1.GetOptions{})
	if kerrors.IsNotFound(err) {
		// Scan by plural to distinguish "CRD not installed" (skip) from
		// "installed under a different group" (fail); the exact-name lookup
		// misses both cases.
		crd = findCRDByPlural(ctx, t, c, gvr.Resource)
		if crd == nil {
			t.Skipf("skipping: no CRD with plural %q is installed (solo-operator not deployed?)", gvr.Resource)
		}
	} else if err != nil {
		t.Fatalf("failed to get CRD %q: %v", crdName, err)
	}

	// .spec.group must equal the group the monitor watches.
	specGroup, found, err := unstructured.NestedString(crd.Object, "spec", "group")
	if err != nil || !found {
		t.Fatalf("CRD %q has no .spec.group (found=%v, err=%v)", crd.GetName(), found, err)
	}
	if specGroup != gvr.Group {
		t.Fatalf("GVR group drift: monitor watches group %q but installed CRD %q has .spec.group %q — update NetworkUpgradeExecuteGVR to match the CRD",
			gvr.Group, crd.GetName(), specGroup)
	}

	// .spec.names.plural must equal the resource the monitor watches.
	specPlural, _, _ := unstructured.NestedString(crd.Object, "spec", "names", "plural")
	if specPlural != gvr.Resource {
		t.Fatalf("GVR resource drift: monitor watches resource %q but installed CRD %q has .spec.names.plural %q",
			gvr.Resource, crd.GetName(), specPlural)
	}

	// The monitor's version must be one the CRD actually serves.
	if !crdServesVersion(crd, gvr.Version) {
		t.Fatalf("GVR version drift: monitor watches version %q but installed CRD %q does not serve it",
			gvr.Version, crd.GetName())
	}
}

// findCRDByPlural scans all CRDs and returns the first whose .spec.names.plural
// matches, or nil if none is installed.
func findCRDByPlural(ctx context.Context, t *testing.T, c *kube.Client, plural string) *unstructured.Unstructured {
	t.Helper()
	list, err := c.Dyn.Resource(crdGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("failed to list CRDs: %v", err)
	}
	for i := range list.Items {
		item := list.Items[i]
		p, _, _ := unstructured.NestedString(item.Object, "spec", "names", "plural")
		if p == plural {
			return &item
		}
	}
	return nil
}

// crdServesVersion reports whether the CRD serves the given version.
func crdServesVersion(crd *unstructured.Unstructured, version string) bool {
	versions, found, err := unstructured.NestedSlice(crd.Object, "spec", "versions")
	if err != nil || !found {
		return false
	}
	for _, v := range versions {
		vm, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		name, _, _ := unstructured.NestedString(vm, "name")
		served, _, _ := unstructured.NestedBool(vm, "served")
		if name == version && served {
			return true
		}
	}
	return false
}
