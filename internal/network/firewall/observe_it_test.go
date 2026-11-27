// SPDX-License-Identifier: Apache-2.0

//go:build integration

package firewall

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/network/nftexec"
	"github.com/stretchr/testify/require"
)

// realTable is a table loaded with the real nft into a throwaway network
// namespace, so the host's own firewall is never touched.
type realTable struct {
	ip, nft, ns string
}

// loadRealTable renders config into a fresh namespace and returns a Manager
// that reads it through nftexec.ListTable, as production does.
func loadRealTable(t *testing.T, config string) (*Manager, realTable) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}
	nftBin, ok := nftexec.Binary()
	if !ok {
		t.Skipf("nft not found (looked for %s)", nftBin)
	}
	rt := realTable{ip: resolveBin(t, "ip", ipBinCandidates), nft: nftBin}
	rt.ns = newNetns(t, rt.ip, "solo-weaver-observe-it")

	cfg, err := ParseConfig([]byte(config))
	require.NoError(t, err)
	table, err := cfg.Table()
	require.NoError(t, err)
	ruleset, err := table.Render()
	require.NoError(t, err)
	dir := t.TempDir()
	loadInNetns(t, rt.ip, rt.nft, rt.ns, writeDoc(t, dir, "ruleset.nft", ruleset))

	// nft run through `ip netns exec`, so ListTable's own parsing is what reads it.
	nftInNs := writeDoc(t, dir, "nft-in-netns",
		"#!/bin/sh\nexec "+rt.ip+" netns exec "+rt.ns+" "+rt.nft+" \"$@\"\n")
	require.NoError(t, os.Chmod(nftInNs, 0o755))

	m := NewManagerWithConfig(Config{
		NftPath:    filepath.Join(dir, "fw.nft"),
		ConfigPath: filepath.Join(dir, "fw.yaml"),
		LockPath:   filepath.Join(dir, "run", ".applying"),
	})
	m.listTable = func(ctx context.Context) (string, bool, error) {
		return nftexec.ListTable(ctx, nftInNs, TableName)
	}
	writeFile(t, m.configPath, config)
	return m, rt
}

// run runs one nft command inside the namespace.
func (rt realTable) run(t *testing.T, command string) {
	t.Helper()
	args := append([]string{"netns", "exec", rt.ns, rt.nft}, strings.Fields(command)...)
	out, err := exec.Command(rt.ip, args...).CombinedOutput()
	require.NoError(t, err, string(out))
}

func TestObserveDrift_RealTableMatchesItsConfig_Integration(t *testing.T) {
	m, _ := loadRealTable(t, goldenConfig)

	got := m.ObserveDrift(context.Background())

	require.False(t, got.Unobserved)
	require.True(t, got.TablePresent)
	require.True(t, got.ConfigPresent)
	require.NotEmpty(t, got.Fields)
	for _, f := range got.Fields {
		require.Equal(t, f.Config, f.Live, f.Field)
	}
}

func TestObserveDrift_RealHandEditsShowOnTheLiveSide_Integration(t *testing.T) {
	m, rt := loadRealTable(t, goldenConfig)
	rt.run(t, "add element inet weaver-host-firewall mgmt_addrs { 203.0.113.7 }")
	rt.run(t, "add element inet weaver-host-firewall mgmt_ports { 23 }")
	rt.run(t, "flush set inet weaver-host-firewall blocked_addrs")

	fields := fieldsByName(m.ObserveDrift(context.Background()))

	require.Equal(t, []string{"10.0.0.0/8", "192.168.0.0/16", "203.0.113.7/32"}, fields["configFile.mgmt.cidrs"].Live)
	require.Equal(t, []string{"22-23"}, fields["configFile.mgmt.ports"].Live)
	require.Empty(t, fields["configFile.blocked.cidrs"].Live)
}

func TestObserveDrift_RealTableAbsent_Integration(t *testing.T) {
	m, rt := loadRealTable(t, goldenConfig)
	rt.run(t, "delete table inet weaver-host-firewall")

	got := m.ObserveDrift(context.Background())

	require.False(t, got.Unobserved)
	require.False(t, got.TablePresent)
	require.True(t, got.ConfigPresent)
}
