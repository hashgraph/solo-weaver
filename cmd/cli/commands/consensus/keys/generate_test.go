// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cnkeys "github.com/hashgraph/solo-weaver/internal/consensus/keys"
	corekeys "github.com/hashgraph/solo-weaver/pkg/consensus/keys"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runGenerateCmd executes `keys generate` with args against a fresh command tree
// and returns combined output. Flags are package-level vars, so reset them first.
func runGenerateCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	flagKeysDir, flagIdentity, flagMachineID = "", "", ""
	flagGossip, flagTLS = false, false
	flagGenerateForce = false
	resetGrpcFlags(t, generateCmd)

	root := GetCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(append([]string{"generate"}, args...))
	err := root.Execute()
	return buf.String(), err
}

func TestGenerateCmd_NodeIdFree(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out")
	out, err := runGenerateCmd(t, "--keys-dir", dir, "--machine-id", "m-uuid")
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(dir, cnkeys.FileGossipPrivate))
	assert.FileExists(t, filepath.Join(dir, cnkeys.FileGrpcCert))
	assert.Contains(t, out, "Generated key material")
	assert.Contains(t, out, "gRPC cert hash (SHA-384):")
}

func TestGenerateCmd_RequiresKeysDir(t *testing.T) {
	_, err := runGenerateCmd(t)
	require.Error(t, err)
}

func TestGenerateCmd_SelectorSubset(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out")
	_, err := runGenerateCmd(t, "--keys-dir", dir, "--machine-id", "m", "--gossip")
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(dir, cnkeys.FileGossipPrivate))
	assert.NoFileExists(t, filepath.Join(dir, cnkeys.FileGrpcKey))
}

func TestGenerateCmd_Idempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	_, err := runGenerateCmd(t, "--keys-dir", dir, "--machine-id", "m", "--gossip")
	require.NoError(t, err)
	first, err := os.ReadFile(filepath.Join(dir, cnkeys.FileGossipPrivate))
	require.NoError(t, err)

	// Re-running does not regenerate: the existing key is kept.
	out, err := runGenerateCmd(t, "--keys-dir", dir, "--machine-id", "m", "--gossip")
	require.NoError(t, err)
	assert.Contains(t, out, "already exists")
	again, err := os.ReadFile(filepath.Join(dir, cnkeys.FileGossipPrivate))
	require.NoError(t, err)
	assert.Equal(t, string(first), string(again), "existing key must be preserved")

	// --force regenerates (overwrites).
	_, err = runGenerateCmd(t, "--keys-dir", dir, "--machine-id", "m", "--gossip", "--force")
	require.NoError(t, err)
	forced, err := os.ReadFile(filepath.Join(dir, cnkeys.FileGossipPrivate))
	require.NoError(t, err)
	assert.NotEqual(t, string(first), string(forced), "--force must regenerate")
}

func TestGenerateCmd_DistinctPerRun(t *testing.T) {
	dirA := filepath.Join(t.TempDir(), "a")
	dirB := filepath.Join(t.TempDir(), "b")
	_, err := runGenerateCmd(t, "--keys-dir", dirA, "--machine-id", "m", "--gossip")
	require.NoError(t, err)
	_, err = runGenerateCmd(t, "--keys-dir", dirB, "--machine-id", "m", "--gossip")
	require.NoError(t, err)

	a, err := os.ReadFile(filepath.Join(dirA, cnkeys.FileGossipPrivate))
	require.NoError(t, err)
	b, err := os.ReadFile(filepath.Join(dirB, cnkeys.FileGossipPrivate))
	require.NoError(t, err)
	assert.NotEqual(t, string(a), string(b))
}

// resetGrpcFlags clears the repeatable --grpc-* flags, which otherwise accumulate
// across Execute calls on the shared command tree.
func resetGrpcFlags(t *testing.T, cmd *cobra.Command) {
	t.Helper()
	for _, name := range []string{"grpc-dns", "grpc-ip"} {
		f := cmd.Flags().Lookup(name)
		require.NotNil(t, f)
		require.NoError(t, f.Value.(pflag.SliceValue).Replace(nil))
	}
	require.NoError(t, cmd.Flags().Set("grpc-validity-days", "0"))
	require.NoError(t, cmd.Flags().Set("grpc-cn", ""))
	require.NoError(t, cmd.Flags().Set("grpc-org", ""))
}

func TestGenerateCmd_GrpcFlags(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out")
	_, err := runGenerateCmd(t, "--keys-dir", dir, "--tls",
		"--grpc-dns", "node0.example.org", "--grpc-ip", "10.1.2.3", "--grpc-validity-days", "365",
		"--grpc-cn", "node0", "--grpc-org", "Example Org")
	require.NoError(t, err)

	pemBytes, err := os.ReadFile(filepath.Join(dir, cnkeys.FileGrpcCert))
	require.NoError(t, err)
	cert, err := corekeys.ParseCertificatePEM(pemBytes)
	require.NoError(t, err)
	assert.Equal(t, "node0", cert.Subject.CommonName)
	assert.Equal(t, "node0", cert.Issuer.CommonName)
	assert.Equal(t, []string{"Example Org"}, cert.Subject.Organization)
	assert.Equal(t, []string{"Example Org"}, cert.Issuer.Organization)
	assert.Contains(t, cert.DNSNames, "node0.example.org")
	assert.Contains(t, cert.DNSNames, "localhost")
	assert.Len(t, cert.IPAddresses, 3)
	assert.Less(t, cert.NotAfter.Sub(cert.NotBefore), 367*24*time.Hour)
}

func TestGenerateCmd_RejectsBadGrpcFlags(t *testing.T) {
	for _, args := range [][]string{
		{"--grpc-ip", "not-an-ip"},
		{"--grpc-dns", "bad name!"},
		{"--grpc-dns", "node2"},
		{"--grpc-cn", "   "},
		{"--grpc-org", "   "},
		{"--grpc-org", strings.Repeat("o", 65)},
		{"--grpc-cn", strings.Repeat("a", 65)},
		{"--grpc-validity-days", "-1"},
		{"--grpc-validity-days", "99999"},
	} {
		dir := filepath.Join(t.TempDir(), "out")
		_, err := runGenerateCmd(t, append([]string{"--keys-dir", dir, "--tls"}, args...)...)
		require.Error(t, err, args)
	}
}

func TestGenerateCmd_RejectsStrayArgs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out")
	_, err := runGenerateCmd(t, "--keys-dir", dir, "--tls", "--grpc-dns", "node0.example.org", "GROUP")
	require.Error(t, err)
}
