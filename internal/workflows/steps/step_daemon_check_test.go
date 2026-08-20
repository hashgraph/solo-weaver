// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package steps

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/daemonkit"
	"github.com/hashgraph/solo-weaver/internal/daemon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serveDaemonStatus starts an HTTP server on a unix socket answering GET /status
// with the given response, and returns the socket path.
func serveDaemonStatus(t *testing.T, status daemon.StatusResponse) string {
	t.Helper()
	sockPath := filepath.Join(t.TempDir(), "d.sock")
	l, err := net.Listen("unix", sockPath)
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(status))
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })
	return sockPath
}

func runCheckStep(t *testing.T, sockPath string, expect ...string) *automa.Report {
	t.Helper()
	step, err := CheckDaemonComponentPrerequisitesStep(sockPath, expect...).Build()
	require.NoError(t, err)
	return step.Execute(context.Background())
}

func TestCheckDaemonComponentPrerequisitesStep_SocketUnreachableSkips(t *testing.T) {
	// No server behind the path — the fresh-install case, where the daemon is
	// provisioned only after the workflow.
	report := runCheckStep(t, filepath.Join(t.TempDir(), "absent.sock"), daemon.ComponentNameBlockNode)
	require.NoError(t, report.Error)
	assert.Equal(t, automa.StatusSkipped, report.Status)
}

func TestCheckDaemonComponentPrerequisitesStep_ExpectedComponentPresent(t *testing.T) {
	sockPath := serveDaemonStatus(t, daemon.StatusResponse{
		Components: map[string]daemon.ComponentStatus{
			daemon.ComponentNameBlockNode: {Monitors: map[string]daemonkit.MonitorState{
				"bn-traffic-shaper-monitor": {State: "running"},
			}},
		},
	})

	report := runCheckStep(t, sockPath, daemon.ComponentNameBlockNode)
	require.NoError(t, report.Error)
	assert.Equal(t, automa.StatusSuccess, report.Status)
}

func TestCheckDaemonComponentPrerequisitesStep_ExpectedComponentMissingFails(t *testing.T) {
	// Daemon reachable but it built no components — e.g. its kubeconfig no
	// longer reaches the cluster.
	sockPath := serveDaemonStatus(t, daemon.StatusResponse{
		Components: map[string]daemon.ComponentStatus{},
	})

	report := runCheckStep(t, sockPath, daemon.ComponentNameBlockNode)
	require.Error(t, report.Error)
	assert.Equal(t, automa.StatusFailed, report.Status)
	assert.Contains(t, report.Error.Error(), daemon.ComponentNameBlockNode)
}

func TestCheckDaemonComponentPrerequisitesStep_NoExpectationsTolerateEmpty(t *testing.T) {
	// Without expectations an empty component set is fine — the existing
	// contract of NewDaemonServiceInstallWorkflow and `daemon service check`.
	sockPath := serveDaemonStatus(t, daemon.StatusResponse{
		Components: map[string]daemon.ComponentStatus{},
	})

	report := runCheckStep(t, sockPath)
	require.NoError(t, report.Error)
	assert.Equal(t, automa.StatusSuccess, report.Status)
}

func TestCheckDaemonComponentPrerequisitesStep_DegradedMonitorFails(t *testing.T) {
	sockPath := serveDaemonStatus(t, daemon.StatusResponse{
		Components: map[string]daemon.ComponentStatus{
			daemon.ComponentNameBlockNode: {Monitors: map[string]daemonkit.MonitorState{
				"bn-traffic-shaper-monitor": {
					State: "degraded",
					Error: &daemonkit.StatusError{Reason: "RBACMissing", Message: "watch failed"},
				},
			}},
		},
	})

	report := runCheckStep(t, sockPath, daemon.ComponentNameBlockNode)
	require.Error(t, report.Error)
	assert.Equal(t, automa.StatusFailed, report.Status)
}
