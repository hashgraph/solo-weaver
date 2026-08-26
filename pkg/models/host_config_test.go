// SPDX-License-Identifier: Apache-2.0

package models

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHostConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     HostConfig
		wantErr bool
	}{
		{
			name: "empty is valid (firewall skipped)",
			cfg:  HostConfig{},
		},
		{
			name: "full valid config",
			cfg: HostConfig{
				ManagementCIDRs: []string{"10.0.0.0/8", "192.168.0.0/16"},
				MgmtPorts:       []int{22, 2222},
				PodCIDR:         "10.4.0.0/14",
				InClusterPorts:  []int{6443, 4244, 7472, 10250},
			},
		},
		{
			name:    "invalid management CIDR",
			cfg:     HostConfig{ManagementCIDRs: []string{"not-a-cidr"}},
			wantErr: true,
		},
		{
			name:    "management CIDR missing prefix",
			cfg:     HostConfig{ManagementCIDRs: []string{"10.0.0.0"}},
			wantErr: true,
		},
		{
			name:    "invalid mgmt port (too high)",
			cfg:     HostConfig{MgmtPorts: []int{70000}},
			wantErr: true,
		},
		{
			name:    "invalid pod CIDR",
			cfg:     HostConfig{PodCIDR: "10.4.0.0"},
			wantErr: true,
		},
		{
			name:    "IPv6 management CIDR rejected",
			cfg:     HostConfig{ManagementCIDRs: []string{"2001:db8::/32"}},
			wantErr: true,
		},
		{
			name:    "IPv6 pod CIDR rejected",
			cfg:     HostConfig{PodCIDR: "2001:db8::/32"},
			wantErr: true,
		},
		{
			name:    "invalid in-cluster port",
			cfg:     HostConfig{InClusterPorts: []int{6443, 99999}},
			wantErr: true,
		},
		{
			name: "empty mgmt ports is allowed (means default)",
			cfg:  HostConfig{MgmtPorts: nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfg
			err := cfg.Validate()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestConfig_Validate_IncludesHost(t *testing.T) {
	c := Config{Host: HostConfig{ManagementCIDRs: []string{"bad"}}}
	require.Error(t, c.Validate(), "Config.Validate must surface host firewall errors")
}
