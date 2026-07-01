// SPDX-License-Identifier: Apache-2.0

package hardware

func init() {
	registerProvider("k8s-substrate", &substrateProvider{})
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
