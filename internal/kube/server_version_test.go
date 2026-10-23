// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package kube

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerVersion_ReturnsGitVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/version", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"major":"1","minor":"33","gitVersion":"v1.33.4"}`))
	}))
	t.Cleanup(server.Close)
	useKubeconfig(t, server.URL)

	v, err := ServerVersion(3 * time.Second)
	require.NoError(t, err)
	assert.Equal(t, "v1.33.4", v)
}

func TestServerVersion_ErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	useKubeconfig(t, server.URL)

	_, err := ServerVersion(3 * time.Second)
	require.Error(t, err)
}

func TestServerVersion_NoServer(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	useKubeconfig(t, "http://"+addr)

	_, err = ServerVersion(3 * time.Second)
	require.Error(t, err)
}
