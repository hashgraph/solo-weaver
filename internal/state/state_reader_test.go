// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package state

import (
	"testing"
)

func TestReadProvisionerVersion_ParsesCorrectly(t *testing.T) {
	data := []byte(`
state:
  provisioner:
    version: "v1.2.3"
  machineState:
    profile: mainnet
`)
	var doc ProvisionerVersionDoc
	if err := unmarshalStateDoc(data, &doc); err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if doc.State.Provisioner.Version != "v1.2.3" {
		t.Fatalf("expected version 'v1.2.3', got %q", doc.State.Provisioner.Version)
	}
}

func TestReadPromptDefaults_ParsesProfile(t *testing.T) {
	data := []byte(`
state:
  machineState:
    profile: testnet
`)
	var doc PromptDefaultsDoc
	if err := unmarshalStateDoc(data, &doc); err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if doc.State.MachineState.Profile != "testnet" {
		t.Fatalf("expected profile 'testnet', got %q", doc.State.MachineState.Profile)
	}
}

func TestReadPromptDefaults_ParsesAllBlockNodeFields(t *testing.T) {
	data := []byte(`
state:
  machineState:
    profile: mainnet
  blockNodeState:
    name: my-release
    namespace: my-ns
    version: "0.8.1"
    historicRetention: "500"
    recentRetention: "12000"
    storage:
      basePath: /mnt/data
      archivePath: /mnt/archive
      livePath: /mnt/live
      logPath: /mnt/log
      verificationPath: /mnt/verification
      pluginsPath: /mnt/plugins
`)
	var doc PromptDefaultsDoc
	if err := unmarshalStateDoc(data, &doc); err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	if doc.State.MachineState.Profile != "mainnet" {
		t.Errorf("expected Profile 'mainnet', got %q", doc.State.MachineState.Profile)
	}
	bn := doc.State.BlockNodeState
	if bn.Name != "my-release" {
		t.Errorf("expected Name 'my-release', got %q", bn.Name)
	}
	if bn.Namespace != "my-ns" {
		t.Errorf("expected Namespace 'my-ns', got %q", bn.Namespace)
	}
	if bn.ChartVersion != "0.8.1" {
		t.Errorf("expected ChartVersion '0.8.1', got %q", bn.ChartVersion)
	}
	if bn.HistoricRetention != "500" {
		t.Errorf("expected HistoricRetention '500', got %q", bn.HistoricRetention)
	}
	if bn.RecentRetention != "12000" {
		t.Errorf("expected RecentRetention '12000', got %q", bn.RecentRetention)
	}
	if bn.Storage.BasePath != "/mnt/data" {
		t.Errorf("expected Storage.BasePath '/mnt/data', got %q", bn.Storage.BasePath)
	}
	if bn.Storage.ArchivePath != "/mnt/archive" {
		t.Errorf("expected Storage.ArchivePath '/mnt/archive', got %q", bn.Storage.ArchivePath)
	}
	if bn.Storage.LivePath != "/mnt/live" {
		t.Errorf("expected Storage.LivePath '/mnt/live', got %q", bn.Storage.LivePath)
	}
	if bn.Storage.LogPath != "/mnt/log" {
		t.Errorf("expected Storage.LogPath '/mnt/log', got %q", bn.Storage.LogPath)
	}
	if bn.Storage.VerificationPath != "/mnt/verification" {
		t.Errorf("expected Storage.VerificationPath '/mnt/verification', got %q", bn.Storage.VerificationPath)
	}
	if bn.Storage.PluginsPath != "/mnt/plugins" {
		t.Errorf("expected Storage.PluginsPath '/mnt/plugins', got %q", bn.Storage.PluginsPath)
	}
}

func TestReadPromptDefaults_MissingFieldsReturnZero(t *testing.T) {
	data := []byte(`
state:
  blockNodeState:
    name: only-name
`)
	var doc PromptDefaultsDoc
	if err := unmarshalStateDoc(data, &doc); err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	bn := doc.State.BlockNodeState
	if bn.Name != "only-name" {
		t.Errorf("expected Name 'only-name', got %q", bn.Name)
	}
	if bn.Namespace != "" {
		t.Errorf("expected empty Namespace, got %q", bn.Namespace)
	}
	if bn.ChartVersion != "" {
		t.Errorf("expected empty ChartVersion, got %q", bn.ChartVersion)
	}
	if bn.HistoricRetention != "" {
		t.Errorf("expected empty HistoricRetention, got %q", bn.HistoricRetention)
	}
	if bn.RecentRetention != "" {
		t.Errorf("expected empty RecentRetention, got %q", bn.RecentRetention)
	}
}

func TestReadPromptDefaults_EmptyStateReturnsZero(t *testing.T) {
	data := []byte(`
state:
  version: v2
`)
	var doc PromptDefaultsDoc
	if err := unmarshalStateDoc(data, &doc); err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	if doc.State.MachineState.Profile != "" {
		t.Errorf("expected empty Profile, got %q", doc.State.MachineState.Profile)
	}
	bn := doc.State.BlockNodeState
	if bn.Name != "" || bn.Namespace != "" || bn.ChartVersion != "" || bn.HistoricRetention != "" || bn.RecentRetention != "" {
		t.Errorf("expected all zero block node fields, got %+v", bn)
	}
}

func TestUnmarshalStateDoc_InvalidYAMLReturnsError(t *testing.T) {
	data := []byte(`{{{invalid yaml`)
	var doc PromptDefaultsDoc

	if err := unmarshalStateDoc(data, &doc); err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}
