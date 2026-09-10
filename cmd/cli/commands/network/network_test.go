// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package network

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNetworkCmd_RegistersEveryScope(t *testing.T) {
	registered := make(map[string]bool)
	for _, c := range GetCmd().Commands() {
		registered[c.Name()] = true
	}

	for _, name := range []string{"firewall", "policy", "reassert", "shape"} {
		assert.True(t, registered[name], "%s must be registered on the network command group", name)
	}
}
