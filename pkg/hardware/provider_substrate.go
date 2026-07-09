// SPDX-License-Identifier: Apache-2.0

package hardware

// NodeTypeSubstrate is the provider key for the Kubernetes substrate floor —
// the hardware Kubernetes itself needs to run, independent of any workload.
// It is intentionally not part of SupportedNodeTypes(): it is never a user-facing
// --node-type, only an internal provider key used by the cluster-install preflight.
const NodeTypeSubstrate = "k8s-substrate"

func init() {
	registerProvider(NodeTypeSubstrate, &substrateProvider{})
}

type substrateProvider struct{}

func (p *substrateProvider) Compute(_ DeploymentSpec) (BaselineRequirements, error) {
	return BaselineRequirements{
		MinCpuCores:    2,
		MinMemoryGB:    2,
		MinStorageGB:   20,
		MinSupportedOS: []string{OSUbuntu18, OSDebian10},
	}, nil
}

func (p *substrateProvider) ComputeWithWhy(spec DeploymentSpec) (BaselineRequirements, map[string]string, error) {
	req, err := p.Compute(spec)
	if err != nil {
		return BaselineRequirements{}, nil, err
	}
	why := map[string]string{
		"cpu":     "Kubernetes control-plane minimum",
		"memory":  "Kubernetes control-plane minimum",
		"storage": "Kubernetes control-plane minimum",
	}
	return req, why, nil
}
