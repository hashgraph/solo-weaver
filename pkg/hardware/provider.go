// SPDX-License-Identifier: Apache-2.0

package hardware

// RequirementsProvider computes hardware floors from a structured deployment spec.
type RequirementsProvider interface {
	Compute(spec DeploymentSpec) (BaselineRequirements, error)
	ComputeWithWhy(spec DeploymentSpec) (BaselineRequirements, map[string]string, error)
}

// DeploymentSpec carries all sizing inputs for a node deployment.
type DeploymentSpec struct {
	NodeType string
	Profile  string
	Options  map[string]any // "preset", "plugins", future keys
}

var globalProviders = map[string]RequirementsProvider{}

// Providers returns a snapshot of the global provider registry keyed by node type.
// Keys: "block", "consensus", "k8s-substrate".
func Providers() map[string]RequirementsProvider {
	out := make(map[string]RequirementsProvider, len(globalProviders))
	for k, v := range globalProviders {
		out[k] = v
	}
	return out
}

func registerProvider(key string, p RequirementsProvider) {
	globalProviders[key] = p
}
