// SPDX-License-Identifier: Apache-2.0

//go:build linux

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
			flags: []string{"local:http://192.168.1.100:9090/api/v1/write:admin"},
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
			flags: []string{"primary:https://prom1.example.com/api/v1/write:user1"},
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
				"primary:https://prom1.example.com/api/v1/write:user1",
				"backup:https://prom2.example.com/api/v1/write:user2",
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
			name:  "URL with multiple ports (edge case)",
			flags: []string{"grafana-cloud:https://logs.grafana.net:443/loki/api/v1/push:12345"},
			expected: []config.AlloyRemoteConfig{
				{
					Name:     "grafana-cloud",
					URL:      "https://logs.grafana.net:443/loki/api/v1/push",
					Username: "12345",
				},
			},
		},
		{
			name:  "empty username is allowed",
			flags: []string{"local:http://localhost:9090/api/v1/write:"},
			expected: []config.AlloyRemoteConfig{
				{
					Name:     "local",
					URL:      "http://localhost:9090/api/v1/write",
					Username: "",
				},
			},
		},
		{
			name:  "whitespace is trimmed",
			flags: []string{" local : http://localhost:9090/api/v1/write : admin "},
			expected: []config.AlloyRemoteConfig{
				{
					Name:     "local",
					URL:      "http://localhost:9090/api/v1/write",
					Username: "admin",
				},
			},
		},
		{
			name:        "missing colon - invalid format",
			flags:       []string{"invalid-no-colon"},
			expectError: true,
			errorMsg:    "expected name:url:username",
		},
		{
			name:        "only one colon - missing username separator",
			flags:       []string{"name:urlwithoutusername"},
			expectError: true,
			errorMsg:    "expected name:url:username",
		},
		{
			name:        "empty name",
			flags:       []string{":http://localhost:9090:admin"},
			expectError: true,
			errorMsg:    "remote name cannot be empty",
		},
		{
			name:        "empty URL",
			flags:       []string{"local::admin"},
			expectError: true,
			errorMsg:    "remote URL cannot be empty",
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
			input:    "dev:http://localhost:9090/api/v1/write:testuser",
			wantName: "dev",
			wantURL:  "http://localhost:9090/api/v1/write",
			wantUser: "testuser",
		},
		{
			name:     "IP address with port",
			input:    "prod:http://10.0.0.1:9090/api/v1/write:admin",
			wantName: "prod",
			wantURL:  "http://10.0.0.1:9090/api/v1/write",
			wantUser: "admin",
		},
		{
			name:     "HTTPS without explicit port",
			input:    "cloud:https://metrics.example.com/api/v1/write:clouduser",
			wantName: "cloud",
			wantURL:  "https://metrics.example.com/api/v1/write",
			wantUser: "clouduser",
		},
		{
			name:     "Loki push endpoint",
			input:    "loki:http://loki.monitoring.svc:3100/loki/api/v1/push:lokiuser",
			wantName: "loki",
			wantURL:  "http://loki.monitoring.svc:3100/loki/api/v1/push",
			wantUser: "lokiuser",
		},
		{
			name:     "Grafana Cloud style",
			input:    "grafana:https://prometheus-prod-01-eu-west-0.grafana.net/api/prom/push:123456",
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
