// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package state

import (
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/models"
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

func TestReadSoftwareVersion_ParsesRecordedVersion(t *testing.T) {
	// cri-o is stored under the installer key "crio", not its artifact name.
	data := []byte(`
state:
  machineState:
    software:
      cilium:
        name: cilium
        version: "0.18.7"
        installed: true
      crio:
        name: cri-o
        version: "1.30.0"
`)
	var doc SoftwareVersionsDoc
	if err := unmarshalStateDoc(data, &doc); err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	// Direct map-key hit.
	if got := softwareVersionFromDoc(doc, "cilium"); got != "0.18.7" {
		t.Fatalf("expected cilium version '0.18.7', got %q", got)
	}
	// Artifact name resolves via the name fallback despite the differing map key.
	if got := softwareVersionFromDoc(doc, "cri-o"); got != "1.30.0" {
		t.Fatalf("expected cri-o version '1.30.0' via name fallback, got %q", got)
	}
	// The installer key still resolves directly too.
	if got := softwareVersionFromDoc(doc, "crio"); got != "1.30.0" {
		t.Fatalf("expected cri-o version '1.30.0' via map key, got %q", got)
	}
	// Unrecorded component resolves to the empty string.
	if got := softwareVersionFromDoc(doc, "teleport"); got != "" {
		t.Fatalf("expected empty version for unrecorded component, got %q", got)
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
    pluginPreset: "tier1-lfh"
    pluginList: "facility-messaging,health"
    storage:
      basePath: /mnt/data
      archivePath: /mnt/archive
      livePath: /mnt/live
      logPath: /mnt/log
      verificationPath: /mnt/verification
      pluginsPath: /mnt/plugins
      applicationStatePath: /mnt/app-state
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
	if bn.PluginPreset != "tier1-lfh" {
		t.Errorf("expected PluginPreset 'tier1-lfh', got %q", bn.PluginPreset)
	}
	if bn.PluginList != "facility-messaging,health" {
		t.Errorf("expected PluginList 'facility-messaging,health', got %q", bn.PluginList)
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
	if bn.Storage.ApplicationStatePath != "/mnt/app-state" {
		t.Errorf("expected Storage.ApplicationStatePath '/mnt/app-state', got %q", bn.Storage.ApplicationStatePath)
	}
}

func TestReadPromptDefaults_ApplicationStatePathFlowsThroughToModel(t *testing.T) {
	data := []byte(`
state:
  blockNodeState:
    storage:
      applicationStatePath: /mnt/app-state
`)
	var doc PromptDefaultsDoc
	if err := unmarshalStateDoc(data, &doc); err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	bn := doc.State.BlockNodeState
	storage := models.BlockNodeStorage{
		BasePath:             bn.Storage.BasePath,
		ArchivePath:          bn.Storage.ArchivePath,
		LivePath:             bn.Storage.LivePath,
		LogPath:              bn.Storage.LogPath,
		VerificationPath:     bn.Storage.VerificationPath,
		PluginsPath:          bn.Storage.PluginsPath,
		ApplicationStatePath: bn.Storage.ApplicationStatePath,
	}
	if storage.ApplicationStatePath != "/mnt/app-state" {
		t.Errorf("expected ApplicationStatePath '/mnt/app-state' in model, got %q", storage.ApplicationStatePath)
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
