// SPDX-License-Identifier: Apache-2.0

// Integration tests for the Kubernetes client that require a running cluster.
//
// Build Tag: require_cluster
//
// These tests are NOT part of the standard `integration` test suite.
// They run in Phase 2 of the Taskfile `test:integration:verbose` task,
// after the cluster has been created in Phase 1:
//
//   Phase 1: go test -tags='cluster_setup' -run '^Test_ClusterSetup$' ./internal/workflows/...
//            → Creates the Kubernetes cluster
//
//   Phase 2: go test -tags='require_cluster' ./...
//            → Runs these tests (and other cluster-dependent tests like helm tests)
//
//   Phase 3: go test -tags='cluster_setup' ./internal/workflows/...
//            → Tears down the cluster
//
//   Phase 4: go test -tags='integration' ./...
//            → Runs general integration tests
//
// Dependencies:
//   - Requires a running Kubernetes cluster (created by Phase 1)
//   - Requires valid kubeconfig (typically at /etc/kubernetes/admin.conf)
//
// To run these tests standalone (with an existing cluster):
//   go test -v -tags='require_cluster' ./internal/kube/...

//go:build require_cluster

package kube

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// mustClient returns a Client or skips the test if it cannot be created
func mustClient(t *testing.T) *Client {
	t.Helper()
	c, err := NewClient()
	if err != nil {
		t.Skipf("skipping integration test; failed to create kube client: %v", err)
	}
	return c
}

// createUnstructured simplifies unstructured object creation
func createUnstructured(kind, apiVersion, ns, name string, spec map[string]interface{}) *unstructured.Unstructured {
	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata": map[string]interface{}{
				"name": name,
			},
		},
	}
	if ns != "" {
		unstructured.SetNestedField(u.Object, ns, "metadata", "namespace")
	}
	if spec != nil {
		for k, v := range spec {
			u.Object[k] = v
		}
	}
	return u
}

func createPodUnstructured(ns, name, cmd string, labels map[string]interface{}) *unstructured.Unstructured {
	if labels == nil {
		labels = map[string]interface{}{
			"app": name,
		}
	} else if _, hasApp := labels["app"]; !hasApp {
		labels["app"] = name
	}

	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": ns,
				"labels":    labels,
			},
			"spec": map[string]interface{}{
				"restartPolicy": "Never",
				"containers": []interface{}{
					map[string]interface{}{
						"name":    "c",
						"image":   "busybox",
						"command": []interface{}{"sh", "-c", cmd},
					},
				},
			},
		},
	}
}

// createAndWait creates a resource and waits for a given condition
func createAndWait(t *testing.T, c *Client, gvr schema.GroupVersionResource, ns string, obj *unstructured.Unstructured, check CheckFunc, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var dr dynamic.ResourceInterface
	if ns != "" {
		dr = c.Dyn.Resource(gvr).Namespace(ns)
	} else {
		dr = c.Dyn.Resource(gvr)
	}

	if _, err := dr.Create(ctx, obj, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create %s/%s: %v", gvr.Resource, obj.GetName(), err)
	}

	if err := c.WaitForResources(ctx, ToResourceKind(gvr), ns, check, timeout, WaitOptions{NamePrefix: obj.GetName()}); err != nil {
		t.Fatalf("%s/%s did not reach desired state: %v", gvr.Resource, obj.GetName(), err)
	}
}

// deleteAndWait deletes a resource and waits for its deletion
func deleteAndWait(t *testing.T, c *Client, gvr schema.GroupVersionResource, ns, name string, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var dr dynamic.ResourceInterface
	if ns != "" {
		dr = c.Dyn.Resource(gvr).Namespace(ns)
	} else {
		dr = c.Dyn.Resource(gvr)
	}

	_ = dr.Delete(ctx, name, metav1.DeleteOptions{})
	if err := c.WaitForResource(ctx, ToResourceKind(gvr), ns, name, IsDeleted, timeout); err != nil {
		t.Logf("Warning: resource %s/%s may not have been deleted: %v", gvr.Resource, name, err)
	}
}

func TestApplyAndDeleteManifest_Integration(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	c := mustClient(t)

	// Create a unique namespace for isolation
	nsName := fmt.Sprintf("it-apply-delete-%d", time.Now().UnixNano())
	defer func() {
		deleteAndWait(t, c, schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}, "", nsName, 2*time.Minute)
	}()

	cmName := "cm-it-sample"

	// Temporary manifest file
	manifest := fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: %s
  namespace: %s
data:
  hello: world
`, nsName, cmName, nsName)
	path, cleanup := writeTempManifest(t, manifest)
	defer cleanup()

	// Apply manifest
	if err := c.ApplyManifest(ctx, path); err != nil {
		t.Fatalf("ApplyManifest: %v", err)
	}

	// Wait for Namespace
	if err := c.WaitForResource(ctx, KindNamespace, "", nsName, IsPresent, 30*time.Second); err != nil {
		t.Fatalf("waiting for namespace %s: %v", nsName, err)
	}

	// Wait for ConfigMap
	if err := c.WaitForResource(ctx, KindConfigMap, nsName, cmName, IsPresent, 30*time.Second); err != nil {
		t.Fatalf("waiting for configmap %s/%s: %v", nsName, cmName, err)
	}

	// Delete manifest
	if err := c.DeleteManifest(ctx, path); err != nil {
		t.Fatalf("DeleteManifest: %v", err)
	}
}

// TestWaitForResources tests Deployments, Jobs, Pods, and PVCs
func TestWaitForResources(t *testing.T) {
	t.Parallel()
	c := mustClient(t)
	ctx := context.Background()

	// Create unique namespace for isolation
	nsName := fmt.Sprintf("it-waitfor-%d", time.Now().UnixNano())
	nsGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
	nsObj := createUnstructured("Namespace", "v1", "", nsName, nil)
	createAndWait(t, c, nsGVR, "", nsObj, IsPhase("Active"), 1*time.Minute)
	defer deleteAndWait(t, c, nsGVR, "", nsName, 2*time.Minute)

	// --- Deployment ---
	deployName := "deploy-it"
	deployGVR := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	deployObj := createUnstructured("Deployment", "apps/v1", nsName, deployName, map[string]interface{}{
		"spec": map[string]interface{}{
			"replicas": int64(1),
			"selector": map[string]interface{}{
				"matchLabels": map[string]interface{}{"app": deployName},
			},
			"template": map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{"app": deployName},
				},
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"name":  "nginx",
							"image": "nginx:1.21",
						},
					},
				},
			},
		},
	})
	createAndWait(t, c, deployGVR, nsName, deployObj, IsDeploymentReady, 2*time.Minute)
	defer deleteAndWait(t, c, deployGVR, nsName, deployName, 2*time.Minute)

	// --- Job ---
	jobName := "job-it"
	jobGVR := schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}
	jobObj := createUnstructured("Job", "batch/v1", nsName, jobName, map[string]interface{}{
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"restartPolicy": "Never",
					"containers": []interface{}{
						map[string]interface{}{
							"name":    "job",
							"image":   "busybox",
							"command": []interface{}{"sh", "-c", "sleep 1; exit 0"},
						},
					},
				},
			},
		},
	})
	createAndWait(t, c, jobGVR, nsName, jobObj, IsJobComplete, 1*time.Minute)
	defer deleteAndWait(t, c, jobGVR, nsName, jobName, 2*time.Minute)

	// --- Pod ---
	podName := "pod-it"
	podGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	podObj := createUnstructured("Pod", "v1", nsName, podName, map[string]interface{}{
		"spec": map[string]interface{}{
			"restartPolicy": "Never",
			"containers": []interface{}{
				map[string]interface{}{
					"name":    "c",
					"image":   "busybox",
					"command": []interface{}{"sh", "-c", "sleep 1; exit 0"},
				},
			},
		},
	})
	createAndWait(t, c, podGVR, nsName, podObj, IsPodReady, 1*time.Minute)
	defer deleteAndWait(t, c, podGVR, nsName, podName, 2*time.Minute)

	// Wait for container completion
	if err := c.WaitForContainer(ctx, nsName, IsContainerReady("c"), 1*time.Minute, WaitOptions{
		NamePrefix: podName,
	}); err != nil {
		t.Fatalf("WaitForContainer failed: %v", err)
	}

	// --- PVC ---
	storageGVR := schema.GroupVersionResource{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses"}
	scList, err := c.Dyn.Resource(storageGVR).List(ctx, metav1.ListOptions{})
	if err != nil || len(scList.Items) == 0 {
		t.Log("no StorageClass found, skipping PVC test")
		return
	}
	scName := scList.Items[0].GetName()
	pvcName := "pvc-it"
	pvcGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "persistentvolumeclaims"}
	pvcObj := createUnstructured("PersistentVolumeClaim", "v1", nsName, pvcName, map[string]interface{}{
		"spec": map[string]interface{}{
			"storageClassName": scName,
			"accessModes":      []interface{}{"ReadWriteOnce"},
			"resources": map[string]interface{}{
				"requests": map[string]interface{}{"storage": "1Mi"},
			},
		},
	})
	createAndWait(t, c, pvcGVR, nsName, pvcObj, IsPhase(PhasePending), 2*time.Minute)
	defer deleteAndWait(t, c, pvcGVR, nsName, pvcName, 2*time.Minute)
}

func TestWaitForContainer_Succeeds(t *testing.T) {
	c := mustClient(t)
	ctx := context.Background()

	// Create unique namespace for isolation
	nsName := fmt.Sprintf("it-waitfor-%d", time.Now().UnixNano())
	nsGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
	nsObj := createUnstructured("Namespace", "v1", "", nsName, nil)
	createAndWait(t, c, nsGVR, "", nsObj, IsPhase("Active"), 1*time.Minute)
	defer deleteAndWait(t, c, nsGVR, "", nsName, 2*time.Minute)

	podName := "test-waitforcontainer-success"
	podGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}

	podObj := createPodUnstructured(nsName, podName, "sleep 1; exit 0", nil)
	createAndWait(t, c, podGVR, nsName, podObj, IsPodReady, 30*time.Second)
	defer deleteAndWait(t, c, podGVR, nsName, podName, 1*time.Minute)

	if err := c.WaitForContainer(ctx, nsName, IsContainerReady("c"), 60*time.Second, WaitOptions{NamePrefix: podName}); err != nil {
		t.Fatalf("expected WaitForContainer to succeed, got: %v", err)
	}
}

func TestWaitForContainer_TerminatedNonZero(t *testing.T) {
	c := mustClient(t)
	ctx := context.Background()

	// Create unique namespace for isolation
	nsName := fmt.Sprintf("it-waitfor-%d", time.Now().UnixNano())
	nsGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
	nsObj := createUnstructured("Namespace", "v1", "", nsName, nil)
	createAndWait(t, c, nsGVR, "", nsObj, IsPhase("Active"), 1*time.Minute)
	defer deleteAndWait(t, c, nsGVR, "", nsName, 2*time.Minute)

	podName := "test-waitforcontainer-fail"
	podGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}

	podObj := createPodUnstructured(nsName, podName, "sleep 1; exit 2", nil)
	createAndWait(t, c, podGVR, nsName, podObj, IsPodReady, 30*time.Second)
	defer deleteAndWait(t, c, podGVR, nsName, podName, 1*time.Minute)

	if err := c.WaitForContainer(ctx, nsName,
		IsContainerTerminated("c", 2), 60*time.Second, WaitOptions{NamePrefix: podName}); err != nil {
		t.Fatalf("expected error for container terminated with exit code 2, got error: %v", err)
	}
}

func TestList_Namespace_Label_FieldSelectors(t *testing.T) {
	t.Parallel()
	c := mustClient(t)

	ctx := context.Background()
	nsGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
	podGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}

	// create two namespaces
	nsA := fmt.Sprintf("it-list-a-%d", time.Now().UnixNano())
	nsB := fmt.Sprintf("it-list-b-%d", time.Now().UnixNano())

	nsAObj := createUnstructured("Namespace", "v1", "", nsA, nil)
	createAndWait(t, c, nsGVR, "", nsAObj, IsPhase("Active"), 1*time.Minute)
	defer deleteAndWait(t, c, nsGVR, "", nsA, 2*time.Minute)

	nsBObj := createUnstructured("Namespace", "v1", "", nsB, nil)
	createAndWait(t, c, nsGVR, "", nsBObj, IsPhase("Active"), 1*time.Minute)
	defer deleteAndWait(t, c, nsGVR, "", nsB, 2*time.Minute)

	// create pods:
	// - podA1 (nsA) label test=foo
	// - podA2 (nsA) label test=bar
	// - podB1 (nsB) label test=foo
	podA1 := createPodUnstructured(nsA, "pod-a1", "sleep 300", map[string]interface{}{"test": "foo"})
	createAndWait(t, c, podGVR, nsA, podA1, IsPodReady, 2*time.Minute)
	defer deleteAndWait(t, c, podGVR, nsA, "pod-a1", 1*time.Minute)

	podA2 := createPodUnstructured(nsA, "pod-a2", "sleep 300", map[string]interface{}{"test": "bar"})
	createAndWait(t, c, podGVR, nsA, podA2, IsPodReady, 2*time.Minute)
	defer deleteAndWait(t, c, podGVR, nsA, "pod-a2", 1*time.Minute)

	podB1 := createPodUnstructured(nsB, "pod-b1", "sleep 300", map[string]interface{}{"test": "foo"})
	createAndWait(t, c, podGVR, nsB, podB1, IsPodReady, 2*time.Minute)
	defer deleteAndWait(t, c, podGVR, nsB, "pod-b1", 1*time.Minute)

	// 1) Namespace filtering: list pods in nsA -> expect pod-a1 and pod-a2 only
	items, err := c.List(ctx, KindPod, nsA, WaitOptions{})
	if err != nil {
		t.Fatalf("List by namespace failed: %v", err)
	}
	names := map[string]struct{}{}
	for _, it := range items.Items {
		names[it.GetName()] = struct{}{}
	}
	if _, ok := names["pod-a1"]; !ok {
		t.Fatalf("expected pod-a1 in namespace list, got: %v", names)
	}
	if _, ok := names["pod-a2"]; !ok {
		t.Fatalf("expected pod-a2 in namespace list, got: %v", names)
	}
	if _, ok := names["pod-b1"]; ok {
		t.Fatalf("did not expect pod-b1 in namespace %s list", nsA)
	}

	// 2) LabelSelector: in nsA, select app=foo -> expect only pod-a1
	itemsLbl, err := c.List(ctx, KindPod, nsA, WaitOptions{LabelSelector: "test=foo"})
	if err != nil {
		t.Fatalf("List with label selector failed: %v", err)
	}
	if len(itemsLbl.Items) != 1 || itemsLbl.Items[0].GetName() != "pod-a1" {
		t.Fatalf("label selector returned unexpected items: %v", itemsLbl)
	}

	// 3) FieldSelector: in nsA, select metadata.name=pod-a1 -> expect only pod-a1
	itemsField, err := c.List(ctx, KindPod, nsA, WaitOptions{FieldSelector: "metadata.name=pod-a1"})
	if err != nil {
		t.Fatalf("List with field selector failed: %v", err)
	}
	if len(itemsField.Items) != 1 || itemsField.Items[0].GetName() != "pod-a1" {
		t.Fatalf("field selector returned unexpected items: %v", itemsField)
	}
}

func TestClusterNodes_Integration(t *testing.T) {
	t.Parallel()
	c := mustClient(t)
	ctx := context.Background()

	// Wait until all discovered nodes are Ready
	if err := c.WaitForResources(ctx, KindNode, "", IsNodeReady, 30*time.Second, WaitOptions{}); err != nil {
		t.Fatalf("waiting for nodes to be ready: %v", err)
	}

	// List nodes and assert there's at least one
	items, err := c.List(ctx, KindNode, "", WaitOptions{})
	if err != nil {
		t.Fatalf("List nodes failed: %v", err)
	}
	if len(items.Items) == 0 {
		t.Fatalf("expected at least one cluster node, got 0")
	}

	names := make([]string, 0, len(items.Items))
	for _, it := range items.Items {
		names = append(names, it.GetName())
	}
	t.Logf("Cluster nodes: %v", names)
}

func TestToGroupVersionResource_KnownAndFallback(t *testing.T) {
	t.Parallel()

	// Known kind
	gvr, err := ToGroupVersionResource(KindNode)
	if err != nil {
		t.Fatalf("ToGroupVersionResource(KindNode) returned error: %v", err)
	}
	if gvr.Resource != "nodes" || gvr.Version != "v1" || gvr.Group != "" {
		t.Fatalf("unexpected GVR for Node: %v", gvr)
	}

	// Unknown kind -> error
	_, err = ToGroupVersionResource(ResourceKind("UnknownKindX"))
	if err == nil {
		t.Fatalf("expected error for unknown kind")
	}

	// ToResourceKind fallback for unknown GVR
	rk := ToResourceKind(schema.GroupVersionResource{Group: "custom", Version: "v1", Resource: "things"})
	if string(rk) != "things" {
		t.Fatalf("ToResourceKind fallback expected 'things', got %q", rk)
	}
}

func TestCheckFuncWrappers_IsPhaseAndContainerFuncs(t *testing.T) {
	t.Parallel()

	// IsPhase
	objRunning := &unstructured.Unstructured{Object: map[string]interface{}{
		"status": map[string]interface{}{"phase": PhaseRunning.String()},
	}}
	ok, err := IsPhase(PhaseRunning)(objRunning, nil)
	if err != nil || !ok {
		t.Fatalf("IsPhase should return true for Running")
	}
	ok, err = IsPhase(PhasePending)(objRunning, nil)
	if err != nil || ok {
		t.Fatalf("IsPhase should return false for non-matching phase")
	}

	// IsContainerReady
	pod := &unstructured.Unstructured{Object: map[string]interface{}{
		"status": map[string]interface{}{
			"containerStatuses": []interface{}{
				map[string]interface{}{"name": "c", "ready": true},
			},
		},
	}}
	ok, err = IsContainerReady("c")(pod, nil)
	if err != nil || !ok {
		t.Fatalf("IsContainerReady expected true but got ok=%v err=%v", ok, err)
	}

	// IsContainerTerminated success and failure cases
	podTerm := &unstructured.Unstructured{Object: map[string]interface{}{
		"status": map[string]interface{}{
			"containerStatuses": []interface{}{
				map[string]interface{}{
					"name": "c",
					"state": map[string]interface{}{
						"terminated": map[string]interface{}{"exitCode": int64(2)},
					},
				},
			},
		},
	}}
	ok, err = IsContainerTerminated("c", 2)(podTerm, nil)
	if err != nil || !ok {
		t.Fatalf("IsContainerTerminated expected success for matching exit code, got ok=%v err=%v", ok, err)
	}
	_, err = IsContainerTerminated("c", 3)(podTerm, nil)
	if err == nil {
		t.Fatalf("IsContainerTerminated expected error for non-matching exit code")
	}
}

func TestIsCRDReady(t *testing.T) {
	t.Parallel()

	objEstablished := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{"name": "crd-established"},
			"status": map[string]interface{}{
				"conditions": []interface{}{
					map[string]interface{}{"type": "Established", "status": "True"},
				},
			},
		},
	}

	objNoConditions := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{"name": "crd-nocond"},
		},
	}

	objNamesNotAccepted := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{"name": "crd-badnames"},
			"status": map[string]interface{}{
				"conditions": []interface{}{
					map[string]interface{}{"type": "NamesAccepted", "status": "False", "reason": "BadName", "message": "invalid"},
				},
			},
		},
	}

	objNamesNotAcceptedEmpty := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{"name": "crd-badnames-empty"},
			"status": map[string]interface{}{
				"conditions": []interface{}{
					map[string]interface{}{"type": "NamesAccepted", "status": "False"},
				},
			},
		},
	}

	notFoundErr := kerrors.NewNotFound(schema.GroupResource{Group: "apiextensions.k8s.io", Resource: "customresourcedefinitions"}, "missing")

	tests := []struct {
		name     string
		obj      *unstructured.Unstructured
		err      error
		wantOk   bool
		wantErr  bool
		msgMatch string
	}{
		{"established", objEstablished, nil, true, false, ""},
		{"no-conditions", objNoConditions, nil, false, false, ""},
		{"names-not-accepted-with-reason", objNamesNotAccepted, nil, false, true, "BadName"},
		{"names-not-accepted-empty-reason", objNamesNotAcceptedEmpty, nil, false, true, "names not accepted"},
		{"not-found-error", nil, notFoundErr, false, false, ""},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ok, err := IsCRDReady(tt.obj, tt.err)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.msgMatch != "" && !strings.Contains(err.Error(), tt.msgMatch) {
					t.Fatalf("error message %q does not contain %q", err.Error(), tt.msgMatch)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			if ok != tt.wantOk {
				t.Fatalf("expected ok=%v, got %v", tt.wantOk, ok)
			}
		})
	}
}

// TestScaleDeployment_Integration tests scaling a deployment up and down
func TestScaleDeployment_Integration(t *testing.T) {
	t.Parallel()
	c := mustClient(t)
	ctx := context.Background()

	// Create unique namespace for isolation
	nsName := fmt.Sprintf("it-scale-%d", time.Now().UnixNano())
	nsGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
	nsObj := createUnstructured("Namespace", "v1", "", nsName, nil)
	createAndWait(t, c, nsGVR, "", nsObj, IsPhase("Active"), 1*time.Minute)
	defer deleteAndWait(t, c, nsGVR, "", nsName, 2*time.Minute)

	// Create a deployment with 1 replica
	deployName := "deploy-scale-test"
	deployGVR := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	deployObj := createUnstructured("Deployment", "apps/v1", nsName, deployName, map[string]interface{}{
		"spec": map[string]interface{}{
			"replicas": int64(1),
			"selector": map[string]interface{}{
				"matchLabels": map[string]interface{}{"app": deployName},
			},
			"template": map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{"app": deployName},
				},
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"name":  "nginx",
							"image": "nginx:1.21",
						},
					},
				},
			},
		},
	})
	createAndWait(t, c, deployGVR, nsName, deployObj, IsDeploymentReady, 2*time.Minute)
	defer deleteAndWait(t, c, deployGVR, nsName, deployName, 2*time.Minute)

	// Scale down to 0
	if err := c.ScaleDeployment(ctx, nsName, deployName, 0); err != nil {
		t.Fatalf("ScaleDeployment to 0 failed: %v", err)
	}

	// Verify replicas is 0
	deploy, err := c.Dyn.Resource(deployGVR).Namespace(nsName).Get(ctx, deployName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get deployment: %v", err)
	}
	replicas, _, _ := unstructured.NestedInt64(deploy.Object, "spec", "replicas")
	if replicas != 0 {
		t.Fatalf("expected 0 replicas, got %d", replicas)
	}

	// Scale back up to 2
	if err := c.ScaleDeployment(ctx, nsName, deployName, 2); err != nil {
		t.Fatalf("ScaleDeployment to 2 failed: %v", err)
	}

	// Verify replicas is 2
	deploy, err = c.Dyn.Resource(deployGVR).Namespace(nsName).Get(ctx, deployName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get deployment: %v", err)
	}
	replicas, _, _ = unstructured.NestedInt64(deploy.Object, "spec", "replicas")
	if replicas != 2 {
		t.Fatalf("expected 2 replicas, got %d", replicas)
	}
}

// TestScaleDeployment_NotFound tests scaling a non-existent deployment
func TestScaleDeployment_NotFound(t *testing.T) {
	t.Parallel()
	c := mustClient(t)
	ctx := context.Background()

	err := c.ScaleDeployment(ctx, "default", "nonexistent-deployment", 1)
	if err == nil {
		t.Fatal("expected error for non-existent deployment, got nil")
	}
}

// TestAnnotateResource_Service_Integration tests annotating a service
func TestAnnotateResource_Service_Integration(t *testing.T) {
	t.Parallel()
	c := mustClient(t)
	ctx := context.Background()

	// Create unique namespace for isolation
	nsName := fmt.Sprintf("it-annotate-%d", time.Now().UnixNano())
	nsGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
	nsObj := createUnstructured("Namespace", "v1", "", nsName, nil)
	createAndWait(t, c, nsGVR, "", nsObj, IsPhase("Active"), 1*time.Minute)
	defer deleteAndWait(t, c, nsGVR, "", nsName, 2*time.Minute)

	// Create a service
	svcName := "svc-annotate-test"
	svcGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}
	svcObj := createUnstructured("Service", "v1", nsName, svcName, map[string]interface{}{
		"spec": map[string]interface{}{
			"selector": map[string]interface{}{"app": "test"},
			"ports": []interface{}{
				map[string]interface{}{
					"port":       int64(80),
					"targetPort": int64(80),
				},
			},
		},
	})

	dr := c.Dyn.Resource(svcGVR).Namespace(nsName)
	if _, err := dr.Create(ctx, svcObj, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	defer deleteAndWait(t, c, svcGVR, nsName, svcName, 1*time.Minute)

	// Annotate the service
	annotations := map[string]string{
		"test.io/annotation": "value1",
		"metallb.io/pool":    "test-pool",
	}
	if err := c.AnnotateResource(ctx, KindService, nsName, svcName, annotations); err != nil {
		t.Fatalf("AnnotateResource failed: %v", err)
	}

	// Verify annotations were added
	svc, err := dr.Get(ctx, svcName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get service: %v", err)
	}
	gotAnnotations := svc.GetAnnotations()
	if gotAnnotations["test.io/annotation"] != "value1" {
		t.Errorf("expected annotation 'test.io/annotation=value1', got %v", gotAnnotations)
	}
	if gotAnnotations["metallb.io/pool"] != "test-pool" {
		t.Errorf("expected annotation 'metallb.io/pool=test-pool', got %v", gotAnnotations)
	}

	// Add another annotation (should merge with existing)
	if err := c.AnnotateResource(ctx, KindService, nsName, svcName, map[string]string{"new/annotation": "new-value"}); err != nil {
		t.Fatalf("AnnotateResource (merge) failed: %v", err)
	}

	svc, err = dr.Get(ctx, svcName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get service after merge: %v", err)
	}
	gotAnnotations = svc.GetAnnotations()
	// Original annotations should still be present
	if gotAnnotations["test.io/annotation"] != "value1" {
		t.Errorf("original annotation missing after merge")
	}
	// New annotation should be added
	if gotAnnotations["new/annotation"] != "new-value" {
		t.Errorf("new annotation not found after merge")
	}
}

// TestAnnotateResource_ConfigMap_Integration tests annotating a configmap
func TestAnnotateResource_ConfigMap_Integration(t *testing.T) {
	t.Parallel()
	c := mustClient(t)
	ctx := context.Background()

	// Create unique namespace for isolation
	nsName := fmt.Sprintf("it-annotate-cm-%d", time.Now().UnixNano())
	nsGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
	nsObj := createUnstructured("Namespace", "v1", "", nsName, nil)
	createAndWait(t, c, nsGVR, "", nsObj, IsPhase("Active"), 1*time.Minute)
	defer deleteAndWait(t, c, nsGVR, "", nsName, 2*time.Minute)

	// Create a configmap
	cmName := "cm-annotate-test"
	cmGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	cmObj := createUnstructured("ConfigMap", "v1", nsName, cmName, map[string]interface{}{
		"data": map[string]interface{}{
			"key": "value",
		},
	})

	dr := c.Dyn.Resource(cmGVR).Namespace(nsName)
	if _, err := dr.Create(ctx, cmObj, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create configmap: %v", err)
	}
	defer deleteAndWait(t, c, cmGVR, nsName, cmName, 1*time.Minute)

	// Annotate the configmap
	annotations := map[string]string{
		"config.io/version": "v1.0.0",
	}
	if err := c.AnnotateResource(ctx, KindConfigMap, nsName, cmName, annotations); err != nil {
		t.Fatalf("AnnotateResource failed: %v", err)
	}

	// Verify annotations were added
	cm, err := dr.Get(ctx, cmName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get configmap: %v", err)
	}
	gotAnnotations := cm.GetAnnotations()
	if gotAnnotations["config.io/version"] != "v1.0.0" {
		t.Errorf("expected annotation 'config.io/version=v1.0.0', got %v", gotAnnotations)
	}
}

// TestAnnotateResource_NotFound tests annotating a non-existent resource
func TestAnnotateResource_NotFound(t *testing.T) {
	t.Parallel()
	c := mustClient(t)
	ctx := context.Background()

	err := c.AnnotateResource(ctx, KindService, "default", "nonexistent-service", map[string]string{"key": "value"})
	if err == nil {
		t.Fatal("expected error for non-existent resource, got nil")
	}
}

// TestScaleStatefulSet_Integration tests scaling a statefulset up and down
func TestScaleStatefulSet_Integration(t *testing.T) {
	t.Parallel()
	c := mustClient(t)
	ctx := context.Background()

	// Create unique namespace for isolation
	nsName := fmt.Sprintf("it-scale-sts-%d", time.Now().UnixNano())
	nsGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
	nsObj := createUnstructured("Namespace", "v1", "", nsName, nil)
	createAndWait(t, c, nsGVR, "", nsObj, IsPhase("Active"), 1*time.Minute)
	defer deleteAndWait(t, c, nsGVR, "", nsName, 2*time.Minute)

	// Create a statefulset with 1 replica
	stsName := "sts-scale-test"
	stsGVR := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}
	stsObj := createUnstructured("StatefulSet", "apps/v1", nsName, stsName, map[string]interface{}{
		"spec": map[string]interface{}{
			"replicas":    int64(1),
			"serviceName": stsName,
			"selector": map[string]interface{}{
				"matchLabels": map[string]interface{}{"app": stsName},
			},
			"template": map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{"app": stsName},
				},
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"name":  "nginx",
							"image": "nginx:1.21",
						},
					},
				},
			},
		},
	})

	// Create headless service required for statefulset
	svcGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}
	svcObj := createUnstructured("Service", "v1", nsName, stsName, map[string]interface{}{
		"spec": map[string]interface{}{
			"clusterIP": "None",
			"selector":  map[string]interface{}{"app": stsName},
			"ports": []interface{}{
				map[string]interface{}{
					"port":       int64(80),
					"targetPort": int64(80),
				},
			},
		},
	})
	svcDr := c.Dyn.Resource(svcGVR).Namespace(nsName)
	if _, err := svcDr.Create(ctx, svcObj, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create headless service: %v", err)
	}
	defer deleteAndWait(t, c, svcGVR, nsName, stsName, 1*time.Minute)

	// Create statefulset
	stsDr := c.Dyn.Resource(stsGVR).Namespace(nsName)
	if _, err := stsDr.Create(ctx, stsObj, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create statefulset: %v", err)
	}
	defer deleteAndWait(t, c, stsGVR, nsName, stsName, 2*time.Minute)

	// Wait for statefulset to be ready (at least 1 ready replica), instead of using a fixed sleep
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for {
		select {
		case <-waitCtx.Done():
			t.Fatalf("timed out waiting for statefulset %s/%s to have at least 1 ready replica: %v", nsName, stsName, waitCtx.Err())
		default:
		}

		sts, err := stsDr.Get(waitCtx, stsName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to get statefulset while waiting for readiness: %v", err)
		}

		readyReplicas, _, _ := unstructured.NestedInt64(sts.Object, "status", "readyReplicas")
		if readyReplicas >= 1 {
			break
		}

		time.Sleep(2 * time.Second)
	}
	// Scale down to 0
	if err := c.ScaleStatefulSet(ctx, nsName, stsName, 0); err != nil {
		t.Fatalf("ScaleStatefulSet to 0 failed: %v", err)
	}

	// Verify replicas is 0
	sts, err := stsDr.Get(ctx, stsName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get statefulset: %v", err)
	}
	replicas, _, _ := unstructured.NestedInt64(sts.Object, "spec", "replicas")
	if replicas != 0 {
		t.Fatalf("expected 0 replicas, got %d", replicas)
	}

	// Scale back up to 2
	if err := c.ScaleStatefulSet(ctx, nsName, stsName, 2); err != nil {
		t.Fatalf("ScaleStatefulSet to 2 failed: %v", err)
	}

	// Verify replicas is 2
	sts, err = stsDr.Get(ctx, stsName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get statefulset: %v", err)
	}
	replicas, _, _ = unstructured.NestedInt64(sts.Object, "spec", "replicas")
	if replicas != 2 {
		t.Fatalf("expected 2 replicas, got %d", replicas)
	}
}

// TestScaleStatefulSet_NotFound tests scaling a non-existent statefulset
func TestScaleStatefulSet_NotFound(t *testing.T) {
	t.Parallel()
	c := mustClient(t)
	ctx := context.Background()

	err := c.ScaleStatefulSet(ctx, "default", "nonexistent-statefulset", 1)
	if err == nil {
		t.Fatal("expected error for non-existent statefulset, got nil")
	}
}

// TestWaitForResourcesDeletion_Integration tests waiting for pods to be deleted
func TestWaitForResourcesDeletion_Integration(t *testing.T) {
	t.Parallel()
	c := mustClient(t)
	ctx := context.Background()

	// Create unique namespace for isolation
	nsName := fmt.Sprintf("it-wait-delete-%d", time.Now().UnixNano())
	nsGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
	nsObj := createUnstructured("Namespace", "v1", "", nsName, nil)
	createAndWait(t, c, nsGVR, "", nsObj, IsPhase("Active"), 1*time.Minute)
	defer deleteAndWait(t, c, nsGVR, "", nsName, 2*time.Minute)

	// Create a pod
	podName := "pod-delete-test"
	podGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	podObj := createPodUnstructured(nsName, podName, "sleep 300", map[string]interface{}{"test": "deletion"})
	createAndWait(t, c, podGVR, nsName, podObj, IsPodReady, 2*time.Minute)

	// Start deletion in background
	go func() {
		time.Sleep(2 * time.Second)
		_ = c.Dyn.Resource(podGVR).Namespace(nsName).Delete(ctx, podName, metav1.DeleteOptions{})
	}()

	// Wait for pod deletion
	opts := WaitOptions{LabelSelector: "test=deletion"}
	err := c.WaitForResourcesDeletion(ctx, KindPod, nsName, 1*time.Minute, opts)
	if err != nil {
		t.Fatalf("WaitForResourcesDeletion failed: %v", err)
	}

	// Verify pod is gone
	_, err = c.Dyn.Resource(podGVR).Namespace(nsName).Get(ctx, podName, metav1.GetOptions{})
	if !kerrors.IsNotFound(err) {
		t.Fatalf("expected NotFound error, got: %v", err)
	}
}

// TestWaitForResourcesDeletion_AlreadyDeleted tests waiting when no matching resources exist
func TestWaitForResourcesDeletion_AlreadyDeleted(t *testing.T) {
	t.Parallel()
	c := mustClient(t)
	ctx := context.Background()

	// Wait for pods with a label that doesn't exist - should return immediately
	opts := WaitOptions{LabelSelector: "nonexistent-label=nonexistent-value"}
	err := c.WaitForResourcesDeletion(ctx, KindPod, "default", 5*time.Second, opts)
	if err != nil {
		t.Fatalf("WaitForResourcesDeletion should succeed when no resources match: %v", err)
	}
}

// TestResourceExists_NamespaceExists tests ResourceExists with an existing namespace
func TestResourceExists_NamespaceExists(t *testing.T) {
	t.Parallel()
	c := mustClient(t)
	ctx := context.Background()

	// The "default" namespace should always exist
	exists, err := c.ResourceExists(ctx, "v1", "Namespace", "", "default")
	if err != nil {
		t.Fatalf("ResourceExists failed: %v", err)
	}
	if !exists {
		t.Fatalf("expected 'default' namespace to exist")
	}
}

// TestResourceExists_NamespaceNotExists tests ResourceExists with a non-existent namespace
func TestResourceExists_NamespaceNotExists(t *testing.T) {
	t.Parallel()
	c := mustClient(t)
	ctx := context.Background()

	// This namespace should not exist
	exists, err := c.ResourceExists(ctx, "v1", "Namespace", "", "nonexistent-namespace-12345")
	if err != nil {
		t.Fatalf("ResourceExists failed: %v", err)
	}
	if exists {
		t.Fatalf("expected namespace to not exist")
	}
}

// TestResourceExists_ConfigMap tests ResourceExists with a namespaced resource
func TestResourceExists_ConfigMap(t *testing.T) {
	t.Parallel()
	c := mustClient(t)
	ctx := context.Background()

	nsName := fmt.Sprintf("test-resourceexists-cm-%d", time.Now().UnixNano())

	// Create test namespace
	ns := createUnstructured("Namespace", "v1", "", nsName, nil)
	nsGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
	_, err := c.Dyn.Resource(nsGVR).Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create namespace: %v", err)
	}
	t.Cleanup(func() {
		_ = c.Dyn.Resource(nsGVR).Delete(context.Background(), nsName, metav1.DeleteOptions{})
	})

	cmName := "test-configmap"
	cmGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}

	// ConfigMap should not exist yet
	exists, err := c.ResourceExists(ctx, "v1", "ConfigMap", nsName, cmName)
	if err != nil {
		t.Fatalf("ResourceExists failed: %v", err)
	}
	if exists {
		t.Fatalf("expected ConfigMap to not exist")
	}

	// Create ConfigMap
	cm := createUnstructured("ConfigMap", "v1", nsName, cmName, map[string]interface{}{
		"data": map[string]interface{}{
			"key": "value",
		},
	})
	_, err = c.Dyn.Resource(cmGVR).Namespace(nsName).Create(ctx, cm, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create ConfigMap: %v", err)
	}

	// ConfigMap should now exist
	exists, err = c.ResourceExists(ctx, "v1", "ConfigMap", nsName, cmName)
	if err != nil {
		t.Fatalf("ResourceExists failed: %v", err)
	}
	if !exists {
		t.Fatalf("expected ConfigMap to exist")
	}
}

// TestGetResourceNestedString_ConfigMap tests GetResourceNestedString with a ConfigMap
func TestGetResourceNestedString_ConfigMap(t *testing.T) {
	t.Parallel()
	c := mustClient(t)
	ctx := context.Background()

	nsName := fmt.Sprintf("test-nestedstring-%d", time.Now().UnixNano())

	// Create test namespace
	ns := createUnstructured("Namespace", "v1", "", nsName, nil)
	nsGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
	_, err := c.Dyn.Resource(nsGVR).Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create namespace: %v", err)
	}
	t.Cleanup(func() {
		_ = c.Dyn.Resource(nsGVR).Delete(context.Background(), nsName, metav1.DeleteOptions{})
	})

	cmName := "test-nested-configmap"
	cmGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}

	// Create ConfigMap with nested data
	cm := createUnstructured("ConfigMap", "v1", nsName, cmName, map[string]interface{}{
		"data": map[string]interface{}{
			"server": "https://example.com:8200",
		},
	})
	_, err = c.Dyn.Resource(cmGVR).Namespace(nsName).Create(ctx, cm, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create ConfigMap: %v", err)
	}

	// Get nested string value
	value, err := c.GetResourceNestedString(ctx, "v1", "ConfigMap", nsName, cmName, "data", "server")
	if err != nil {
		t.Fatalf("GetResourceNestedString failed: %v", err)
	}
	if value != "https://example.com:8200" {
		t.Fatalf("expected value 'https://example.com:8200', got %q", value)
	}
}

// TestGetResourceNestedString_NotFound tests GetResourceNestedString with non-existent resource
func TestGetResourceNestedString_NotFound(t *testing.T) {
	t.Parallel()
	c := mustClient(t)
	ctx := context.Background()

	// Try to get nested string from non-existent ConfigMap
	value, err := c.GetResourceNestedString(ctx, "v1", "ConfigMap", "default", "nonexistent-cm-12345", "data", "key")
	if err != nil {
		t.Fatalf("GetResourceNestedString should return empty string for not found, got error: %v", err)
	}
	if value != "" {
		t.Fatalf("expected empty string for not found resource, got %q", value)
	}
}

// TestGetResourceNestedString_MissingField tests GetResourceNestedString with missing field
func TestGetResourceNestedString_MissingField(t *testing.T) {
	t.Parallel()
	c := mustClient(t)
	ctx := context.Background()

	nsName := fmt.Sprintf("test-missingfield-%d", time.Now().UnixNano())

	// Create test namespace
	ns := createUnstructured("Namespace", "v1", "", nsName, nil)
	nsGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
	_, err := c.Dyn.Resource(nsGVR).Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create namespace: %v", err)
	}
	t.Cleanup(func() {
		_ = c.Dyn.Resource(nsGVR).Delete(context.Background(), nsName, metav1.DeleteOptions{})
	})

	cmName := "test-missing-field-cm"
	cmGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}

	// Create ConfigMap without the field we're looking for
	cm := createUnstructured("ConfigMap", "v1", nsName, cmName, map[string]interface{}{
		"data": map[string]interface{}{
			"other": "value",
		},
	})
	_, err = c.Dyn.Resource(cmGVR).Namespace(nsName).Create(ctx, cm, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create ConfigMap: %v", err)
	}

	// Get nested string value for missing field
	value, err := c.GetResourceNestedString(ctx, "v1", "ConfigMap", nsName, cmName, "data", "nonexistent")
	if err != nil {
		t.Fatalf("GetResourceNestedString failed: %v", err)
	}
	if value != "" {
		t.Fatalf("expected empty string for missing field, got %q", value)
	}
}
