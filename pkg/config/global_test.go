// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"testing"
)

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

// TestInitialize_EnvVarNotAppliedAtConfigLayer verifies that SOLO_PROVISIONER_* environment
// variables are NOT merged into models.Config during Initialize. Env var overrides are now
// handled exclusively by the RSL layer (Resolver.WithEnv), which gives them the correct
// precedence: env > config file, but not > CLI flags.
func TestInitialize_EnvVarNotAppliedAtConfigLayer(t *testing.T) {
	yamlCfg := `
blockNode:
  namespace: "original-ns"
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

	envKey := "SOLO_PROVISIONER_BLOCKNODE_STORAGE_BASEPATH"
	envVal := "/data/block-node"
	orig := os.Getenv(envKey)
	if err := os.Setenv(envKey, envVal); err != nil {
		t.Fatalf("set env: %v", err)
	}
	defer func() { _ = os.Setenv(envKey, orig) }()

	if err := Initialize(tmpFile.Name()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// config layer must return the file value — env override is the RSL layer's job
	got := Get().BlockNode.Storage.BasePath
	if got != "/mnt/fast-storage" {
		t.Fatalf("config layer must not apply env var: expected %q (file value), got %q", "/mnt/fast-storage", got)
	}
}

// TestDefaultsConfig_ReturnsDepsConstants verifies that DefaultsConfig returns
// the hardcoded deps constants and does not read env vars or config files.
func TestDefaultsConfig_ReturnsDepsConstants(t *testing.T) {
	cfg := DefaultsConfig()

	if cfg.BlockNode.Namespace == "" {
		t.Error("Namespace: expected non-empty deps constant, got empty")
	}
	if cfg.BlockNode.Release == "" {
		t.Error("Release: expected non-empty deps constant, got empty")
	}
	if cfg.BlockNode.Chart == "" {
		t.Error("Chart: expected non-empty deps constant, got empty")
	}
	if cfg.BlockNode.ChartVersion == "" {
		t.Error("ChartVersion: expected non-empty deps constant, got empty")
	}
	if cfg.BlockNode.Storage.BasePath == "" {
		t.Error("Storage.BasePath: expected non-empty deps constant, got empty")
	}
	if cfg.Teleport.Version == "" {
		t.Error("Teleport.Version: expected non-empty deps constant, got empty")
	}
	// Alloy and cluster fields are intentionally not in defaults (no deps constants for them)
	if cfg.Alloy.ClusterName != "" {
		t.Errorf("Alloy.ClusterName: expected empty (no default), got %q", cfg.Alloy.ClusterName)
	}
}

// TestEnvConfig_ReadsEnvVars verifies that EnvConfig populates fields from
// SOLO_PROVISIONER_* environment variables and leaves unset fields at zero value.
func TestEnvConfig_ReadsEnvVars(t *testing.T) {
	vars := map[string]string{
		"SOLO_PROVISIONER_BLOCKNODE_NAMESPACE":           "env-ns",
		"SOLO_PROVISIONER_BLOCKNODE_VERSION":             "1.2.3",
		"SOLO_PROVISIONER_BLOCKNODE_STORAGE_BASEPATH":    "/env/base",
		"SOLO_PROVISIONER_BLOCKNODE_STORAGE_ARCHIVEPATH": "/env/archive",
	}
	for k, v := range vars {
		orig := os.Getenv(k)
		_ = os.Setenv(k, v)
		defer func(key, orig string) { _ = os.Setenv(key, orig) }(k, orig)
	}

	cfg := EnvConfig()

	if cfg.BlockNode.Namespace != "env-ns" {
		t.Errorf("Namespace: expected %q, got %q", "env-ns", cfg.BlockNode.Namespace)
	}
	if cfg.BlockNode.ChartVersion != "1.2.3" {
		t.Errorf("ChartVersion: expected %q, got %q", "1.2.3", cfg.BlockNode.ChartVersion)
	}
	if cfg.BlockNode.Storage.BasePath != "/env/base" {
		t.Errorf("BasePath: expected %q, got %q", "/env/base", cfg.BlockNode.Storage.BasePath)
	}
	if cfg.BlockNode.Storage.ArchivePath != "/env/archive" {
		t.Errorf("ArchivePath: expected %q, got %q", "/env/archive", cfg.BlockNode.Storage.ArchivePath)
	}
	// unset fields must be empty
	if cfg.BlockNode.Release != "" {
		t.Errorf("Release: expected empty, got %q", cfg.BlockNode.Release)
	}
}
