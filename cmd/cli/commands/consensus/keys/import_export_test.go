// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	cnkeys "github.com/hashgraph/solo-weaver/internal/consensus/keys"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSecretClient is an in-memory cnkeys.SecretClient for command tests.
type fakeSecretClient struct {
	store map[string]map[string][]byte
}

func (f *fakeSecretClient) ApplySecret(_ context.Context, ns, name string, data map[string][]byte, _ map[string]string) error {
	f.store[ns+"/"+name] = data
	return nil
}

func (f *fakeSecretClient) GetSecretData(_ context.Context, ns, name string) (map[string][]byte, bool, error) {
	d, ok := f.store[ns+"/"+name]
	return d, ok, nil
}

// stubSecretClient swaps newSecretClient for the duration of a test.
func stubSecretClient(t *testing.T, fake *fakeSecretClient) {
	t.Helper()
	orig := newSecretClient
	newSecretClient = func(context.Context) (cnkeys.SecretClient, error) { return fake, nil }
	t.Cleanup(func() { newSecretClient = orig })
}

// runUnderNode wires cmd under a parent that provides the inherited --namespace
// and --node-id persistent flags (defined on `consensus node` in production),
// resets flag state, and executes with args.
func runUnderNode(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	parent := &cobra.Command{Use: "node"}
	parent.PersistentFlags().String("namespace", "default", "")
	parent.PersistentFlags().Int64("node-id", 0, "")
	parent.AddCommand(cmd)

	resetFlags(parent)
	resetFlags(cmd)

	buf := &bytes.Buffer{}
	parent.SetOut(buf)
	parent.SetErr(buf)
	parent.SetArgs(append([]string{cmd.Name()}, args...))
	err := parent.Execute()
	return buf.String(), err
}

func resetFlags(c *cobra.Command) {
	reset := func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	}
	c.Flags().VisitAll(reset)
	c.PersistentFlags().VisitAll(reset)
}

func genFolder(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "keys")
	set, err := cnkeys.Generate(cnkeys.GenerateOptions{MachineID: "m"})
	require.NoError(t, err)
	require.NoError(t, set.WriteToDir(dir))
	return dir
}

func TestImportCmd_HappyPath(t *testing.T) {
	dir := genFolder(t)
	fake := &fakeSecretClient{store: map[string]map[string][]byte{}}
	stubSecretClient(t, fake)

	out, err := runUnderNode(t, importCmd, "--namespace", "orbit-1", "--node-id", "0", "--keys-dir", dir)
	require.NoError(t, err)
	assert.Contains(t, out, "secret/node0-gossip-keys")
	assert.Contains(t, fake.store, "orbit-1/node0-admin-pubkey")
}

func TestImportCmd_RequiresNodeId(t *testing.T) {
	dir := genFolder(t)
	stubSecretClient(t, &fakeSecretClient{store: map[string]map[string][]byte{}})

	_, err := runUnderNode(t, importCmd, "--namespace", "ns", "--keys-dir", dir)
	require.Error(t, err)
}

func TestImportCmd_RequiresFrom(t *testing.T) {
	stubSecretClient(t, &fakeSecretClient{store: map[string]map[string][]byte{}})
	_, err := runUnderNode(t, importCmd, "--namespace", "ns", "--node-id", "0")
	require.Error(t, err)
}

func TestImportCmd_RejectsVaultSource(t *testing.T) {
	stubSecretClient(t, &fakeSecretClient{store: map[string]map[string][]byte{}})
	_, err := runUnderNode(t, importCmd, "--namespace", "ns", "--node-id", "0", "--keys-dir", ".", "--source", "vault")
	require.Error(t, err)
}

func TestExportCmd_HappyPath(t *testing.T) {
	dir := genFolder(t)
	fake := &fakeSecretClient{store: map[string]map[string][]byte{}}
	stubSecretClient(t, fake)

	// Seed the cluster by importing first.
	_, err := runUnderNode(t, importCmd, "--namespace", "ns", "--node-id", "0", "--keys-dir", dir)
	require.NoError(t, err)

	out := filepath.Join(t.TempDir(), "backup")
	got, err := runUnderNode(t, exportCmd, "--namespace", "ns", "--node-id", "0", "--keys-dir", out)
	require.NoError(t, err)
	assert.Contains(t, got, "secret/node0-gossip-keys")
	assert.FileExists(t, filepath.Join(out, cnkeys.FileGossipPrivate))
}

func TestExportCmd_RequiresOut(t *testing.T) {
	stubSecretClient(t, &fakeSecretClient{store: map[string]map[string][]byte{}})
	_, err := runUnderNode(t, exportCmd, "--namespace", "ns", "--node-id", "0")
	require.Error(t, err)
}
