// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Test_bandwidthManagerEnabled covers the decision the StartCilium guard makes on
// the cilium-config enable-bandwidth-manager value to assert the BN traffic
// shaper precondition (Bandwidth Manager off, issue #741). The ConfigMap value is
// the load-bearing contract, so these cases pin how it maps to enabled/disabled.
func Test_bandwidthManagerEnabled(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "true", value: "true", want: true},
		{name: "mixed case true", value: "True", want: true},
		{name: "true with whitespace", value: "  true ", want: true},
		{name: "false", value: "false", want: false},
		{name: "absent key (empty)", value: "", want: false},
		{name: "disabled literal", value: "disabled", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, bandwidthManagerEnabled(tt.value))
		})
	}
}
