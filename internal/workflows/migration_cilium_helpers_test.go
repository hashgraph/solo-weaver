// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
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

// defers reports whether a Cilium migration meeting err would defer.
func defers(err error) bool {
	return errorx.IsOfType(deferIfAPIDown(err), migration.ErrDeferred)
}

func requireAPIFailedNamingAdminConf(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	reason, _ := errx.ReasonOf(err)
	assert.Equal(t, reasons.K8sAPIFailed, reason, "got %v", err)
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
	assert.True(t, defers(err), "got %v", err)

	_, _, _, err = defaultReadHostLegacyRoutingState(ctx)
	requireAPIFailedNamingAdminConf(t, err)
	assert.True(t, defers(err), "got %v", err)

	_, err = defaultInstalledCiliumVersion(ctx)
	requireAPIFailedNamingAdminConf(t, err)
	assert.True(t, defers(err), "got %v", err)

	require.Error(t, defaultRestartCiliumAgents(ctx))

	assert.Zero(t, defaultHits.Load(), "$KUBECONFIG must not be used")
}

// answering is an API server that answers every request with status.
func answering(t *testing.T, status int) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","code":%d}`, status)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// Connection failures an httptest server can't stage quickly defer too: client-go
// retries a dropped connection until the timeout, and a dial error other than a
// refusal needs a real network. Each is built the way client-go returns it.
func TestCiliumHelpers_ConnectionFailuresMeanTheAPICannotBeReached(t *testing.T) {
	api := "https://192.0.2.1:6443/api"
	for name, cause := range map[string]error{
		"no route to host":   &url.Error{Op: "Get", URL: api, Err: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.EHOSTUNREACH}},
		"no such host":       &url.Error{Op: "Get", URL: api, Err: &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "cp.example", IsNotFound: true}}},
		"connection reset":   &url.Error{Op: "Get", URL: api, Err: &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}},
		"connection dropped": &url.Error{Op: "Get", URL: api, Err: io.EOF},
	} {
		t.Run(name, func(t *testing.T) {
			assert.True(t, apiUnreachable(clusterAPIError(cause, "cannot read kube-system/cilium-config")))
		})
	}
}

// hung is an API server that never answers.
func hung(t *testing.T) string {
	t.Helper()
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })
	return server.URL
}

// requiringClientCert is a TLS API server that rejects a client without a
// certificate, behind a kubeconfig that skips server verification.
func requiringClientCert(t *testing.T) string {
	t.Helper()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server.URL
}

// A connection error, a timeout, or a "try later" answer (503, 429) means the
// API can't be reached; a refusal or a bad admin kubeconfig won't clear by
// waiting, so it fails.
func TestCiliumHelpers_DeferOnlyWhenTheAPICannotBeReached(t *testing.T) {
	orig := ciliumAPITimeout
	t.Cleanup(func() { ciliumAPITimeout = orig })

	cases := []struct {
		name     string
		server   func(t *testing.T) string
		defers   bool
		certHint bool
	}{
		{"connection refused", closedPort, true, false},
		{"hung API server", hung, true, false},
		{"504 gateway timeout", func(t *testing.T) string { return answering(t, http.StatusGatewayTimeout) }, true, false},
		{"403 forbidden", func(t *testing.T) string { return answering(t, http.StatusForbidden) }, false, false},
		{"401 unauthorized", func(t *testing.T) string { return answering(t, http.StatusUnauthorized) }, false, true},
		{"503 unavailable", func(t *testing.T) string { return answering(t, http.StatusServiceUnavailable) }, true, false},
		{"429 too many requests", func(t *testing.T) string { return answering(t, http.StatusTooManyRequests) }, true, false},
		{"untrusted server certificate", func(t *testing.T) string {
			server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			t.Cleanup(server.Close)
			return server.URL
		}, false, true},
		{"client certificate rejected", requiringClientCert, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Only the hung server waits out the timeout; the others keep the
			// real one, so a slow TLS handshake can't turn into a timeout.
			ciliumAPITimeout = orig
			if tc.name == "hung API server" {
				ciliumAPITimeout = 100 * time.Millisecond
			}
			pointAdminKubeconfigAt(t, tc.server(t))
			if tc.name == "client certificate rejected" {
				skipServerVerification(t)
			}

			_, _, err := defaultReadCiliumState(context.Background())

			require.Error(t, err)
			assert.Equal(t, tc.defers, defers(err), "got %v", err)
			if !tc.defers {
				hints, _ := errx.Hints(err)
				assert.Equal(t, tc.certHint, slices.Contains(hints, certificatesHint), "hints %v", hints)
			}
		})
	}
}

// skipServerVerification rewrites the admin kubeconfig to trust any server
// certificate, so only the client side of the handshake can fail.
func skipServerVerification(t *testing.T) {
	t.Helper()
	b, err := os.ReadFile(adminKubeconfigPath)
	require.NoError(t, err)
	out := strings.Replace(string(b), "    server: ", "    insecure-skip-tls-verify: true\n    server: ", 1)
	require.NoError(t, os.WriteFile(adminKubeconfigPath, []byte(out), 0o600))
}

func TestCiliumHelpers_MalformedAdminKubeconfigFails(t *testing.T) {
	pointAdminKubeconfigAt(t, closedPort(t))
	require.NoError(t, os.WriteFile(adminKubeconfigPath, []byte("not: [a kubeconfig"), 0o600))

	_, _, err := defaultReadCiliumState(context.Background())

	require.Error(t, err)
	assert.False(t, defers(err), "got %v", err)
}

// A hung API server (up, with etcd down) must cost the command a bounded
// wait, then a deferral.
func TestCiliumHelpers_HungAPIServerDefersWithinTheTimeout(t *testing.T) {
	pointAdminKubeconfigAt(t, hung(t))
	orig := ciliumAPITimeout
	t.Cleanup(func() { ciliumAPITimeout = orig })
	ciliumAPITimeout = 100 * time.Millisecond

	start := time.Now()
	_, _, err := defaultReadCiliumState(context.Background())

	assert.True(t, defers(err), "want a deferral, got %v", err)
	assert.Less(t, time.Since(start), 3*time.Second)
}

// No cilium DaemonSet will not appear by waiting, so the version lookup fails
// rather than defers.
func TestCiliumHelpers_MissingDaemonSetFails(t *testing.T) {
	pointAdminKubeconfigAt(t, answering(t, http.StatusNotFound))

	_, err := defaultInstalledCiliumVersion(context.Background())

	require.Error(t, err)
	assert.False(t, defers(err), "got %v", err)
	reason, _ := errx.ReasonOf(err)
	assert.Equal(t, reasons.PreconditionNotMet, reason, "got %v", err)
}

// The version lookup replaces its hints, so it must add the certificates hint
// itself.
func TestCiliumHelpers_VersionLookupNamesTheCertificatesOnA401(t *testing.T) {
	pointAdminKubeconfigAt(t, answering(t, http.StatusUnauthorized))

	_, err := defaultInstalledCiliumVersion(context.Background())

	require.Error(t, err)
	hints, _ := errx.Hints(err)
	assert.Contains(t, hints, certificatesHint)
}
