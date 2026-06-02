// SPDX-License-Identifier: Apache-2.0

package network

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProbeTCP_SucceedsWhenListenerAccepts(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	// Accept-and-close loop so DialContext succeeds.
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	attempts, err := ProbeTCP(context.Background(), ln.Addr().String(), 5*time.Second, 1*time.Second, 100*time.Millisecond)
	require.NoError(t, err)
	assert.Equal(t, 1, attempts, "expected first-attempt success")
}

func TestProbeTCP_FailsWhenTargetUnreachable(t *testing.T) {
	t.Parallel()

	// Reserve a port then close it so the address is in a "connection refused"
	// state. Loopback gives us a fast refusal rather than relying on routing.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	deadAddr := ln.Addr().String()
	_ = ln.Close()

	overall := 600 * time.Millisecond
	start := time.Now()
	attempts, err := ProbeTCP(context.Background(), deadAddr, overall, 200*time.Millisecond, 100*time.Millisecond)
	elapsed := time.Since(start)

	require.Error(t, err, "expected error dialing closed port")
	assert.GreaterOrEqual(t, attempts, 2, "expected at least 2 retry attempts")
	assert.LessOrEqual(t, elapsed, overall+300*time.Millisecond, "ProbeTCP ran longer than overall budget")
}

func TestProbeTCP_RespectsParentContextCancel(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	deadAddr := ln.Addr().String()
	_ = ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err = ProbeTCP(ctx, deadAddr, 30*time.Second, 200*time.Millisecond, 100*time.Millisecond)
	elapsed := time.Since(start)

	require.Error(t, err, "expected error after parent cancel")
	assert.LessOrEqual(t, elapsed, 2*time.Second, "ProbeTCP did not exit promptly after parent cancel")
}

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
