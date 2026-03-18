// SPDX-License-Identifier: Apache-2.0
package labels

// EngProfile is the default label profile used by engineering.
// It includes only the mandatory "cluster" label.
type EngProfile struct{}

func init() {
	Register(EngProfile{})
}

func (EngProfile) Name() string { return "eng" }

// Labels returns the label set for the eng profile.
//
// Labels added (from LabelInput):
//   - cluster = ClusterName
func (EngProfile) Labels(input LabelInput) map[string]string {
	m := make(map[string]string)
	if input.ClusterName != "" {
		m["cluster"] = input.ClusterName
	}
	return m
}
