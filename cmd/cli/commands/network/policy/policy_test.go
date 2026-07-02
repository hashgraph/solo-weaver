// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	pol "github.com/hashgraph/solo-weaver/internal/network/policy"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

// fakeRunner satisfies pol.Runner without touching the kernel.
type fakeRunner struct{ applied string }

func (f *fakeRunner) Apply(_ context.Context, doc string) error              { f.applied = doc; return nil }
func (f *fakeRunner) AddElements(context.Context, string, []string) error    { return nil }
func (f *fakeRunner) ListElements(context.Context, string) ([]string, error) { return nil, nil }
func (f *fakeRunner) List(context.Context) (string, error)                   { return f.applied, nil }
func (f *fakeRunner) Delete(context.Context) error                           { return nil }
func (f *fakeRunner) Exists(context.Context) (bool, error)                   { return f.applied != "", nil }

func TestPolicyCmd_Structure(t *testing.T) {
	cmd := GetCmd()
	require.Equal(t, "policy", cmd.Use)

	var hasCreate bool
	for _, sub := range cmd.Commands() {
		if sub.Use == "create" {
			hasCreate = true
		}
	}
	require.True(t, hasCreate, "create verb not registered under policy")
}

func TestCreateCmd_Flags(t *testing.T) {
	for _, name := range []string{"name", "stamp", "deny", "reply-stamp", "from-entity", "ports", "cidrs", "cidrs-file", "pod-cidr"} {
		require.NotNil(t, createCmd.Flags().Lookup(name), "create is missing --%s", name)
	}
	// No --direction flag: direction is derived from --stamp's class (§5).
	require.Nil(t, createCmd.Flags().Lookup("direction"), "create should not have a --direction flag")
}

// testEnv holds the state a sequence of create invocations shares: the same
// fakeRunner and on-disk paths, so a second runCreate call can see what a
// first one did (e.g. to exercise create-if-missing / --force behavior).
// detect, if set, overrides the default always-succeeds pod-CIDR stub.
type testEnv struct {
	dir     string
	nftPath string
	runner  *fakeRunner
	detect  func(context.Context) (string, error)
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	return &testEnv{dir: dir, nftPath: filepath.Join(dir, "network-weaver.nft"), runner: &fakeRunner{}}
}

// runCreate executes the real create command against this env's manager and
// stubbed pod-CIDR detection, returning the persisted network-weaver.nft.
func (e *testEnv) runCreate(t *testing.T, args ...string) (string, error) {
	t.Helper()
	origMgr, origDetect := newManager, detectPodCIDR
	newManager = func() *pol.Manager {
		return pol.NewManagerWithConfig(pol.Config{
			Runner:        e.runner,
			WeaverNftPath: e.nftPath,
			RegistryDir:   filepath.Join(e.dir, "policies"),
			LockPath:      filepath.Join(e.dir, ".applying"),
			EnsureService: func(context.Context) error { return nil },
		})
	}
	if e.detect != nil {
		detectPodCIDR = e.detect
	} else {
		detectPodCIDR = func(context.Context) (string, error) { return "10.4.0.0/24", nil }
	}
	defer func() { newManager, detectPodCIDR = origMgr, origDetect }()

	// Reset shared flag vars between invocations so state doesn't leak.
	resetFlags()

	root := &cobra.Command{Use: "test"}
	// --force is a persistent flag on the real root (cmd/cli/commands/root.go);
	// this synthetic root needs its own copy for common.FlagForce().Value to
	// find it.
	root.PersistentFlags().Bool("force", false, "force")
	root.AddCommand(GetCmd())
	root.SetArgs(append([]string{"policy", "create"}, args...))
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	if err := root.Execute(); err != nil {
		return "", err
	}
	data, err := os.ReadFile(e.nftPath)
	return string(data), err
}

// runCreate is the single-invocation convenience wrapper most tests use: a
// fresh, isolated env per call.
func runCreate(t *testing.T, args ...string) (string, error) {
	return newTestEnv(t).runCreate(t, args...)
}

func resetFlags() {
	flagName, flagStamp = "", ""
	flagDeny = false
	flagReplyStamp, flagFromEntity = "", ""
	flagPorts, flagCIDRs = nil, nil
	flagCIDRsFile, flagPodCIDR = "", ""
	// createCmd is a package singleton, so cobra's per-flag Changed state leaks
	// across Execute() calls; clear it too or a prior test's --cidrs would trip
	// the mutual-exclusion guard here.
	createCmd.Flags().VisitAll(func(f *pflag.Flag) { f.Changed = false })
}

func TestCreateCmd_StampIngress(t *testing.T) {
	doc, err := runCreate(t, "--name", "bn-publisher", "--stamp", "publisher", "--ports", "40840", "--cidrs", "10.1.0.1/32")
	require.NoError(t, err)
	require.Contains(t, doc, "ip daddr 10.4.0.0/24 ip saddr @bn-publisher tcp dport @bn-publisher_ports meta priority set 0x10010 accept")
	// Membership is never persisted (§8.3.1).
	require.NotContains(t, doc, "10.1.0.1/32")
}

func TestCreateCmd_Deny(t *testing.T) {
	doc, err := runCreate(t, "--name", "bn-restricted", "--deny", "--cidrs", "10.99.0.0/16")
	require.NoError(t, err)
	require.Contains(t, doc, "ip saddr @bn-restricted drop")
	require.Contains(t, doc, "ip daddr @bn-restricted drop")
}

func TestCreateCmd_DenySkipsPodCIDRDetection(t *testing.T) {
	env := newTestEnv(t)
	called := false
	env.detect = func(context.Context) (string, error) {
		called = true
		return "", errors.New("no cluster reachable")
	}

	doc, err := env.runCreate(t, "--name", "bn-restricted", "--deny", "--cidrs", "10.99.0.0/16")
	require.NoError(t, err, "--deny must not need pod-CIDR detection at all")
	require.False(t, called, "detectPodCIDR must not be invoked for --deny")
	require.Contains(t, doc, "ip saddr @bn-restricted drop")
}

func TestCreateCmd_StampAndDenyMutuallyExclusive(t *testing.T) {
	_, err := runCreate(t, "--name", "x", "--stamp", "publisher", "--deny")
	require.ErrorContains(t, err, "mutually exclusive")
}

func TestCreateCmd_FromEntityInvalidValue(t *testing.T) {
	_, err := runCreate(t, "--name", "x", "--stamp", "public", "--from-entity", "internet")
	require.ErrorContains(t, err, "only")
}

func TestCreateCmd_CIDRsAndFileMutuallyExclusive(t *testing.T) {
	f := filepath.Join(t.TempDir(), "cidrs.txt")
	require.NoError(t, os.WriteFile(f, []byte("10.0.0.0/8\n"), 0o644))
	_, err := runCreate(t, "--name", "x", "--deny", "--cidrs", "10.1.0.0/16", "--cidrs-file", f)
	require.ErrorContains(t, err, "mutually exclusive")
}

func TestCreateCmd_ExistingWithoutForceIsNoOp(t *testing.T) {
	env := newTestEnv(t)
	doc1, err := env.runCreate(t, "--name", "bn-publisher", "--stamp", "publisher", "--ports", "40840")
	require.NoError(t, err)
	require.Contains(t, doc1, "40840")

	doc2, err := env.runCreate(t, "--name", "bn-publisher", "--stamp", "publisher", "--ports", "50000")
	require.NoError(t, err)
	require.Equal(t, doc1, doc2, "an existing policy without --force must not change")
}

func TestCreateCmd_ForceReplacesExisting(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.runCreate(t, "--name", "bn-publisher", "--stamp", "publisher", "--ports", "40840")
	require.NoError(t, err)

	doc, err := env.runCreate(t, "--name", "bn-publisher", "--stamp", "publisher", "--ports", "50000", "--force")
	require.NoError(t, err)
	require.Contains(t, doc, "50000")
	require.NotContains(t, doc, "40840", "--force replaces the config, it doesn't merge with the old one")
}

func TestCreateCmd_CIDRsFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "cidrs.txt")
	require.NoError(t, os.WriteFile(f, []byte("# quarantine\n10.99.0.0/16, 10.98.0.0/16\n"), 0o644))
	doc, err := runCreate(t, "--name", "bn-restricted", "--deny", "--cidrs-file", f)
	require.NoError(t, err)
	// Set schema present; membership not persisted.
	require.Contains(t, doc, "set bn-restricted { type ipv4_addr; flags interval; }")
	require.NotContains(t, doc, "10.99.0.0/16")
}
