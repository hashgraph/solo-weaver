// SPDX-License-Identifier: Apache-2.0
package labels

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestEngProfile_Labels(t *testing.T) {
	t.Run("returns only cluster label", func(t *testing.T) {
		result := EngProfile{}.Labels(LabelInput{ClusterName: "my-cluster", DeployProfile: "previewnet", MachineIP: "10.0.0.1"})
		assert.Equal(t, map[string]string{
			"cluster": "my-cluster",
		}, result)
	})
	t.Run("returns empty map when cluster name is empty", func(t *testing.T) {
		result := EngProfile{}.Labels(LabelInput{})
		assert.Empty(t, result)
	})
}
