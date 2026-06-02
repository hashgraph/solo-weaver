// SPDX-License-Identifier: Apache-2.0

package cilium

import (
	"testing"
	"time"
)

// TestAgentDaemonSetIdentity guards the well-known location of weaver's Cilium
// install (kube-system/cilium). If these change, the embedded chart in
// internal/templates/files/cilium/ must change in lockstep — surfacing as a
// test failure here so the two don't silently drift apart.
func TestAgentDaemonSetIdentity(t *testing.T) {
	t.Parallel()
	if AgentDaemonSetNamespace != "kube-system" {
		t.Fatalf("AgentDaemonSetNamespace = %q, want %q", AgentDaemonSetNamespace, "kube-system")
	}
	if AgentDaemonSetName != "cilium" {
		t.Fatalf("AgentDaemonSetName = %q, want %q", AgentDaemonSetName, "cilium")
	}
}

func TestDefaultRolloutTimeout_IsReasonable(t *testing.T) {
	t.Parallel()
	if DefaultRolloutTimeout < 30*time.Second || DefaultRolloutTimeout > 5*time.Minute {
		t.Fatalf("DefaultRolloutTimeout = %v outside the [30s, 5m] sanity range — too tight risks flakes, too loose hides bugs", DefaultRolloutTimeout)
	}
}
