// SPDX-License-Identifier: Apache-2.0

package hardware

// Contribution describes a rule's resource contribution.
// Reducer semantics are hardcoded by resource type:
//
//	CpuCores, MemoryGB → Max (take the highest single rule's value)
//	StorageGB, SSDStorageGB, HDDStorageGB → Sum (accumulate across all firing rules)
type Contribution struct {
	CpuCores     int
	MemoryGB     int
	StorageGB    int
	SSDStorageGB int
	HDDStorageGB int
	Why          string // human-readable attribution surfaced in doctor error output
}

// Rule pairs a predicate with its hardware contribution.
type Rule struct {
	When func(DeploymentSpec) bool
	Then Contribution
}

// Reduce applies all firing rules and returns BaselineRequirements plus a
// per-resource Why string map. Why keys: "cpu", "memory", "storage".
func Reduce(rules []Rule, spec DeploymentSpec) (BaselineRequirements, map[string]string, error) {
	var req BaselineRequirements
	why := map[string]string{}

	for _, rule := range rules {
		if rule.When(spec) {
			c := rule.Then
			// CPU and memory: Max
			if c.CpuCores > req.MinCpuCores {
				req.MinCpuCores = c.CpuCores
				why["cpu"] = c.Why
			}
			if c.MemoryGB > req.MinMemoryGB {
				req.MinMemoryGB = c.MemoryGB
				why["memory"] = c.Why
			}
			// Storage: Sum per axis
			req.MinStorageGB += c.StorageGB
			req.MinSSDStorageGB += c.SSDStorageGB
			req.MinHDDStorageGB += c.HDDStorageGB
			if c.StorageGB > 0 || c.SSDStorageGB > 0 || c.HDDStorageGB > 0 {
				if why["storage"] != "" {
					why["storage"] += "; " + c.Why
				} else {
					why["storage"] = c.Why
				}
			}
		}
	}

	return req, why, nil
}
