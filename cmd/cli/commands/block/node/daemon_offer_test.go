// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package node

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// newDaemonVersionCmd returns a command carrying just the --daemon-version flag,
// optionally marked as explicitly set by the operator.
func newDaemonVersionCmd(t *testing.T, explicit string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "install"}
	// cobra.Command.Context() returns nil until Execute sets it; the resolver reads
	// it, so stand in for what a real invocation would have done.
	cmd.SetContext(context.Background())
	var v string
	common.FlagDaemonVersion().SetVarP(cmd, &v, false)
	if explicit != "" {
		// SetVarP registers on PersistentFlags; cmd.Flags() only reflects it after
		// cobra's pre-Execute merge, which a direct unit-test call never performs.
		require.NoError(t, cmd.PersistentFlags().Set(common.FlagDaemonVersion().Name, explicit))
	}
	return cmd
}

func TestIsDownloadableDaemonVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		want    bool
		why     string
	}{
		{"released version", "1.4.2", true, "a stamped release resolves to a published artifact"},
		{"released version with prerelease", "1.4.2-rc1", true, "prerelease tags are published too"},
		{"taskfile default", "0.0.0", false, "the Taskfile's VERSION default has no release"},
		{"unstamped build", "dev", false, "the version package's placeholder has no release"},
		{"empty", "", false, "an empty version cannot resolve"},
		{"not a version", "not-a-version", false, "an unparseable version cannot resolve"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmd := newDaemonVersionCmd(t, "")
			require.Equal(t, tc.want, isDownloadableDaemonVersion(cmd, tc.version), tc.why)
		})
	}
}

// An operator who names a version has made a claim about what exists; let the
// download attempt produce the error rather than pre-judging it here.
func TestIsDownloadableDaemonVersion_ExplicitFlagIsAlwaysTrusted(t *testing.T) {
	t.Parallel()

	cmd := newDaemonVersionCmd(t, "0.0.0")
	require.True(t, isDownloadableDaemonVersion(cmd, "0.0.0"),
		"an explicitly-passed --daemon-version must not be second-guessed")
}

// models.SetPaths is process-wide, so these cases cannot run in parallel.
func TestInstalledDaemonBinaryPath(t *testing.T) {
	home := t.TempDir()
	restore := models.SetPaths(home)
	defer restore()

	binDir := models.Paths().BinDir
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	installed := filepath.Join(binDir, software.DaemonBinaryName)

	require.Empty(t, installedDaemonBinaryPath(), "nothing is installed yet")

	// A non-executable file is a leftover or partial write, not an installed daemon.
	require.NoError(t, os.WriteFile(installed, []byte("x"), 0o644))
	require.Empty(t, installedDaemonBinaryPath(), "a non-executable file is not an installed daemon")

	require.NoError(t, os.Chmod(installed, 0o755))
	require.Equal(t, installed, installedDaemonBinaryPath(), "an executable binary in BinDir is a usable source")
}

// The quiet resolver is what stands between the operator and the prompt, so its
// precedence order is asserted end to end. Not parallel: it mutates the
// process-wide paths, the daemon flag vars, and the environment.
func TestResolveDaemonBinarySource(t *testing.T) {
	home := t.TempDir()
	restore := models.SetPaths(home)
	defer restore()

	binDir := models.Paths().BinDir
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	installed := filepath.Join(binDir, software.DaemonBinaryName)

	// flagDaemonBin/flagDaemonVersion are package-level flag targets shared across
	// this package's commands; restore them so later tests see the real defaults.
	origBin, origVersion := flagDaemonBin, flagDaemonVersion
	defer func() { flagDaemonBin, flagDaemonVersion = origBin, origVersion }()
	flagDaemonVersion = "0.0.0"

	t.Run("nothing available is an error, not a prompt", func(t *testing.T) {
		flagDaemonBin = ""
		cmd := newDaemonVersionCmd(t, "")

		source, err := resolveDaemonBinarySource(cmd)
		require.Error(t, err, "a placeholder version with no installed binary cannot resolve")
		require.Empty(t, source.BinPath)
	})

	t.Run("an installed binary resolves it", func(t *testing.T) {
		flagDaemonBin = ""
		require.NoError(t, os.WriteFile(installed, []byte("#!/bin/sh\n"), 0o755))
		defer func() { require.NoError(t, os.Remove(installed)) }()

		source, err := resolveDaemonBinarySource(newDaemonVersionCmd(t, ""))
		require.NoError(t, err)
		require.Equal(t, installed, source.BinPath)
	})

	t.Run("the environment beats an installed binary", func(t *testing.T) {
		flagDaemonBin = ""
		require.NoError(t, os.WriteFile(installed, []byte("#!/bin/sh\n"), 0o755))
		defer func() { require.NoError(t, os.Remove(installed)) }()
		t.Setenv(envDaemonBin, "/from/env/solo-provisioner-daemon")

		source, err := resolveDaemonBinarySource(newDaemonVersionCmd(t, ""))
		require.NoError(t, err)
		require.Equal(t, "/from/env/solo-provisioner-daemon", source.BinPath)
	})

	t.Run("the flag beats everything", func(t *testing.T) {
		flagDaemonBin = "/from/flag/solo-provisioner-daemon"
		require.NoError(t, os.WriteFile(installed, []byte("#!/bin/sh\n"), 0o755))
		defer func() { require.NoError(t, os.Remove(installed)) }()
		t.Setenv(envDaemonBin, "/from/env/solo-provisioner-daemon")

		source, err := resolveDaemonBinarySource(newDaemonVersionCmd(t, ""))
		require.NoError(t, err)
		require.Equal(t, "/from/flag/solo-provisioner-daemon", source.BinPath)
	})

	// A released build needs no local binary — the catalog download can supply it.
	t.Run("a downloadable version resolves without a local binary", func(t *testing.T) {
		flagDaemonBin = ""
		flagDaemonVersion = "1.4.2"
		defer func() { flagDaemonVersion = "0.0.0" }()

		source, err := resolveDaemonBinarySource(newDaemonVersionCmd(t, ""))
		require.NoError(t, err)
		require.Empty(t, source.BinPath, "an empty BinPath selects the auto-download path")
		require.Equal(t, "1.4.2", source.Version)
	})
}
