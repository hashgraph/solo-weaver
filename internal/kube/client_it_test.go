//go:build kubeclient || require_cluster

package kube

import (
	"context"
	"fmt"
	"testing"
	"time"

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

func createPodUnstructured(ns, name, cmd string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": ns,
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
func createAndWait(t *testing.T, c *Client, gvr schema.GroupVersionResource, ns string, obj *unstructured.Unstructured, check func(*unstructured.Unstructured) (bool, error), timeout time.Duration) {
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

	if err := c.WaitForResource(ctx, ToResourceKind(gvr), ns, obj.GetName(), check, timeout); err != nil {
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
	if err := c.WaitForDeleted(ctx, gvr, ns, name, timeout); err != nil {
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
	nsGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
	if err := c.WaitForExistence(ctx, nsGVR, "", nsName, 30*time.Second); err != nil {
		t.Fatalf("waiting for namespace %s: %v", nsName, err)
	}

	// Wait for ConfigMap
	cmGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	if err := c.WaitForExistence(ctx, cmGVR, nsName, cmName, 30*time.Second); err != nil {
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
	if err := c.WaitForContainer(ctx, nsName, podName, IsContainerReady("c"), 1*time.Minute); err != nil {
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

	podObj := createPodUnstructured(nsName, podName, "sleep 1; exit 0")
	createAndWait(t, c, podGVR, nsName, podObj, IsPodReady, 30*time.Second)
	defer deleteAndWait(t, c, podGVR, nsName, podName, 1*time.Minute)

	if err := c.WaitForContainer(ctx, nsName, podName, IsContainerReady("c"), 60*time.Second); err != nil {
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

	podObj := createPodUnstructured(nsName, podName, "sleep 1; exit 2")
	createAndWait(t, c, podGVR, nsName, podObj, IsPodReady, 30*time.Second)
	defer deleteAndWait(t, c, podGVR, nsName, podName, 1*time.Minute)

	if err := c.WaitForContainer(ctx, nsName, podName,
		IsContainerTerminated("c", 2), 60*time.Second); err != nil {
		t.Fatalf("expected error for container terminated with exit code 2, got error: %v", err)
	}
}
