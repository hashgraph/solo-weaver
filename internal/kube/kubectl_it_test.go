//go:build require_cluster

package kube

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestApplyManifest(t *testing.T) {
	ctx := context.Background()

	_, dynClient, err := buildConfigAndClient()
	if err != nil {
		t.Skipf("skipping integration test (no kubeconfig / cluster): %v", err)
	}

	// create unique namespace manifest
	name := fmt.Sprintf("kubectl-it-test-%d", time.Now().UnixNano()%1000000)
	manifest := fmt.Sprintf("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: %s\n", name)

	tmp, err := os.CreateTemp("", "kubectl-it-test-*.yaml")
	if err != nil {
		t.Fatalf("create temp manifest: %v", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(manifest); err != nil {
		tmp.Close()
		t.Fatalf("write manifest: %v", err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatalf("close manifest: %v", err)
	}

	// apply
	if err := ApplyManifest(ctx, tmp.Name()); err != nil {
		t.Fatalf("ApplyManifest failed: %v", err)
	}

	// verify created (namespaces are cluster-scoped)
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
	// dynClient implements the minimal interface expected by waitForResourcePresence
	if err := waitForResourcePresence(ctx, dynClient, gvr, name, true, 10*time.Second); err != nil {
		t.Fatalf("resource did not appear: %v", err)
	}

	// delete
	if err := DeleteManifest(ctx, tmp.Name()); err != nil {
		t.Fatalf("DeleteManifest failed: %v", err)
	}

	// verify deleted
	if err := waitForResourcePresence(ctx, dynClient, gvr, name, false, 15*time.Second); err != nil {
		t.Fatalf("resource was not deleted: %v", err)
	}
}
