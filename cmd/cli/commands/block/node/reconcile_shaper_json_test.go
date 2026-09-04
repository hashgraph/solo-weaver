// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package node

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/blocknode/shaper"
	"github.com/stretchr/testify/require"
)

// fakeStatusz serves the two statusz endpoints the reconciler reads, so the
// command can be driven end to end without a block node.
func fakeStatusz(t *testing.T) string {
	t.Helper()
	const inbound = `{"activeEndpoints":[
		{"local":{"address":"10.0.0.5","port":"40840"},"remote":{"address":"10.1.0.1","port":"*"},"category":"publisher"},
		{"local":{"address":"10.0.0.5","port":"40840"},"remote":{"address":"","port":""},"category":"public"}
	]}`
	const outbound = `{"activeEndpoints":[
		{"local":{"address":"10.0.0.5","port":"*"},"remote":{"address":"10.2.0.1","port":"50980"},"category":"partner"}
	]}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "inbound") {
			_, _ = io.WriteString(w, inbound)
			return
		}
		_, _ = io.WriteString(w, outbound)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// TestReconcileShaperCheck_JSONOutputIsExactlyOneDocument is the command-layer
// half of the machine-readable contract (the package-layer half is
// shaper.TestPackageEmitsNoLogs).
//
// privexec.ReconcileShaperCheck json.Unmarshals this command's whole stdout to
// read the digest back, so anything printed beside the document — a second
// object, a stray human-readable line, a progress note — faults the daemon's
// statusz poll loop into a retry-forever backoff while the command still exits
// 0. Decoding into a json.Decoder and asserting the stream ends after one value
// is what makes "extra output" fail here rather than in production.
func TestReconcileShaperCheck_JSONOutputIsExactlyOneDocument(t *testing.T) {
	prevFormat, prevURL, prevDry := common.OutputFormat, flagStatuszURL, flagReconcileDry
	t.Cleanup(func() {
		common.OutputFormat, flagStatuszURL, flagReconcileDry = prevFormat, prevURL, prevDry
	})
	common.OutputFormat = "json"
	flagStatuszURL = fakeStatusz(t)
	flagReconcileDry = true

	var out bytes.Buffer
	cmd := reconcileShaperCmd
	cmd.SetOut(&out)
	cmd.SetContext(context.Background())
	t.Cleanup(func() { cmd.SetOut(nil) })

	require.NoError(t, cmd.RunE(cmd, nil))

	dec := json.NewDecoder(bytes.NewReader(out.Bytes()))
	var doc map[string]any
	require.NoError(t, dec.Decode(&doc), "stdout must be valid JSON")

	var trailing any
	require.ErrorIs(t, dec.Decode(&trailing), io.EOF,
		"stdout carried more than one JSON value; the daemon decodes the whole stream and would fault:\n%s",
		out.String())

	require.Contains(t, doc, "desired-digest")
	require.NotEmpty(t, doc["desired-digest"], "an empty digest fails the daemon's contract check")
}

// TestReconcileShaperApply_JSONOutputIsExactlyOneDocument covers the apply half
// of the same contract. The apply summary is logged as well as printed, and
// under --output json that log line lands on stdout beside the document — so the
// emission has to stay inside the human-readable branch. Copilot caught this on
// review; the check-path guard above would never have.
//
// It renders a synthetic Result rather than running Apply, which needs root and
// a live nft table.
func TestReconcileShaperApply_JSONOutputIsExactlyOneDocument(t *testing.T) {
	prevFormat := common.OutputFormat
	t.Cleanup(func() { common.OutputFormat = prevFormat })
	common.OutputFormat = "json"

	var out bytes.Buffer
	cmd := reconcileShaperCmd
	cmd.SetOut(&out)
	t.Cleanup(func() { cmd.SetOut(nil) })

	require.NoError(t, renderApplyResult(cmd, shaper.Result{
		Applied:    []string{"bn-publisher"},
		Unchanged:  []string{"bn-restricted"},
		Digest:     "deadbeef",
		Unresolved: []string{"nx.example.invalid"},
	}))

	dec := json.NewDecoder(bytes.NewReader(out.Bytes()))
	var doc map[string]any
	require.NoError(t, dec.Decode(&doc), "stdout must be valid JSON")

	var trailing any
	require.ErrorIs(t, dec.Decode(&trailing), io.EOF,
		"stdout carried more than one JSON value:\n%s", out.String())

	require.Equal(t, "deadbeef", doc["digest"])
	require.Contains(t, doc, "unresolved")
}

// TestReconcileShaperCheck_OmitsUnresolvedWhenEverythingResolved keeps the
// `unresolved` field from becoming noise the daemon has to reason about: it is
// omitempty, so a clean tick carries no such key at all.
func TestReconcileShaperCheck_OmitsUnresolvedWhenEverythingResolved(t *testing.T) {
	prevFormat, prevURL, prevDry := common.OutputFormat, flagStatuszURL, flagReconcileDry
	t.Cleanup(func() {
		common.OutputFormat, flagStatuszURL, flagReconcileDry = prevFormat, prevURL, prevDry
	})
	common.OutputFormat = "json"
	flagStatuszURL = fakeStatusz(t)
	flagReconcileDry = true

	var out bytes.Buffer
	cmd := reconcileShaperCmd
	cmd.SetOut(&out)
	cmd.SetContext(context.Background())
	t.Cleanup(func() { cmd.SetOut(nil) })

	require.NoError(t, cmd.RunE(cmd, nil))

	var doc map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &doc))
	require.NotContains(t, doc, "unresolved",
		"the payload holds only literals, so nothing was left unresolved")
}
