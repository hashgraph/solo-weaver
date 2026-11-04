// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	cnkeys "github.com/hashgraph/solo-weaver/internal/consensus/keys"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSecretClient is an in-memory cnkeys.SecretClient for command tests. Build
// it as &fakeSecretClient{store: map[string]map[string][]byte{}}.
type fakeSecretClient struct {
	store       map[string]map[string][]byte
	labels      map[string]map[string]string
	annotations map[string]map[string]string
}

func (f *fakeSecretClient) ApplySecret(_ context.Context, ns, name string, data map[string][]byte, labels, annotations map[string]string) error {
	if f.labels == nil {
		f.labels = map[string]map[string]string{}
		f.annotations = map[string]map[string]string{}
	}
	f.store[ns+"/"+name] = data
	f.labels[ns+"/"+name] = labels
	f.annotations[ns+"/"+name] = annotations
	return nil
}

func (f *fakeSecretClient) GetSecretData(_ context.Context, ns, name string) (map[string][]byte, bool, error) {
	d, ok := f.store[ns+"/"+name]
	return d, ok, nil
}

func (f *fakeSecretClient) GetSecretInfo(_ context.Context, ns, name string) (*kube.SecretInfo, bool, error) {
	if _, ok := f.store[ns+"/"+name]; !ok {
		return nil, false, nil
	}
	return &kube.SecretInfo{Name: name, Labels: f.labels[ns+"/"+name], Annotations: f.annotations[ns+"/"+name]}, true, nil
}

func (f *fakeSecretClient) PatchSecretLabels(_ context.Context, ns, name string, labels map[string]string) error {
	merged := map[string]string{}
	for k, v := range f.labels[ns+"/"+name] {
		merged[k] = v
	}
	for k, v := range labels {
		merged[k] = v
	}
	f.labels[ns+"/"+name] = merged
	return nil
}

func (f *fakeSecretClient) ListSecrets(_ context.Context, ns, selector string) ([]kube.SecretInfo, error) {
	var out []kube.SecretInfo
	for k := range f.store {
		if !strings.HasPrefix(k, ns+"/") {
			continue
		}
		match := true
		for _, term := range strings.Split(selector, ",") {
			if kv := strings.SplitN(term, "=", 2); len(kv) == 2 && f.labels[k][kv[0]] != kv[1] {
				match = false
			}
		}
		if match {
			out = append(out, kube.SecretInfo{Name: strings.TrimPrefix(k, ns+"/"), Labels: f.labels[k], Annotations: f.annotations[k]})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// hasSecretWithPrefix reports whether the fake holds a secret in ns whose name
// starts with prefix.
func (f *fakeSecretClient) hasSecretWithPrefix(ns, prefix string) bool {
	for k := range f.store {
		if strings.HasPrefix(k, ns+"/"+prefix) {
			return true
		}
	}
	return false
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
		// A repeatable flag's DefValue is "[]", which Set would store as a literal
		// element, so clear it with Replace instead.
		if sv, ok := f.Value.(pflag.SliceValue); ok {
			_ = sv.Replace(nil)
		} else {
			_ = f.Value.Set(f.DefValue)
		}
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
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, cnkeys.FileAdminPublic), []byte(hex.EncodeToString(pub)+"\n"), 0o644))
	return dir
}

func TestImportCmd_HappyPath(t *testing.T) {
	dir := genFolder(t)
	fake := &fakeSecretClient{store: map[string]map[string][]byte{}}
	stubSecretClient(t, fake)

	out, err := runUnderNode(t, importCmd, "--namespace", "orbit-1", "--node-id", "0", "--keys-dir", dir)
	require.NoError(t, err)
	assert.Contains(t, out, "secret/node0-gossip-keys")
	assert.True(t, fake.hasSecretWithPrefix("orbit-1", "node0-admin-pubkey-"))
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

func TestImportCmd_SecretSuffixAndBadSuffix(t *testing.T) {
	dir := genFolder(t)
	fake := &fakeSecretClient{store: map[string]map[string][]byte{}}
	stubSecretClient(t, fake)

	out, err := runUnderNode(t, importCmd, "--namespace", "ns", "--node-id", "0", "--keys-dir", dir, "--secret-suffix", "v0.75.6")
	require.NoError(t, err)
	assert.Contains(t, out, "secret/node0-gossip-keys-v0.75.6-")

	_, err = runUnderNode(t, importCmd, "--namespace", "ns", "--node-id", "0", "--keys-dir", dir, "--secret-suffix", "Bad Suffix")
	require.Error(t, err)
}

func TestExportCmd_FingerprintNeedsOneKeyType(t *testing.T) {
	stubSecretClient(t, &fakeSecretClient{store: map[string]map[string][]byte{}})
	_, err := runUnderNode(t, exportCmd, "--namespace", "ns", "--node-id", "0", "--keys-dir", t.TempDir(), "--fingerprint", "abcd")
	require.Error(t, err)
}

func TestImportCmd_LabelFlag(t *testing.T) {
	dir := genFolder(t)
	fake := &fakeSecretClient{store: map[string]map[string][]byte{}}
	stubSecretClient(t, fake)

	out, err := runUnderNode(t, importCmd, "--namespace", "ns", "--node-id", "0", "--keys-dir", dir,
		"--label", "example.com/external-ref=ticket-42", "--label", "app.kubernetes.io/version=v0.75.6")
	require.NoError(t, err)
	assert.Contains(t, out, "secret/node0-gossip-keys-")
	for k, lbls := range fake.labels {
		assert.Equal(t, "ticket-42", lbls["example.com/external-ref"], k)
		assert.Equal(t, "v0.75.6", lbls["app.kubernetes.io/version"], k)
	}

	// Running again with the same labels changes nothing; a new one is labels-only.
	out, err = runUnderNode(t, importCmd, "--namespace", "ns", "--node-id", "0", "--keys-dir", dir,
		"--label", "example.com/external-ref=ticket-43")
	require.NoError(t, err)
	assert.Contains(t, out, "labels updated")
}

func TestImportCmd_RejectsBadLabels(t *testing.T) {
	dir := genFolder(t)
	stubSecretClient(t, &fakeSecretClient{store: map[string]map[string][]byte{}})
	for _, arg := range []string{"no-equals-sign", "solo.hiero.org/node=x", "bad key=v"} {
		_, err := runUnderNode(t, importCmd, "--namespace", "ns", "--node-id", "0", "--keys-dir", dir, "--label", arg)
		require.Error(t, err, arg)
	}
}
