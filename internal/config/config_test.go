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
