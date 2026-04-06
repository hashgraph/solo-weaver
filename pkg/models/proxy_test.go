// SPDX-License-Identifier: Apache-2.0

package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxyConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		config      ProxyConfig
		expectError bool
		errorMsg    string
	}{
		{
			name:        "empty_config_is_valid",
			config:      ProxyConfig{},
			expectError: false,
		},
		{
			name: "full_valid_config",
			config: ProxyConfig{
				Enabled:                true,
				URL:                    "127.0.0.1:3128",
				NoProxy:                "localhost,127.0.0.1",
				SSLCertFile:            "/etc/ssl/certs/ca-certificates.crt",
				ContainerRegistryProxy: "localhost:5050",
			},
			expectError: false,
		},
		{
			name: "invalid_url_with_path",
			config: ProxyConfig{
				URL: "127.0.0.1:3128/path",
			},
			expectError: true,
			errorMsg:    "invalid proxy url",
		},
		{
			name: "invalid_ssl_cert_path",
			config: ProxyConfig{
				SSLCertFile: "../../../../etc/passwd",
			},
			expectError: true,
			errorMsg:    "invalid proxy sslCertFile",
		},
		{
			name: "invalid_container_registry_proxy",
			config: ProxyConfig{
				ContainerRegistryProxy: "localhost:5050/../escape",
			},
			expectError: true,
			errorMsg:    "invalid proxy containerRegistryProxy",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.Validate()
			if tc.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
