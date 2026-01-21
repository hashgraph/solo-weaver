// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"testing"
)

func TestInitialize_EnvOverride_BlockNodeStorageBasePath(t *testing.T) {
	// prepare a temp config file with a different storage.basePath
	yamlCfg := `
log:
  level: "Info"
blockNode:
  namespace: "original-ns"
  release: "original-release"
  chart: "original-chart"
  version: "0.0.1"
  storage:
    basePath: "/mnt/fast-storage"
`
	tmpFile, err := os.CreateTemp("", "weaver-config-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(yamlCfg); err != nil {
		tmpFile.Close()
		t.Fatalf("write temp config: %v", err)
	}
	tmpFile.Close()

	// set environment variable to override the storage.basePath
	envKey := "WEAVER_BLOCKNODE_STORAGE_BASEPATH"
	expected := "/data/block-node"
	orig := os.Getenv(envKey)
	if err := os.Setenv(envKey, expected); err != nil {
		t.Fatalf("set env: %v", err)
	}
	// restore env afterwards
	defer func() {
		_ = os.Setenv(envKey, orig)
	}()

	// call Initialize with the temp config path
	if err := Initialize(tmpFile.Name()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// verify the env variable value took precedence
	got := Get().BlockNode.Storage.BasePath
	if got != expected {
		t.Fatalf("env override failed: expected %q, got %q", expected, got)
	}
}

func TestInitialize_BackwardCompatibility_OldFieldNames(t *testing.T) {
	// Test that old config field names (release, chart) are migrated to new names (releaseName, chartRepo)
	yamlCfg := `
blockNode:
  namespace: "test-ns"
  release: "old-release-name"
  chart: "oci://ghcr.io/old/chart"
  version: "1.0.0"
  storage:
    basePath: "/mnt/storage"
`
	tmpFile, err := os.CreateTemp("", "weaver-config-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(yamlCfg); err != nil {
		tmpFile.Close()
		t.Fatalf("write temp config: %v", err)
	}
	tmpFile.Close()

	if err := Initialize(tmpFile.Name()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	cfg := Get()

	// Verify old field names are migrated to new struct fields
	if cfg.BlockNode.ReleaseName != "old-release-name" {
		t.Errorf("ReleaseName migration failed: expected %q, got %q", "old-release-name", cfg.BlockNode.ReleaseName)
	}
	if cfg.BlockNode.ChartRepo != "oci://ghcr.io/old/chart" {
		t.Errorf("ChartRepo migration failed: expected %q, got %q", "oci://ghcr.io/old/chart", cfg.BlockNode.ChartRepo)
	}
}

func TestInitialize_NewFieldNames(t *testing.T) {
	// Test that new config field names work directly
	yamlCfg := `
blockNode:
  namespace: "test-ns"
  releaseName: "new-release-name"
  chartRepo: "oci://ghcr.io/new/chart"
  chartVersion: "2.0.0"
  version: "1.0.0"
  storage:
    basePath: "/mnt/storage"
`
	tmpFile, err := os.CreateTemp("", "weaver-config-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(yamlCfg); err != nil {
		tmpFile.Close()
		t.Fatalf("write temp config: %v", err)
	}
	tmpFile.Close()

	if err := Initialize(tmpFile.Name()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	cfg := Get()

	if cfg.BlockNode.ReleaseName != "new-release-name" {
		t.Errorf("ReleaseName: expected %q, got %q", "new-release-name", cfg.BlockNode.ReleaseName)
	}
	if cfg.BlockNode.ChartRepo != "oci://ghcr.io/new/chart" {
		t.Errorf("ChartRepo: expected %q, got %q", "oci://ghcr.io/new/chart", cfg.BlockNode.ChartRepo)
	}
	if cfg.BlockNode.ChartVersion != "2.0.0" {
		t.Errorf("ChartVersion: expected %q, got %q", "2.0.0", cfg.BlockNode.ChartVersion)
	}
}

func TestInitialize_NewFieldNamesTakePrecedence(t *testing.T) {
	// Test that new field names take precedence over old ones when both are specified
	yamlCfg := `
blockNode:
  namespace: "test-ns"
  release: "old-release"
  releaseName: "new-release"
  chart: "oci://old/chart"
  chartRepo: "oci://new/chart"
  version: "1.0.0"
  storage:
    basePath: "/mnt/storage"
`
	tmpFile, err := os.CreateTemp("", "weaver-config-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(yamlCfg); err != nil {
		tmpFile.Close()
		t.Fatalf("write temp config: %v", err)
	}
	tmpFile.Close()

	if err := Initialize(tmpFile.Name()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	cfg := Get()

	// New field names should take precedence
	if cfg.BlockNode.ReleaseName != "new-release" {
		t.Errorf("ReleaseName precedence failed: expected %q, got %q", "new-release", cfg.BlockNode.ReleaseName)
	}
	if cfg.BlockNode.ChartRepo != "oci://new/chart" {
		t.Errorf("ChartRepo precedence failed: expected %q, got %q", "oci://new/chart", cfg.BlockNode.ChartRepo)
	}
}
