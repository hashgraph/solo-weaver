// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package reality

import (
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func makePV(claimNamespace, claimName, size, hostPath string) unstructured.Unstructured {
	return unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"claimRef": map[string]interface{}{
					"namespace": claimNamespace,
					"name":      claimName,
				},
				"capacity": map[string]interface{}{
					"storage": size,
				},
				"hostPath": map[string]interface{}{
					"path": hostPath,
				},
			},
		},
	}
}

func TestPopulateStorageFromPVs_ApplicationStatePVC(t *testing.T) {
	pvs := &unstructured.UnstructuredList{
		Items: []unstructured.Unstructured{
			makePV("test-ns", "application-state-storage-pvc", "50Gi", "/mnt/fast-storage/block-node/application-state"),
		},
	}
	storage := &models.BlockNodeStorage{}
	checker := &blockNodeChecker{}

	if err := checker.populateStorageFromPVs(pvs, "test-ns", storage); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if storage.ApplicationStatePath != "/mnt/fast-storage/block-node/application-state" {
		t.Errorf("expected ApplicationStatePath '/mnt/fast-storage/block-node/application-state', got %q", storage.ApplicationStatePath)
	}
	if storage.ApplicationStateSize != "50Gi" {
		t.Errorf("expected ApplicationStateSize '50Gi', got %q", storage.ApplicationStateSize)
	}
}

func TestPopulateStorageFromPVs_AllFields(t *testing.T) {
	pvs := &unstructured.UnstructuredList{
		Items: []unstructured.Unstructured{
			makePV("ns", "live-storage-pvc", "100Gi", "/mnt/live"),
			makePV("ns", "archive-storage-pvc", "200Gi", "/mnt/archive"),
			makePV("ns", "log-storage-pvc", "10Gi", "/mnt/log"),
			makePV("ns", "verification-storage-pvc", "20Gi", "/mnt/verification"),
			makePV("ns", "plugins-storage-pvc", "5Gi", "/mnt/plugins"),
			makePV("ns", "application-state-storage-pvc", "50Gi", "/mnt/app-state"),
		},
	}
	storage := &models.BlockNodeStorage{}
	checker := &blockNodeChecker{}

	if err := checker.populateStorageFromPVs(pvs, "ns", storage); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if storage.LivePath != "/mnt/live" {
		t.Errorf("LivePath: got %q", storage.LivePath)
	}
	if storage.ArchivePath != "/mnt/archive" {
		t.Errorf("ArchivePath: got %q", storage.ArchivePath)
	}
	if storage.LogPath != "/mnt/log" {
		t.Errorf("LogPath: got %q", storage.LogPath)
	}
	if storage.VerificationPath != "/mnt/verification" {
		t.Errorf("VerificationPath: got %q", storage.VerificationPath)
	}
	if storage.PluginsPath != "/mnt/plugins" {
		t.Errorf("PluginsPath: got %q", storage.PluginsPath)
	}
	if storage.ApplicationStatePath != "/mnt/app-state" {
		t.Errorf("ApplicationStatePath: got %q", storage.ApplicationStatePath)
	}
	if storage.ApplicationStateSize != "50Gi" {
		t.Errorf("ApplicationStateSize: got %q", storage.ApplicationStateSize)
	}
}

func TestPopulateStorageFromPVs_SkipsDifferentNamespace(t *testing.T) {
	pvs := &unstructured.UnstructuredList{
		Items: []unstructured.Unstructured{
			makePV("other-ns", "application-state-storage-pvc", "50Gi", "/mnt/app-state"),
		},
	}
	storage := &models.BlockNodeStorage{}
	checker := &blockNodeChecker{}

	if err := checker.populateStorageFromPVs(pvs, "test-ns", storage); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if storage.ApplicationStatePath != "" {
		t.Errorf("expected empty ApplicationStatePath for wrong namespace, got %q", storage.ApplicationStatePath)
	}
}

func TestPopulateStorageFromPVs_NilInputsNoError(t *testing.T) {
	checker := &blockNodeChecker{}

	if err := checker.populateStorageFromPVs(nil, "ns", nil); err != nil {
		t.Fatalf("unexpected error for nil inputs: %v", err)
	}
}
