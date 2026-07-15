// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRenderWeaverNft_EmptyRegistry(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "network-weaver.nft")

	// Missing registry dir is treated as an empty registry: must be a no-op.
	// Writing a forward chain with policy drop for zero policies would silently
	// block all new forwarded traffic (pod startup, image pulls, DNS).
	require.NoError(t, RenderWeaverNft(filepath.Join(dir, "policies"), out, ""))

	_, err := os.Stat(out)
	require.True(t, os.IsNotExist(err), "RenderWeaverNft must not write a file for an empty registry")
}

func TestRenderWeaverNft_EmptyRegistryRemovesStaleFile(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "network-weaver.nft")

	// A stale file left over from a previous non-empty render. An empty registry
	// must remove it so the boot oneshot never replays a harmful/old table.
	require.NoError(t, os.WriteFile(out, []byte("table inet weaver { }\n"), 0o644))

	require.NoError(t, RenderWeaverNft(filepath.Join(dir, "policies"), out, ""))

	_, err := os.Stat(out)
	require.True(t, os.IsNotExist(err), "stale network-weaver.nft must be removed for an empty registry")
}

func TestRenderWeaverNft_DenyPolicy(t *testing.T) {
	dir := t.TempDir()
	regDir := filepath.Join(dir, "policies")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	out := filepath.Join(dir, "network-weaver.nft")

	p := &Policy{
		Name:      "bn-restricted",
		Action:    ActionDeny,
		CreatedAt: time.Now().UTC(),
	}
	data, err := json.MarshalIndent(p, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "bn-restricted.json"), append(data, '\n'), 0o644))

	require.NoError(t, RenderWeaverNft(regDir, out, ""))

	content, err := os.ReadFile(out)
	require.NoError(t, err)
	require.Contains(t, string(content), "bn-restricted")
	require.Contains(t, string(content), "drop")
}

func TestRenderWeaverNft_IdempotentWrite(t *testing.T) {
	dir := t.TempDir()
	regDir := filepath.Join(dir, "policies")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	out := filepath.Join(dir, "network-weaver.nft")

	// Use a deny policy so the registry is non-empty and a file is actually written.
	p := &Policy{
		Name:      "bn-restricted",
		Action:    ActionDeny,
		CreatedAt: time.Now().UTC(),
	}
	data, err := json.MarshalIndent(p, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "bn-restricted.json"), append(data, '\n'), 0o644))

	require.NoError(t, RenderWeaverNft(regDir, out, ""))

	first, err := os.ReadFile(out)
	require.NoError(t, err)
	firstSum := sha256.Sum256(first)

	// Stat the file to detect if the second call re-writes it.
	info1, err := os.Stat(out)
	require.NoError(t, err)

	require.NoError(t, RenderWeaverNft(regDir, out, ""))

	info2, err := os.Stat(out)
	require.NoError(t, err)
	require.Equal(t, info1.ModTime(), info2.ModTime(), "second call must not re-write an unchanged file")

	second, err := os.ReadFile(out)
	require.NoError(t, err)
	require.Equal(t, firstSum, sha256.Sum256(second))
}

func TestRenderWeaverNft_CorruptRegistry(t *testing.T) {
	dir := t.TempDir()
	regDir := filepath.Join(dir, "policies")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	out := filepath.Join(dir, "network-weaver.nft")

	require.NoError(t, os.WriteFile(filepath.Join(regDir, "bad.json"), []byte("not json"), 0o644))

	err := RenderWeaverNft(regDir, out, "")
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "bad.json") || strings.Contains(err.Error(), "parse") || strings.Contains(err.Error(), "json"),
		"error should mention the corrupt file: %v", err)
}

func TestRenderWeaverNft_RecoversPodCIDRFromExistingFile(t *testing.T) {
	dir := t.TempDir()
	regDir := filepath.Join(dir, "policies")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	out := filepath.Join(dir, "network-weaver.nft")

	// Seed the output file with content that embeds the pod CIDR so
	// ExtractPodCIDR can recover it when podCIDR="" is passed.
	const podCIDR = "10.244.0.0/16"
	require.NoError(t, os.WriteFile(out, []byte("ip daddr "+podCIDR+" placeholder\n"), 0o644))

	// Use a stamp policy — it exercises needsPodCIDR()=true, which triggers
	// the recovery path. Direction is derived by Validate from the stamp class
	// ("publisher" → DirectionIngress); we mirror the JSON Manager.Create persists.
	p := &Policy{
		Name:      "bn-publisher",
		Action:    ActionStamp,
		Stamp:     "publisher",
		Direction: DirectionIngress,
		Ports:     []string{"40840"},
		CreatedAt: time.Now().UTC(),
	}
	data, err := json.MarshalIndent(p, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "bn-publisher.json"), append(data, '\n'), 0o644))

	require.NoError(t, RenderWeaverNft(regDir, out, ""))
	content, err := os.ReadFile(out)
	require.NoError(t, err)
	require.Contains(t, string(content), podCIDR, "rendered output must include the pod CIDR recovered from the existing file")
}
