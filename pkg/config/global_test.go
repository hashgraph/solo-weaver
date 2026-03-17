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
	envKey := "SOLO_PROVISIONER_BLOCKNODE_STORAGE_BASEPATH"
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

	// verify the chart version from config file was loaded correctly
	gotVersion := Get().BlockNode.ChartVersion
	if gotVersion != "0.0.1" {
		t.Fatalf("chart version not loaded from config: expected %q, got %q", "0.0.1", gotVersion)
	}
}

func TestInitialize_ChartVersionFromConfigFile(t *testing.T) {
	yamlCfg := `
blockNode:
  namespace: "test-ns"
  release: "test-release"
  chart: "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server"
  version: "0.26.0"
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

	if err := Initialize(tmpFile.Name()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	cfg := Get()

	if cfg.BlockNode.ChartVersion != "0.26.0" {
		t.Fatalf("ChartVersion: expected %q, got %q", "0.26.0", cfg.BlockNode.ChartVersion)
	}
	if cfg.BlockNode.Namespace != "test-ns" {
		t.Fatalf("Namespace: expected %q, got %q", "test-ns", cfg.BlockNode.Namespace)
	}
	if cfg.BlockNode.Release != "test-release" {
		t.Fatalf("Release: expected %q, got %q", "test-release", cfg.BlockNode.Release)
	}
	if cfg.BlockNode.Chart != "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server" {
		t.Fatalf("Chart: expected OCI reference, got %q", cfg.BlockNode.Chart)
	}
	if cfg.BlockNode.Storage.BasePath != "/mnt/fast-storage" {
		t.Fatalf("BasePath: expected %q, got %q", "/mnt/fast-storage", cfg.BlockNode.Storage.BasePath)
	}
}
