// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	cnkeys "github.com/hashgraph/solo-weaver/internal/consensus/keys"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInspectCmd_FromFolder(t *testing.T) {
	dir := genFolder(t)
	out, err := runUnderNode(t, inspectCmd, "--keys-dir", dir)
	require.NoError(t, err)
	assert.Contains(t, out, "[s-public.pem | gossip certificate]")
	assert.Contains(t, out, "[hedera.crt | gRPC TLS certificate]")
	assert.Contains(t, out, "[admin.pub | admin public key]")
	assert.Contains(t, out, "SHA-384:")
	assert.NotContains(t, out, "PRIVATE KEY")
	assert.Contains(t, out, "openssl x509 -in <file> -text -noout")
	assert.Contains(t, out, "consensus keys verify")
}

func TestInspectCmd_File(t *testing.T) {
	dir := genFolder(t)
	out, err := runUnderNode(t, inspectCmd, "--file", filepath.Join(dir, "hedera.crt"))
	require.NoError(t, err)
	assert.Contains(t, out, "[hedera.crt | gRPC TLS certificate]")
	assert.Contains(t, out, "Subject:")
}

func TestInspectCmd_FileHexWithoutPubNameIsNotPrinted(t *testing.T) {
	secret := strings.Repeat("cd", 32)
	path := filepath.Join(t.TempDir(), "admin.key")
	require.NoError(t, os.WriteFile(path, []byte(secret+"\n"), 0o600))

	out, err := runUnderNode(t, inspectCmd, "--file", path)
	require.Error(t, err)
	assert.Contains(t, out, "*.pub")
	assert.NotContains(t, out, secret)
	assert.NotContains(t, out, "| admin public key]")
	assert.NotContains(t, err.Error(), secret)
}

func TestInspectCmd_RequiresSource(t *testing.T) {
	_, err := runUnderNode(t, inspectCmd)
	require.Error(t, err)
}

func TestInspectCmd_NothingFoundFails(t *testing.T) {
	_, err := runUnderNode(t, inspectCmd, "--keys-dir", t.TempDir())
	require.Error(t, err)
}

func TestInspectCmd_PartialFolderSucceeds(t *testing.T) {
	dir := genFolder(t)
	require.NoError(t, os.Remove(filepath.Join(dir, cnkeys.FileGossipCert)))
	require.NoError(t, os.Remove(filepath.Join(dir, cnkeys.FileAdminPublic)))

	out, err := runUnderNode(t, inspectCmd, "--keys-dir", dir)
	require.NoError(t, err)
	assert.Contains(t, out, "[hedera.crt | gRPC TLS certificate]")
	assert.NotContains(t, out, "ERROR")
	assert.Contains(t, out, "Not found (skipped): s-public.pem, admin.pub")
}
