// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeKubeconfig(t *testing.T, server string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "admin.conf")
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: c
  cluster:
    server: %s
contexts:
- name: c
  context: {cluster: c, user: u}
current-context: c
users:
- name: u
  user: {}
`, server)), 0o600))
	return path
}

// pointAdminKubeconfigAt makes the helpers' admin kubeconfig name server, and
// points $KUBECONFIG at another server that counts what reaches it: the
// helpers must never use it.
func pointAdminKubeconfigAt(t *testing.T, server string) (defaultHits *atomic.Int32) {
	t.Helper()
	defaultHits = new(atomic.Int32)
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		defaultHits.Add(1)
		http.Error(w, "not this one", http.StatusTeapot)
	}))
	t.Cleanup(other.Close)
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBECONFIG", writeKubeconfig(t, other.URL))

	orig := adminKubeconfigPath
	t.Cleanup(func() { adminKubeconfigPath = orig })
	adminKubeconfigPath = writeKubeconfig(t, server)
	return defaultHits
}

func closedPort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	return "http://" + addr
}

func requireAPIFailedNamingAdminConf(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	assert.True(t, apiDown(err), "want K8sAPIFailed, got %v", err)
	hints, _ := errx.Hints(err)
	assert.NotEmpty(t, hints)
	for _, h := range hints {
		assert.Contains(t, h, adminKubeconfigPath, "a hint must name the kubeconfig the migration used")
	}
}

func TestCiliumHelpers_ReadThroughTheAdminKubeconfig(t *testing.T) {
	defaultHits := pointAdminKubeconfigAt(t, closedPort(t))
	ctx := context.Background()

	_, _, err := defaultReadCiliumState(ctx)
	requireAPIFailedNamingAdminConf(t, err)

	_, _, _, err = defaultReadHostLegacyRoutingState(ctx)
	requireAPIFailedNamingAdminConf(t, err)

	_, err = defaultInstalledCiliumVersion(ctx)
	requireAPIFailedNamingAdminConf(t, err)

	err = defaultRestartCiliumAgents(ctx)
	requireAPIFailedNamingAdminConf(t, err)

	assert.Zero(t, defaultHits.Load(), "$KUBECONFIG must not be used")
}

// Once the patch has landed the agents are rolling, so a rollout the API
// server cannot report on is a failure, never a deferral.
func TestCiliumHelpers_RolloutWaitFailureIsNotADeferral(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"apiVersion":"apps/v1","kind":"DaemonSet","metadata":{"name":"cilium","namespace":"kube-system"}}`))
			return
		}
		http.Error(w, "etcd unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	pointAdminKubeconfigAt(t, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := defaultRestartCiliumAgents(ctx)

	require.Error(t, err)
	assert.False(t, apiDown(err), "the patch landed, so this must fail: %v", err)
}

// patchAnswering is an API server whose PATCH of the cilium DaemonSet answers
// status; everything else is unavailable.
func patchAnswering(t *testing.T, status int) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = fmt.Fprintf(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","code":%d,"reason":%q}`, status, http.StatusText(status))
			return
		}
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// No cilium DaemonSet will not appear by waiting, so the restart fails rather
// than deferring on every command.
func TestCiliumHelpers_MissingDaemonSetIsNotADeferral(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusUnprocessableEntity} {
		pointAdminKubeconfigAt(t, patchAnswering(t, status))

		err := defaultRestartCiliumAgents(context.Background())

		require.Error(t, err, "status %d", status)
		assert.False(t, apiDown(err), "status %d must fail, not defer: %v", status, err)
		// It fails every command until fixed, so it must say what to check.
		reason, _ := errx.ReasonOf(err)
		assert.Equal(t, reasons.PreconditionNotMet, reason, "status %d", status)
		hints, _ := errx.Hints(err)
		assert.Contains(t, strings.Join(hints, "\n"), "-n kube-system get daemonset cilium", "status %d", status)
	}
}

// Missing RBAC is the operator's to fix and must not block uninstall (R1.4),
// so it defers, with a hint naming the permission the patch needs.
func TestCiliumHelpers_ForbiddenPatchDefersWithThePatchPermission(t *testing.T) {
	pointAdminKubeconfigAt(t, patchAnswering(t, http.StatusForbidden))

	err := defaultRestartCiliumAgents(context.Background())

	require.True(t, apiDown(err), "want a deferral, got %v", err)
	hints, _ := errx.Hints(err)
	assert.Contains(t, strings.Join(hints, "\n"), "auth can-i patch daemonsets -n kube-system")
}

// A hung API server (up, with etcd down) must cost the command a bounded
// wait, then a deferral.
func TestCiliumHelpers_HungAPIServerDefersWithinTheTimeout(t *testing.T) {
	release := make(chan struct{})
	hung := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(hung.Close)
	t.Cleanup(func() { close(release) })
	pointAdminKubeconfigAt(t, hung.URL)
	orig := ciliumAPITimeout
	t.Cleanup(func() { ciliumAPITimeout = orig })
	ciliumAPITimeout = 100 * time.Millisecond

	start := time.Now()
	_, _, err := defaultReadCiliumState(context.Background())

	assert.True(t, apiDown(err), "want a deferral, got %v", err)
	assert.Less(t, time.Since(start), 3*time.Second)
}
