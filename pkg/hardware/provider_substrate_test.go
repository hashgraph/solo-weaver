// SPDX-License-Identifier: Apache-2.0

package hardware

import (
	"strings"
	"testing"
)

func TestSubstrateProvider_Values(t *testing.T) {
	p := &substrateProvider{}
	reqs, err := p.Compute(DeploymentSpec{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs.MinCpuCores != 2 {
		t.Errorf("expected 2 CPU cores, got %d", reqs.MinCpuCores)
	}
	if reqs.MinMemoryGB != 2 {
		t.Errorf("expected 2 GB memory, got %d", reqs.MinMemoryGB)
	}
	if reqs.MinStorageGB != 20 {
		t.Errorf("expected 20 GB storage, got %d", reqs.MinStorageGB)
	}
	if len(reqs.MinSupportedOS) == 0 {
		t.Error("expected at least one supported OS")
	}
}

func TestSubstrateProvider_ComputeWithWhy(t *testing.T) {
	p := &substrateProvider{}
	reqs, why, err := p.ComputeWithWhy(DeploymentSpec{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs.MinCpuCores != 2 {
		t.Errorf("expected 2 CPU cores, got %d", reqs.MinCpuCores)
	}
	if why["cpu"] == "" {
		t.Error("expected non-empty Why for cpu")
	}
	if why["memory"] == "" {
		t.Error("expected non-empty Why for memory")
	}
	if why["storage"] == "" {
		t.Error("expected non-empty Why for storage")
	}
}

func TestSubstrateProvider_IgnoresSpec(t *testing.T) {
	p := &substrateProvider{}
	// Substrate provider should return the same values regardless of spec
	spec1 := DeploymentSpec{NodeType: "k8s-substrate", Profile: "mainnet"}
	spec2 := DeploymentSpec{NodeType: "k8s-substrate", Profile: "local"}
	reqs1, _ := p.Compute(spec1)
	reqs2, _ := p.Compute(spec2)
	if reqs1.MinCpuCores != reqs2.MinCpuCores {
		t.Errorf("expected same CPU regardless of profile: %d vs %d", reqs1.MinCpuCores, reqs2.MinCpuCores)
	}
}

func TestCreateSubstrateSpec_BypassesProfileAndNodeTypeGates(t *testing.T) {
	// A host that clears the substrate floor (2/2/20) but carries no profile and
	// a node type that is NOT in SupportedNodeTypes. CreateNodeSpec would reject it;
	// CreateSubstrateSpec must not.
	host := NewMockHostProfile("ubuntu", "20.04", 8, 16, 500)

	spec, err := CreateSubstrateSpec(host)
	if err != nil {
		t.Fatalf("CreateSubstrateSpec returned error: %v", err)
	}

	reqs := spec.GetBaselineRequirements()
	if reqs.MinCpuCores != 2 || reqs.MinMemoryGB != 2 || reqs.MinStorageGB != 20 {
		t.Errorf("expected substrate floor 2/2/20, got %d/%d/%d",
			reqs.MinCpuCores, reqs.MinMemoryGB, reqs.MinStorageGB)
	}
	if err := spec.ValidateCPU(); err != nil {
		t.Errorf("expected CPU validation to pass on an 8-core host: %v", err)
	}
	if err := spec.ValidateStorage(); err != nil {
		t.Errorf("expected storage validation to pass on a 500 GB host: %v", err)
	}

	// The substrate spec has no profile; its display name must not render empty
	// parentheses (e.g. "K8s-Substrate Node ()") in operator-facing errors.
	if name := spec.GetNodeType(); strings.Contains(name, "()") {
		t.Errorf("substrate display name should omit empty profile parentheses, got %q", name)
	}

	// Sanity: the same host through CreateNodeSpec with the substrate key + empty
	// profile must be rejected — proving the substrate helper is doing the bypass.
	if _, err := CreateNodeSpec(DeploymentSpec{NodeType: NodeTypeSubstrate}, host); err == nil {
		t.Error("expected CreateNodeSpec to reject the substrate node type / empty profile")
	}
}

func TestSubstrateProvider_RegistryEntry(t *testing.T) {
	providers := Providers()
	p, ok := providers["k8s-substrate"]
	if !ok {
		t.Fatal("k8s-substrate provider not registered in global registry")
	}
	reqs, err := p.Compute(DeploymentSpec{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs.MinCpuCores != 2 {
		t.Errorf("expected 2 CPU cores from registry provider, got %d", reqs.MinCpuCores)
	}
}
