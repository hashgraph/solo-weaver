// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"testing"

	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRemoteFlags(t *testing.T) {
	tests := []struct {
		name        string
		flags       []string
		expected    []config.AlloyRemoteConfig
		expectError bool
		errorMsg    string
	}{
		{
			name:     "empty flags",
			flags:    []string{},
			expected: nil,
		},
		{
			name:     "nil flags",
			flags:    nil,
			expected: nil,
		},
		{
			name:  "single remote with http URL and port",
			flags: []string{"name=local,url=http://192.168.1.100:9090/api/v1/write,username=admin"},
			expected: []config.AlloyRemoteConfig{
				{
					Name:     "local",
					URL:      "http://192.168.1.100:9090/api/v1/write",
					Username: "admin",
				},
			},
		},
		{
			name:  "single remote with https URL",
			flags: []string{"name=primary,url=https://prom1.example.com/api/v1/write,username=user1"},
			expected: []config.AlloyRemoteConfig{
				{
					Name:     "primary",
					URL:      "https://prom1.example.com/api/v1/write",
					Username: "user1",
				},
			},
		},
		{
			name: "multiple remotes",
			flags: []string{
				"name=primary,url=https://prom1.example.com/api/v1/write,username=user1",
				"name=backup,url=https://prom2.example.com/api/v1/write,username=user2",
			},
			expected: []config.AlloyRemoteConfig{
				{
					Name:     "primary",
					URL:      "https://prom1.example.com/api/v1/write",
					Username: "user1",
				},
				{
					Name:     "backup",
					URL:      "https://prom2.example.com/api/v1/write",
					Username: "user2",
				},
			},
		},
		{
			name:  "URL with explicit port",
			flags: []string{"name=grafana-cloud,url=https://logs.grafana.net:443/loki/api/v1/push,username=12345"},
			expected: []config.AlloyRemoteConfig{
				{
					Name:     "grafana-cloud",
					URL:      "https://logs.grafana.net:443/loki/api/v1/push",
					Username: "12345",
				},
			},
		},
		{
			name:  "without username (optional)",
			flags: []string{"name=local,url=http://localhost:9090/api/v1/write"},
			expected: []config.AlloyRemoteConfig{
				{
					Name:     "local",
					URL:      "http://localhost:9090/api/v1/write",
					Username: "",
				},
			},
		},
		{
			name:  "keys in different order",
			flags: []string{"url=http://localhost:9090/api/v1/write,username=admin,name=local"},
			expected: []config.AlloyRemoteConfig{
				{
					Name:     "local",
					URL:      "http://localhost:9090/api/v1/write",
					Username: "admin",
				},
			},
		},
		{
			name:  "URL with commas in query parameters",
			flags: []string{"name=complex,url=http://example.com/api?labels=a,b,c&other=value,username=admin"},
			expected: []config.AlloyRemoteConfig{
				{
					Name:     "complex",
					URL:      "http://example.com/api?labels=a,b,c&other=value",
					Username: "admin",
				},
			},
		},
		{
			name:        "missing name key",
			flags:       []string{"url=http://localhost:9090/api/v1/write,username=admin"},
			expectError: true,
			errorMsg:    "missing required 'name'",
		},
		{
			name:        "missing url key",
			flags:       []string{"name=local,username=admin"},
			expectError: true,
			errorMsg:    "missing required 'url'",
		},
		{
			name:        "unknown key is rejected",
			flags:       []string{"name=local,url=http://localhost:9090,password=secret"},
			expectError: true,
			errorMsg:    "unknown key",
		},
		{
			name:        "missing equals sign",
			flags:       []string{"name:local,url=http://localhost:9090"},
			expectError: true,
			errorMsg:    "invalid key=value pair",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseRemoteFlags(tt.flags)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseRemoteFlags_URLPatterns(t *testing.T) {
	// Test various real-world URL patterns to ensure parsing is robust
	tests := []struct {
		name     string
		input    string
		wantName string
		wantURL  string
		wantUser string
	}{
		{
			name:     "localhost with port",
			input:    "name=dev,url=http://localhost:9090/api/v1/write,username=testuser",
			wantName: "dev",
			wantURL:  "http://localhost:9090/api/v1/write",
			wantUser: "testuser",
		},
		{
			name:     "IP address with port",
			input:    "name=prod,url=http://10.0.0.1:9090/api/v1/write,username=admin",
			wantName: "prod",
			wantURL:  "http://10.0.0.1:9090/api/v1/write",
			wantUser: "admin",
		},
		{
			name:     "HTTPS without explicit port",
			input:    "name=cloud,url=https://metrics.example.com/api/v1/write,username=clouduser",
			wantName: "cloud",
			wantURL:  "https://metrics.example.com/api/v1/write",
			wantUser: "clouduser",
		},
		{
			name:     "Loki push endpoint",
			input:    "name=loki,url=http://loki.monitoring.svc:3100/loki/api/v1/push,username=lokiuser",
			wantName: "loki",
			wantURL:  "http://loki.monitoring.svc:3100/loki/api/v1/push",
			wantUser: "lokiuser",
		},
		{
			name:     "Grafana Cloud style",
			input:    "name=grafana,url=https://prometheus-prod-01-eu-west-0.grafana.net/api/prom/push,username=123456",
			wantName: "grafana",
			wantURL:  "https://prometheus-prod-01-eu-west-0.grafana.net/api/prom/push",
			wantUser: "123456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseRemoteFlags([]string{tt.input})

			require.NoError(t, err)
			require.Len(t, result, 1)

			assert.Equal(t, tt.wantName, result[0].Name)
			assert.Equal(t, tt.wantURL, result[0].URL)
			assert.Equal(t, tt.wantUser, result[0].Username)
		})
	}
}
