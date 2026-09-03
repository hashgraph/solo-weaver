// SPDX-License-Identifier: Apache-2.0

package software

import (
	"os"
	"path"
	"path/filepath"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"golang.org/x/sys/unix"
)

// bashCompletionLoader is the entire contents of every loader file. The dynamic
// loader sources it with the command name as $1, so one line serves any tool
// that can emit its own completion script.
const bashCompletionLoader = `eval -- "$("$1" completion bash 2>/dev/null)"` + "\n"

// The loader dir is under /usr/local/share, which is on the dynamic loader's
// default search path and, unlike /usr/share, is not owned by a distro package.
// Both are vars only so tests can re-root them.
var (
	bashCompletionDir = "/usr/local/share/bash-completion/completions"
	systemBinDir      = models.SystemBinDir
)

// completionCommands are the installed tools that emit `<tool> completion bash`.
var completionCommands = []string{KubectlBinaryName, HelmBinaryName}

func bashCompletionPath(cmd string) string {
	return filepath.Join(bashCompletionDir, cmd)
}

// weaverManagedTool reports whether cmd is a live weaver symlink: the system
// binary points into the sandbox and the target still exists. A link left
// dangling by a teardown, and an operator's own binary at the same path, both
// report false.
func weaverManagedTool(cmd string) bool {
	systemBinary := filepath.Join(systemBinDir, cmd)

	target, err := os.Readlink(systemBinary)
	if err != nil {
		return false
	}

	if target != path.Join(models.Paths().SandboxBinDir, cmd) {
		return false
	}

	// Readlink succeeds on a dangling link; Stat follows it and does not.
	_, err = os.Stat(systemBinary)

	return err == nil
}

func loaderPresent(cmd string) bool {
	_, err := os.Stat(bashCompletionPath(cmd))

	return err == nil
}

// ShellCompletionNeedsWrite reports whether an installed tool is missing its
// loader. It stats directly rather than building an fsx manager, which would
// resolve the weaver principal on a path that runs before every root command.
func ShellCompletionNeedsWrite() bool {
	for _, cmd := range completionCommands {
		if weaverManagedTool(cmd) && !loaderPresent(cmd) {
			return true
		}
	}

	return false
}

// ShellCompletionWritable reports whether the loader directory can be written,
// testing the nearest existing ancestor. Callers gate on it so a read-only
// /usr/local/share is skipped instead of retried on every invocation.
func ShellCompletionWritable() bool {
	dir := bashCompletionDir

	for {
		if _, err := os.Stat(dir); err == nil {
			return unix.Access(dir, unix.W_OK) == nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}

		dir = parent
	}
}

// writeBashCompletionLoader writes the loader for one command, leaving any file
// weaver did not write in place. The file must stay root-owned and not group- or
// world-writable: the completing shell executes the binary it names.
func writeBashCompletionLoader(fm fsx.Manager, cmd string) error {
	loaderPath := bashCompletionPath(cmd)

	if content, err := fm.ReadFile(loaderPath, -1); err == nil && string(content) != bashCompletionLoader {
		logx.As().Debug().Str("path", loaderPath).
			Msg("bash completion loader was not written by weaver; leaving it in place")

		return nil
	}

	if err := fm.CreateDirectory(bashCompletionDir, true); err != nil {
		return NewConfigurationError(err, cmd)
	}

	if err := fm.AtomicWriteFile(loaderPath, []byte(bashCompletionLoader),
		models.DefaultFilePerm); err != nil {
		return NewConfigurationError(err, cmd)
	}

	return nil
}

// removeBashCompletionLoader deletes the loader for one command, leaving any
// file weaver did not write in place, as the write path does. An empty name
// would resolve to the loader directory itself, so it is rejected.
func removeBashCompletionLoader(fm fsx.Manager, cmd string) error {
	if cmd == "" {
		return NewConfigurationError(nil, cmd)
	}

	loaderPath := bashCompletionPath(cmd)

	content, err := fm.ReadFile(loaderPath, -1)
	if err != nil {
		// Already gone, or unreadable: nothing to remove.
		return nil
	}

	if string(content) != bashCompletionLoader {
		logx.As().Debug().Str("path", loaderPath).
			Msg("bash completion loader was not written by weaver; leaving it in place")

		return nil
	}

	if err := fm.RemoveAll(loaderPath); err != nil {
		return NewConfigurationError(err, cmd)
	}

	return nil
}

// ReconfigureShellCompletion writes the loader for every installed tool. It is
// idempotent.
func ReconfigureShellCompletion() error {
	fm, err := fsx.NewManager()
	if err != nil {
		return NewFileSystemError(err)
	}

	for _, cmd := range completionCommands {
		if !weaverManagedTool(cmd) {
			continue
		}

		if err := writeBashCompletionLoader(fm, cmd); err != nil {
			return err
		}
	}

	return nil
}

// RemoveShellCompletion deletes every loader, whether or not the tool is still
// installed; by teardown time the binaries are usually gone.
func RemoveShellCompletion() error {
	fm, err := fsx.NewManager()
	if err != nil {
		return NewFileSystemError(err)
	}

	for _, cmd := range completionCommands {
		if err := removeBashCompletionLoader(fm, cmd); err != nil {
			return err
		}
	}

	return nil
}
