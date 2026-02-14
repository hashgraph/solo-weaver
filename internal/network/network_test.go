// SPDX-License-Identifier: Apache-2.0

package network

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckEndpointReachable(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func() *httptest.Server
		urlOverride string // if set, use this URL instead of server URL
		timeout     time.Duration
		expectError bool
		errorMsg    string
	}{
		{
			name: "reachable endpoint returns 200",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
			},
			timeout:     5 * time.Second,
			expectError: false,
		},
		{
			name: "reachable endpoint returns 404 - still considered reachable",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			timeout:     5 * time.Second,
			expectError: false,
		},
		{
			name: "reachable endpoint returns 500 - still considered reachable",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}))
			},
			timeout:     5 * time.Second,
			expectError: false,
		},
		{
			name: "reachable endpoint returns 401 - still considered reachable",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
				}))
			},
			timeout:     5 * time.Second,
			expectError: false,
		},
		{
			name:        "empty URL returns nil",
			setupServer: nil,
			urlOverride: "",
			timeout:     5 * time.Second,
			expectError: false,
		},
		{
			name:        "invalid URL returns error",
			setupServer: nil,
			urlOverride: "://invalid-url",
			timeout:     5 * time.Second,
			expectError: true,
			errorMsg:    "invalid URL",
		},
		{
			name:        "unreachable endpoint returns error",
			setupServer: nil,
			urlOverride: "http://localhost:59999", // unlikely to have anything running here
			timeout:     1 * time.Second,
			expectError: true,
			errorMsg:    "is not reachable",
		},
		{
			name: "HEAD request is used",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodHead {
						t.Errorf("expected HEAD request, got %s", r.Method)
					}
					w.WriteHeader(http.StatusOK)
				}))
			},
			timeout:     5 * time.Second,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var url string
			var server *httptest.Server

			if tt.setupServer != nil {
				server = tt.setupServer()
				defer server.Close()
				url = server.URL
			}

			if tt.urlOverride != "" || tt.setupServer == nil {
				url = tt.urlOverride
			}

			ctx := context.Background()
			err := CheckEndpointReachable(ctx, url, tt.timeout)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCheckEndpointReachable_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := CheckEndpointReachable(ctx, server.URL, 10*time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not reachable")
}

func TestCheckEndpointReachable_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx := context.Background()
	err := CheckEndpointReachable(ctx, server.URL, 100*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not reachable")
}

func TestCheckEndpointReachable_URLPathStripped(t *testing.T) {
	// Verify that only the base URL (scheme + host) is used, not the full path
	var requestedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx := context.Background()
	// Add a path to the URL
	urlWithPath := server.URL + "/api/v1/write"
	err := CheckEndpointReachable(ctx, urlWithPath, 5*time.Second)

	require.NoError(t, err)
	// The path should be empty or "/" since we strip the path
	assert.True(t, requestedPath == "" || requestedPath == "/", "expected empty path, got %q", requestedPath)
}
