// SPDX-License-Identifier: Apache-2.0

package software

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reRootCompletionDir points the loader directory at a temp dir.
func reRootCompletionDir(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "bash-completion", "completions")
	orig := bashCompletionDir
	t.Cleanup(func() { bashCompletionDir = orig })
	bashCompletionDir = dir

	return dir
}

// installFakeTool reproduces the /usr/local/bin -> sandbox symlink layout under
// a temp dir. kind selects the shapes weaverManagedTool must tell apart.
func installFakeTool(t *testing.T, cmd, kind string) {
	t.Helper()

	root := t.TempDir()

	restorePaths := models.SetPaths(filepath.Join(root, "weaver"))
	t.Cleanup(restorePaths)

	sandboxBinary := filepath.Join(models.Paths().SandboxBinDir, cmd)
	require.NoError(t, os.MkdirAll(models.Paths().SandboxBinDir, 0o755))

	sysBin := filepath.Join(root, "usr", "local", "bin")
	require.NoError(t, os.MkdirAll(sysBin, 0o755))

	origSysBin := systemBinDir
	t.Cleanup(func() { systemBinDir = origSysBin })
	systemBinDir = sysBin

	switch kind {
	case "weaver":
		require.NoError(t, os.WriteFile(sandboxBinary, []byte("#!/bin/sh\n"), 0o755))
		require.NoError(t, os.Symlink(sandboxBinary, filepath.Join(sysBin, cmd)))
	case "dangling":
		// Symlink into the sandbox, but teardown removed the target.
		require.NoError(t, os.Symlink(sandboxBinary, filepath.Join(sysBin, cmd)))
	case "foreign":
		// An operator's own binary sitting at the same path.
		require.NoError(t, os.WriteFile(filepath.Join(sysBin, cmd), []byte("#!/bin/sh\n"), 0o755))
	case "absent":
	default:
		t.Fatalf("unknown kind %q", kind)
	}
}

func Test_BashCompletionLoader_Content(t *testing.T) {
	assert.Equal(t, "eval -- \"$(\"$1\" completion bash 2>/dev/null)\"\n", bashCompletionLoader)

	// The dynamic loader only finds the file at this path and without a leading
	// underscore on the filename.
	assert.Equal(t, "/usr/local/share/bash-completion/completions", bashCompletionDir)
	assert.Equal(t, "/usr/local/share/bash-completion/completions/kubectl", bashCompletionPath("kubectl"))
}

func Test_WriteBashCompletionLoader(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockFsx := fsx.NewMockManager(ctrl)
	mockFsx.EXPECT().ReadFile(bashCompletionPath("kubectl"), int64(-1)).
		Return(nil, errors.New("no such file"))
	mockFsx.EXPECT().CreateDirectory(bashCompletionDir, true).Return(nil)
	// 0644: a group- or world-writable loader is code execution in root's shell.
	mockFsx.EXPECT().AtomicWriteFile(
		bashCompletionPath("kubectl"),
		[]byte(bashCompletionLoader),
		os.FileMode(models.DefaultFilePerm),
	).Return(nil)

	require.NoError(t, writeBashCompletionLoader(mockFsx, "kubectl"))
}

func Test_WriteBashCompletionLoader_Errors(t *testing.T) {
	tests := []struct {
		name    string
		expects func(m *fsx.MockManager)
	}{
		{
			name: "directory creation failure is reported",
			expects: func(m *fsx.MockManager) {
				m.EXPECT().ReadFile(gomock.Any(), gomock.Any()).Return(nil, errors.New("absent"))
				m.EXPECT().CreateDirectory(bashCompletionDir, true).
					Return(errors.New("read-only file system"))
			},
		},
		{
			name: "write failure is reported",
			expects: func(m *fsx.MockManager) {
				m.EXPECT().ReadFile(gomock.Any(), gomock.Any()).Return(nil, errors.New("absent"))
				m.EXPECT().CreateDirectory(bashCompletionDir, true).Return(nil)
				m.EXPECT().AtomicWriteFile(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(errors.New("disk full"))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockFsx := fsx.NewMockManager(ctrl)
			tc.expects(mockFsx)

			require.Error(t, writeBashCompletionLoader(mockFsx, "helm"))
		})
	}
}

// The write path applies the same ownership rule as the remove path: a file
// weaver did not write is left alone rather than clobbered on every install.
func Test_WriteBashCompletionLoader_LeavesForeignFile(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockFsx := fsx.NewMockManager(ctrl)
	mockFsx.EXPECT().ReadFile(bashCompletionPath("kubectl"), int64(-1)).
		Return([]byte("# hand-written by the operator\n"), nil)
	// No CreateDirectory/AtomicWriteFile: the file is not ours to replace.

	require.NoError(t, writeBashCompletionLoader(mockFsx, "kubectl"))
}

func Test_RemoveBashCompletionLoader(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockFsx := fsx.NewMockManager(ctrl)
	mockFsx.EXPECT().ReadFile(bashCompletionPath("helm"), int64(-1)).
		Return([]byte(bashCompletionLoader), nil)
	mockFsx.EXPECT().RemoveAll(bashCompletionPath("helm")).Return(nil)

	require.NoError(t, removeBashCompletionLoader(mockFsx, "helm"))
}

// Teardown must not delete a file an operator put at the loader path, matching
// the ownership rule weaverManagedTool applies on the write side.
func Test_RemoveBashCompletionLoader_LeavesForeignFile(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockFsx := fsx.NewMockManager(ctrl)
	mockFsx.EXPECT().ReadFile(bashCompletionPath("kubectl"), int64(-1)).
		Return([]byte("# hand-written by the operator\n"), nil)
	// No RemoveAll expectation: the file is not ours.

	require.NoError(t, removeBashCompletionLoader(mockFsx, "kubectl"))
}

// A loader that is already gone is not an error.
func Test_RemoveBashCompletionLoader_MissingFileIsNotAnError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockFsx := fsx.NewMockManager(ctrl)
	mockFsx.EXPECT().ReadFile(bashCompletionPath("helm"), int64(-1)).
		Return(nil, errors.New("no such file"))

	require.NoError(t, removeBashCompletionLoader(mockFsx, "helm"))
}

// An empty name resolves to the loader directory, which RemoveAll would take.
func Test_RemoveBashCompletionLoader_RejectsEmptyName(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// No RemoveAll expectation: the guard fires before any filesystem call.
	mockFsx := fsx.NewMockManager(ctrl)

	require.Error(t, removeBashCompletionLoader(mockFsx, ""))
}

func Test_WeaverManagedTool(t *testing.T) {
	tests := []struct {
		kind string
		want bool
		why  string
	}{
		{"weaver", true, "live symlink into the sandbox"},
		{"dangling", false, "symlink left by teardown, target gone"},
		{"foreign", false, "an operator's own binary at the same path"},
		{"absent", false, "nothing installed"},
	}

	for _, tc := range tests {
		t.Run(tc.kind, func(t *testing.T) {
			installFakeTool(t, KubectlBinaryName, tc.kind)
			assert.Equal(t, tc.want, weaverManagedTool(KubectlBinaryName), tc.why)
		})
	}
}

func Test_ShellCompletionNeedsWrite(t *testing.T) {
	t.Run("managed tool without a loader needs the write", func(t *testing.T) {
		reRootCompletionDir(t)
		installFakeTool(t, KubectlBinaryName, "weaver")

		assert.True(t, ShellCompletionNeedsWrite())
	})

	t.Run("managed tool with a loader needs nothing", func(t *testing.T) {
		dir := reRootCompletionDir(t)
		installFakeTool(t, KubectlBinaryName, "weaver")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, KubectlBinaryName),
			[]byte(bashCompletionLoader), 0o644))

		assert.False(t, ShellCompletionNeedsWrite())
	})

	t.Run("torn-down host needs nothing even with no loaders", func(t *testing.T) {
		reRootCompletionDir(t)
		installFakeTool(t, KubectlBinaryName, "dangling")

		assert.False(t, ShellCompletionNeedsWrite())
	})
}

func Test_ShellCompletionWritable(t *testing.T) {
	t.Run("writable ancestor", func(t *testing.T) {
		reRootCompletionDir(t)

		assert.True(t, ShellCompletionWritable())
	})

	t.Run("read-only ancestor", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root bypasses the write permission bit")
		}

		locked := filepath.Join(t.TempDir(), "locked")
		require.NoError(t, os.MkdirAll(locked, 0o555))
		t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

		orig := bashCompletionDir
		t.Cleanup(func() { bashCompletionDir = orig })
		bashCompletionDir = filepath.Join(locked, "bash-completion", "completions")

		assert.False(t, ShellCompletionWritable())
	})
}

// Exercises the exported seams the install step, migration and teardown call,
// against a real filesystem.
func Test_ReconfigureAndRemoveShellCompletion_RoundTrip(t *testing.T) {
	dir := reRootCompletionDir(t)
	installFakeTool(t, KubectlBinaryName, "weaver")

	require.True(t, ShellCompletionNeedsWrite(), "precondition: loader is missing")

	require.NoError(t, ReconfigureShellCompletion())

	content, err := os.ReadFile(filepath.Join(dir, KubectlBinaryName))
	require.NoError(t, err)
	assert.Equal(t, bashCompletionLoader, string(content))

	info, err := os.Stat(filepath.Join(dir, KubectlBinaryName))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(models.DefaultFilePerm), info.Mode().Perm(),
		"loader must not be group- or world-writable")

	assert.False(t, ShellCompletionNeedsWrite(), "the write must satisfy the probe")

	// helm is not installed in this fixture, so it must not get a loader.
	_, err = os.Stat(filepath.Join(dir, HelmBinaryName))
	assert.True(t, os.IsNotExist(err), "no loader for a tool that is not installed")

	require.NoError(t, RemoveShellCompletion())

	_, err = os.Stat(filepath.Join(dir, KubectlBinaryName))
	assert.True(t, os.IsNotExist(err), "teardown must remove the loader")
}
