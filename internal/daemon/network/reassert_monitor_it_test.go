// SPDX-License-Identifier: Apache-2.0

//go:build integration && linux

package network

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/pkg/models"
)

// This runs against the installed, running daemon rather than an in-process
// monitor: the point is the whole loop — the tick, the sudo exec of
// `network reassert`, the JSON contract, and the history GET /status serves.
// It needs root, a provisioned host and the daemon unit, so it runs only in a
// VM after `block node install`.

const (
	nftBin       = "/usr/sbin/nft"
	systemctlBin = "/usr/bin/systemctl"
	// networkComponent mirrors daemon.ComponentNameNetwork by value; importing
	// internal/daemon here would be an import cycle.
	networkComponent = "network"
	nftLoaderUnit    = "solo-provisioner-network-nft.service"
)

// statusPollTimeout allows the startup grace, one interval and the worker's run.
var statusPollTimeout = reassertStartupGrace + reassertInterval + 45*time.Second

// nftTables maps the nft artifacts to the kernel tables they stand for.
var nftTables = map[string]string{
	"host-firewall":   "weaver-host-firewall",
	"workload-policy": "weaver-workload-policy",
}

func requireRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("requires root to delete nft tables and read the daemon socket")
	}
}

// statusEnvelope decodes only what this test needs from GET /status.
type statusEnvelope struct {
	Components map[string]struct {
		Detail json.RawMessage `json:"detail"`
	} `json:"components"`
}

func daemonClient(sock string) *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", sock)
			},
		},
	}
}

// networkSnapshot fetches the network component's history from the running
// daemon. ok is false when the daemon reports no such component.
func networkSnapshot(t *testing.T, c *http.Client) (snap Snapshot, ok bool) {
	t.Helper()
	resp, err := c.Get("http://daemon/status")
	require.NoError(t, err, "GET /status over the daemon socket")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var env statusEnvelope
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&env))
	comp, ok := env.Components[networkComponent]
	if !ok {
		return Snapshot{}, false
	}
	if len(comp.Detail) > 0 && string(comp.Detail) != "null" {
		require.NoError(t, json.Unmarshal(comp.Detail, &snap))
	}
	return snap, true
}

func artifactState(snap Snapshot, id string) (ArtifactState, bool) {
	for _, a := range snap.Artifacts {
		if a.Artifact == id {
			return a, true
		}
	}
	return ArtifactState{}, false
}

// tableLive shells out so the kernel check is independent of the code under test.
func tableLive(table string) bool {
	return exec.Command(nftBin, "list", "table", "inet", table).Run() == nil
}

// Test_DaemonLoop_RestoresDeletedTablesWithinOneInterval deletes the live weaver
// tables and waits for the daemon — not for the kernel — to report them
// restored. The reassert_count moving is what proves the daemon's own tick did
// the repair rather than some other actor.
func Test_DaemonLoop_RestoresDeletedTablesWithinOneInterval_Integration(t *testing.T) {
	requireRoot(t)

	sock := models.Paths().DaemonSockPath
	if _, err := os.Stat(sock); err != nil {
		t.Skipf("daemon socket %s absent; the daemon runs only on a provisioned host", sock)
	}
	c := daemonClient(sock)
	base, ok := networkSnapshot(t, c)
	require.True(t, ok, "the running daemon reports no %q component; the installed daemon must be built from this branch",
		networkComponent)

	// Targets are the nft artifacts the daemon itself says are provisioned.
	baseCount := map[string]int{}
	var targets []string
	for _, a := range base.Artifacts {
		if _, isNft := nftTables[a.Artifact]; isNft && a.Expected {
			targets = append(targets, a.Artifact)
			baseCount[a.Artifact] = a.ReassertCount
			require.True(t, a.Present, "precondition: %s must be live (%s)", a.Artifact, a.Detail)
			require.True(t, tableLive(nftTables[a.Artifact]),
				"precondition: inet %s must be live in the kernel", nftTables[a.Artifact])
		}
	}
	if len(targets) == 0 {
		t.Skip("no nft artifact provisioned on this host; run `block node install` first")
	}

	// Whatever happens below, the loader is re-run so the host is never left bare.
	t.Cleanup(func() {
		if err := exec.Command(systemctlBin, "restart", nftLoaderUnit).Run(); err != nil {
			t.Logf("WARNING: could not restore the weaver nft tables: %v", err)
		}
	})

	for _, id := range targets {
		table := nftTables[id]
		require.NoError(t, exec.Command(nftBin, "delete", "table", "inet", table).Run(),
			"could not delete inet %s", table)
		require.False(t, tableLive(table))
	}
	deletedAt := time.Now()

	restored := func(snap Snapshot) bool {
		for _, id := range targets {
			st, found := artifactState(snap, id)
			if !found || !st.Present || st.ReassertCount <= baseCount[id] {
				return false
			}
		}
		return true
	}

	deadline := time.Now().Add(statusPollTimeout)
	var last Snapshot
	for {
		last, _ = networkSnapshot(t, c)
		if restored(last) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the daemon did not restore %v within %s; last status: %+v",
				targets, statusPollTimeout, last)
		}
		time.Sleep(2 * time.Second)
	}
	t.Logf("daemon restored %v %s after deletion", targets, time.Since(deletedAt).Round(time.Second))

	for _, id := range targets {
		table := nftTables[id]
		assert.True(t, tableLive(table), "inet %s must be live in the kernel again", table)

		st, _ := artifactState(last, id)
		assert.True(t, st.Present, "%s: %+v", id, st)
		assert.False(t, st.ProbeFailed, "%s: %+v", id, st)
		assert.Equal(t, baseCount[id]+1, st.ReassertCount,
			"%s must have been restored by exactly one tick", id)
		assert.Zero(t, st.ConsecutiveFailures, "%s: %+v", id, st)
		require.NotNil(t, st.LastReassertedAt, "%s: last_reasserted_at must be set", id)
		assert.False(t, st.LastReassertedAt.Before(deletedAt.Add(-time.Second)),
			"%s: last_reasserted_at %s predates the deletion at %s", id, st.LastReassertedAt, deletedAt)
	}
	assert.Empty(t, last.LastError, "the worker exec must have succeeded")
	require.NotNil(t, last.LastCheckAt)
	assert.False(t, last.LastCheckAt.Before(deletedAt.Add(-time.Second)),
		"last_check_at must reflect the repairing tick")
}
