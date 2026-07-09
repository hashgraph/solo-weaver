// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package blocknode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

// TestVethResolver_Resolve_Success verifies that Resolve returns the interface
// name whose ifindex matches the value returned by readPodIflink.
func TestVethResolver_Resolve_Success(t *testing.T) {
	dir := t.TempDir()
	writeIfindex(t, dir, "lo", 1)
	writeIfindex(t, dir, "eth0", 41)
	writeIfindex(t, dir, "lxcabc123", 42)
	writeIfindex(t, dir, "lxcdef456", 43)

	r := &VethResolver{
		readPodIflink: func(_ context.Context, _ *corev1.Pod) (int, error) { return 42, nil },
		sysClassNet:   dir,
	}
	name, err := r.Resolve(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, "lxcabc123", name)
}

// TestVethResolver_Resolve_VethNotFound verifies ErrVethNotFound is returned
// when no host interface has a matching ifindex.
func TestVethResolver_Resolve_VethNotFound(t *testing.T) {
	dir := t.TempDir()
	writeIfindex(t, dir, "lo", 1)
	writeIfindex(t, dir, "eth0", 41)

	r := &VethResolver{
		readPodIflink: func(_ context.Context, _ *corev1.Pod) (int, error) { return 99, nil },
		sysClassNet:   dir,
	}
	_, err := r.Resolve(context.Background(), nil)
	require.ErrorIs(t, err, ErrVethNotFound)
}

// TestVethResolver_Resolve_ReadPodIfLinkError verifies that an error from
// readPodIflink is propagated without wrapping.
func TestVethResolver_Resolve_ReadPodIfLinkError(t *testing.T) {
	dir := t.TempDir()
	writeIfindex(t, dir, "lxcabc123", 42)

	sentinel := errors.New("exec failed")
	r := &VethResolver{
		readPodIflink: func(_ context.Context, _ *corev1.Pod) (int, error) { return 0, sentinel },
		sysClassNet:   dir,
	}
	_, err := r.Resolve(context.Background(), nil)
	require.ErrorIs(t, err, sentinel)
}

// TestVethResolver_Resolve_SkipsUnreadableIfindex verifies that interfaces
// with no ifindex file are skipped and the search continues.
func TestVethResolver_Resolve_SkipsUnreadableIfindex(t *testing.T) {
	dir := t.TempDir()
	// A directory without an ifindex file — should be skipped.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "eth0"), 0o755))
	writeIfindex(t, dir, "lxcabc123", 42)

	r := &VethResolver{
		readPodIflink: func(_ context.Context, _ *corev1.Pod) (int, error) { return 42, nil },
		sysClassNet:   dir,
	}
	name, err := r.Resolve(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, "lxcabc123", name)
}

// TestVethResolver_Resolve_BadIfindexContent verifies that an interface whose
// ifindex file contains non-numeric content is skipped (and ErrVethNotFound is
// returned when it's the only candidate).
func TestVethResolver_Resolve_BadIfindexContent(t *testing.T) {
	dir := t.TempDir()
	ifDir := filepath.Join(dir, "lxcabc123")
	require.NoError(t, os.MkdirAll(ifDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ifDir, "ifindex"), []byte("bad\n"), 0o644))

	r := &VethResolver{
		readPodIflink: func(_ context.Context, _ *corev1.Pod) (int, error) { return 42, nil },
		sysClassNet:   dir,
	}
	_, err := r.Resolve(context.Background(), nil)
	require.ErrorIs(t, err, ErrVethNotFound)
}

// writeIfindex creates <dir>/<iface>/ifindex containing the given index value.
func writeIfindex(t *testing.T, dir, iface string, index int) {
	t.Helper()
	ifDir := filepath.Join(dir, iface)
	require.NoError(t, os.MkdirAll(ifDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ifDir, "ifindex"), []byte(fmt.Sprintf("%d\n", index)), 0o644))
}
