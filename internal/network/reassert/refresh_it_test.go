// SPDX-License-Identifier: Apache-2.0

//go:build integration && linux

package reassert

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/internal/network/policy"
)

// weaverCommentRe matches the comments render format 2 added.
var weaverCommentRe = regexp.MustCompile(` comment "weaver:[^"]*"`)

// provisionPolicyTable creates a real workload-policy table in the current
// render format from a temp registry, and returns its manager and artifact.
func provisionPolicyTable(t *testing.T, nft string) (*policy.Manager, string) {
	t.Helper()
	dir := t.TempDir()
	artifact := filepath.Join(dir, "workload-policy.nft")
	mgr := policy.NewManagerWithConfig(policy.Config{
		WeaverNftPath: artifact,
		RegistryDir:   filepath.Join(dir, "policies"),
		LockPath:      filepath.Join(dir, ".applying"),
		EnsureService: func(context.Context) error { return nil },
	})
	t.Cleanup(func() { _ = exec.Command(nft, "delete", "table", "inet", "weaver-workload-policy").Run() })

	p := &policy.Policy{
		Name: "bn-publisher", Action: policy.ActionStamp, Stamp: "publisher",
		Direction: policy.DirectionIngress, Ports: []string{"40840"},
	}
	_, err := mgr.Create(context.Background(), p, []string{"10.1.0.0/24", "10.2.0.1/32"}, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)
	return mgr, artifact
}

// provisionOlderPolicyTable is provisionPolicyTable reloaded without counters
// or weaver: comments, the way an upgraded host still has it.
func provisionOlderPolicyTable(t *testing.T, nft string) *policy.Manager {
	t.Helper()
	mgr, artifact := provisionPolicyTable(t, nft)

	doc, err := os.ReadFile(artifact)
	require.NoError(t, err)
	old := weaverCommentRe.ReplaceAllString(strings.ReplaceAll(string(doc), "counter ", ""), "")
	require.NotEqual(t, string(doc), old)
	cmd := exec.Command(nft, "-f", "-")
	cmd.Stdin = strings.NewReader(old)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
	return mgr
}

// Test_Reassert_ReloadsAnOlderFormatTable_Integration: on a real kernel, --check leaves the old table
// and a full run reloads it once, keeping every peer list.
func Test_Reassert_ReloadsAnOlderFormatTable_Integration(t *testing.T) {
	requireRoot(t)
	skipIfProvisioned(t)
	nft := requireNftBinary(t)
	ctx := context.Background()

	mgr := provisionOlderPolicyTable(t, nft)
	runner := policy.NewExecRunner()
	before, err := runner.ListElements(ctx, "bn-publisher")
	require.NoError(t, err)
	require.NotEmpty(t, before)
	v := kernelVerifierFor(mgr)

	checked := v.verify(ctx, verifyScope{policyLive: true})
	require.Equal(t, CheckOlderFormat, checked.Rules.Status, checked.Rules.Detail)
	require.Equal(t, 1, checked.Rules.LiveFormat)
	require.NotNil(t, checked.Counters)
	require.NotEqual(t, "0", checked.Counters.RulesEpoch, "the live table handle must be read from nft -j")

	c := v.verify(ctx, verifyScope{policyLive: true, refreshOlderFormat: true})

	assert.Equal(t, CheckOK, c.Rules.Status, c.Rules.Detail)
	assert.Equal(t, policy.FormatVersion, c.Rules.LiveFormat)
	assert.Contains(t, c.Rules.Detail, "reloaded the live rules")
	require.NotNil(t, c.Counters)
	assert.True(t, c.Counters.ForwardCounted)
	assert.Contains(t, c.Counters.RuleBytes, "publisher")
	assert.NotEqual(t, checked.Counters.RulesEpoch, c.Counters.RulesEpoch,
		"a reload restarts the counters, so the daemon must see a new epoch")
	after, err := runner.ListElements(ctx, "bn-publisher")
	require.NoError(t, err)
	assert.ElementsMatch(t, before, after, "the reload must keep every peer list")

	again := v.verify(ctx, verifyScope{policyLive: true, refreshOlderFormat: true})
	assert.Equal(t, CheckOK, again.Rules.Status)
	assert.Empty(t, again.Rules.Detail, "a current table is not reloaded again")
	assert.Zero(t, again.Rules.ReloadedFrom)
	require.NotNil(t, again.Counters)
	assert.Equal(t, c.Counters.RulesEpoch, again.Counters.RulesEpoch, "no reload, same epoch")
}
