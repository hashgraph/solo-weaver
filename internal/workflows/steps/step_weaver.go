// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/version"
	"github.com/joomcode/errorx"
)

const weaverBinaryName = "weaver"

var errWeaverInstallationRequired = errorx.IllegalState.New("weaver installation or re-installation required")

// CheckWeaverInstallation checks if weaver is installed at the given binDir.
func CheckWeaverInstallation(binDir string) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("check-weaver-installation").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			exePath, err := os.Executable()
			if err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to locate current executable")))
			}

			expectedPath := filepath.Join(binDir, weaverBinaryName)
			if exePath != expectedPath {
				var args string
				if len(os.Args) > 1 {
					args = " " + strings.TrimSpace(strings.Join(os.Args[1:], " "))
				}

				resolution := fmt.Sprintf("install or re-install weaver binary; "+
					"run `sudo %s install` to install and then run `weaver%s`.", exePath, args)

				errWithResolution := errWeaverInstallationRequired.WithProperty(doctor.ErrPropertyResolution, resolution)

				logx.As().Error().
					Err(errWithResolution).
					Str("exePath", exePath).
					Str("expectedPath", expectedPath).
					Msg("Weaver installation check failed: current executable is not in the expected bin directory")

				return automa.StepFailureReport(stp.Id(), automa.WithError(errWithResolution))
			}

			meta := map[string]string{
				"weaver_path":       exePath,
				"installed_version": version.Number(),
				"installed_commit":  version.Commit(),
			}

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		})
}

// InstallWeaver installs the currently running executable as the `weaver` binary
// into the provided `binDir` and attempts to create a convenience symlink in
// `/usr/local/bin`.
//
// Behavior
//   - The step locates the currently running executable (source).
//   - It ensures `binDir` exists and then copies the source executable into a
//     temporary file created inside `binDir` (pattern `weaver.tmp.*`).
//   - After the copy completes the temp file is closed, its mode is set to
//     executable (`0o755`), and the temp file is atomically renamed to the final
//     destination `binDir/weaver`.
//
// Why a temp file + rename
//   - Atomic replacement: renaming a file within the same filesystem is atomic on
//     POSIX. This guarantees other processes see either the old binary or the
//     fully-written new one, never a half-written file.
//   - Crash/failure safety: if the copy fails (disk full, interrupt, etc.) the
//     existing installed binary is not touched; the incomplete temp file can be
//     removed without corrupting the installation.
//   - Running processes remain valid: on Unix, processes holding the old inode
//     continue to run unaffected after the file at the destination is replaced.
//   - Correct final state: permissions and any finalization (e.g. fsync if added)
//     can be applied to the temp file before it becomes visible at the final
//     path.
//
// Implementation notes
//   - The temp file is created inside `binDir` to ensure the rename is a same-
//     filesystem move (required for atomicity).
//   - If creating a symlink at `/usr/local/bin/weaver` fails the step logs a
//     warning but does not treat this as a hard error (installation can still
//     succeed without the symlink).
//   - The step returns an automa success or failure report describing the outcome.
//   - Elevated permissions (e.g. `sudo`) are typically required to write to the
//     system `binDir` or create the symlink in `/usr/local/bin`.
//
// Usage
//   - Intended to be executed as part of an installation workflow; callers should
//     ensure the process has the required permissions when calling this step.
func InstallWeaver(binDir string) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("install-weaver").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			srcPath, err := os.Executable()
			if err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to locate current executable")))
			}

			if err := os.MkdirAll(binDir, 0o755); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to create bin directory %s", binDir)))
			}

			destPath := filepath.Join(binDir, weaverBinaryName)

			src, err := os.Open(srcPath)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errorx.InternalError.Wrap(err, "failed to open source executable %s", srcPath)))
			}
			defer src.Close()

			// write to a temp file in the destination dir then rename
			tmpFile, err := os.CreateTemp(binDir, weaverBinaryName+".tmp.*")
			if err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.
						Wrap(err, "failed to create temp file in %s", binDir)))
			}
			tmpPath := tmpFile.Name()

			if _, err := io.Copy(tmpFile, src); err != nil {
				_ = tmpFile.Close()
				_ = os.Remove(tmpPath)
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to copy binary")))
			}

			if err := tmpFile.Close(); err != nil {
				_ = os.Remove(tmpPath)
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to finalize temp file")))
			}

			// ensure executable permission
			if err := os.Chmod(tmpPath, 0o755); err != nil {
				_ = os.Remove(tmpPath)
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.
						Wrap(err, "failed to set executable permission")))
			}

			// atomically move into place
			if err := os.Rename(tmpPath, destPath); err != nil {
				_ = os.Remove(tmpPath)
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.
						Wrap(err, "failed to install binary to %s", destPath)))
			}

			// create a symlink to usr/local/bin if possible
			symlinkPath := filepath.Join("/usr/local/bin", weaverBinaryName)
			_ = os.Remove(symlinkPath) // ignore error
			if err := os.Symlink(destPath, symlinkPath); err != nil {
				logx.As().Warn().
					Str("weaver_path", destPath).
					Str("symlink_path", symlinkPath).
					Err(err).
					Msg("Failed to create symlink to weaver binary in /usr/local/bin")
			} else {
				logx.As().Info().
					Str("weaver_path", destPath).
					Str("symlink_path", symlinkPath).
					Msg("Created symlink to weaver binary in /usr/local/bin")
			}

			logx.As().Info().
				Str("weaver_path", destPath).
				Msg("Weaver installed successfully")

			return automa.StepSuccessReport(stp.Id())
		})
}

func UninstallWeaver(binDir string) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("uninstall-weaver").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			destPath := filepath.Join(binDir, weaverBinaryName)

			if err := os.Remove(destPath); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.
						Wrap(err, "failed to remove weaver binary at %s", destPath)))
			}

			symlinkPath := filepath.Join("/usr/local/bin", weaverBinaryName)
			_ = os.Remove(symlinkPath) // ignore error

			logx.As().Info().
				Str("weaver_path", destPath).
				Msg("Weaver uninstalled successfully")

			return automa.StepSuccessReport(stp.Id())
		})
}
