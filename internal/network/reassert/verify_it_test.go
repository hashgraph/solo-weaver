// SPDX-License-Identifier: Apache-2.0

//go:build integration && linux

package reassert

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test_Verify_RulesCheckOnALiveTable_Integration: only a real kernel shows that a table
// updated the production way reads back like the expected one.
func Test_Verify_RulesCheckOnALiveTable_Integration(t *testing.T) {
	requireRoot(t)
	skipIfProvisioned(t)
	nft := requireNftBinary(t)
	ctx := context.Background()

	mgr, _ := provisionPolicyTable(t, nft)
	v := kernelVerifierFor(mgr)

	fresh := v.verify(ctx, verifyScope{policyLive: true})
	require.Equal(t, CheckOK, fresh.Rules.Status, "%+v", fresh.Rules)

	require.NoError(t, mgr.Add(ctx, "bn-publisher", []string{"10.3.0.7/32"}))
	added := v.verify(ctx, verifyScope{policyLive: true})
	require.Equal(t, CheckOK, added.Rules.Status, "a peer added later is not drift: %+v", added.Rules)

	out, err := exec.Command(nft, "add", "rule", "inet", "weaver-workload-policy", "forward_ipv4",
		"ip", "saddr", "10.9.9.9", "drop").CombinedOutput()
	require.NoError(t, err, "%s", out)

	edited := v.verify(ctx, verifyScope{policyLive: true, refreshOlderFormat: true})

	assert.Equal(t, CheckFailed, edited.Rules.Status)
	assert.Empty(t, edited.Rules.Missing)
	require.Len(t, edited.Rules.Unexpected, 1)
	assert.Contains(t, edited.Rules.Unexpected[0], "10.9.9.9")
	out, err = exec.Command(nft, "list", "chain", "inet", "weaver-workload-policy", "forward_ipv4").CombinedOutput()
	require.NoError(t, err, "%s", out)
	assert.Contains(t, string(out), "10.9.9.9", "drift is reported, never overwritten")
}
