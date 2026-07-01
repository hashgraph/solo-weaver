// SPDX-License-Identifier: Apache-2.0

package hardware

import (
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
