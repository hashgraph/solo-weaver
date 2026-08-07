// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/automa-saga/version"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	pkgos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/hashgraph/solo-weaver/pkg/security/sudoers"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/joomcode/errorx"
)

const weaverBinaryName = "solo-provisioner"

var errWeaverInstallationRequired = errorx.IllegalState.New("solo-provisioner installation or re-installation required")

// CheckWeaverInstallation checks if solo-provisioner is installed at the given binDir.
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

				resolution := fmt.Sprintf("install or re-install solo-provisioner binary; "+
					"run `sudo %s install` to install and then run `solo-provisioner%s`.", exePath, args)

				errWithResolution := errWeaverInstallationRequired.WithProperty(doctor.ErrPropertyResolution, resolution)

				logx.As().Error().
					Err(errWithResolution).
					Str("exePath", exePath).
					Str("expectedPath", expectedPath).
					Msg("Solo Provisioner installation check failed: current executable is not in the expected bin directory")

				return automa.StepFailureReport(stp.Id(), automa.WithError(errWithResolution))
			}

			meta := map[string]string{
				"weaver_path":       exePath,
				"installed_version": version.Get().Version,
				"installed_commit":  version.Get().Commit,
			}

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking solo-provisioner installation")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Solo Provisioner installation check failed")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Solo Provisioner installation verified")
		})
}

// InstallWeaver installs the currently running executable as the `solo-provisioner` binary
// into the provided `binDir` and attempts to create a convenience symlink in
// `/usr/local/bin`.
//
// Behavior
//   - The step locates the currently running executable (source).
//   - It ensures `binDir` exists and then copies the source executable into a
//     temporary file created inside `binDir` (pattern `solo-provisioner.tmp.*`).
//   - After the copy completes the temp file is closed, its mode is set to
//     executable (`0o755`), and the temp file is atomically renamed to the final
//     destination `binDir/solo-provisioner`.
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
//   - If creating a symlink at `/usr/local/bin/solo-provisioner` fails the step logs a
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
					Msg("Failed to create symlink to solo-provisioner binary in /usr/local/bin")
			} else {
				logx.As().Info().
					Str("weaver_path", destPath).
					Str("symlink_path", symlinkPath).
					Msg("Created symlink to solo-provisioner binary in /usr/local/bin")
			}

			logx.As().Info().
				Str("weaver_path", destPath).
				Msg("Solo Provisioner installed successfully")

			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Installing solo-provisioner")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to install solo-provisioner")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Solo Provisioner installed successfully")
		})
}

// InstallColocatedDaemonBinary installs a solo-provisioner-daemon binary found
// beside the running executable into binDir, so that a block-node install with
// traffic shaping enabled has a daemon binary to use without the operator
// supplying a path.
//
// Self-install is the only command that runs from outside binDir — every other
// command is pinned there by CheckWeaverInstallation — so it is also the only
// point at which the CLI can still see the daemon binary that was built next to
// it. `task build` emits both binaries into the same bin/, so after
// `sudo bin/solo-provisioner-<os>-<arch> install` the CLI and the daemon on the
// host are always a matching pair, and rebuilding refreshes both.
//
// The step is best-effort: an official install downloads the CLI on its own and
// has no sibling daemon binary to find, in which case this is a no-op and the
// daemon is obtained later from the infrastructure catalog. Nothing here can
// fail the self-install.
func InstallColocatedDaemonBinary(binDir string) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("install-colocated-daemon-binary").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			exePath, err := os.Executable()
			if err != nil {
				logx.As().Warn().Err(err).Msg(
					"could not locate the current executable; skipping co-located daemon binary install")
				return automa.StepSuccessReport(stp.Id())
			}

			srcPath := findColocatedDaemonBinary(filepath.Dir(exePath))
			if srcPath == "" {
				logx.As().Debug().
					Str("search_dir", filepath.Dir(exePath)).
					Msg("No co-located solo-provisioner-daemon binary found; it will be downloaded when needed")
				return automa.StepSuccessReport(stp.Id())
			}

			// Re-running `install` from the already-installed location makes the
			// search dir and binDir the same, so the sibling found above IS the
			// destination. copyBinaryFile opens the destination O_TRUNC, which would
			// erase the installed daemon binary.
			dstPath := filepath.Join(binDir, software.DaemonBinaryName)
			if sameFile(srcPath, dstPath) {
				logx.As().Debug().Str("path", dstPath).
					Msg("Co-located solo-provisioner-daemon binary is already the installed one; nothing to copy")
				return automa.StepSuccessReport(stp.Id())
			}

			// Atomic copy, not copyBinaryFile: this step swallows its own errors, so a
			// half-written destination would be left behind AND reported as success,
			// corrupting a daemon binary that was working before self-install ran.
			if err := copyBinaryAtomic(srcPath, dstPath); err != nil {
				logx.As().Warn().Err(err).
					Str("src", srcPath).
					Str("dst", dstPath).
					Msg("Failed to install co-located solo-provisioner-daemon binary; it will be downloaded when needed")
				return automa.StepSuccessReport(stp.Id())
			}

			logx.As().Info().
				Str("src", srcPath).
				Str("dst", dstPath).
				Msg("Co-located solo-provisioner-daemon binary installed")

			// The rename above leaves a running daemon on its old inode, so the process
			// keeps executing the previous binary. Say so rather than letting the
			// on-disk and running versions diverge silently.
			if running, err := pkgos.IsServiceRunning(ctx, daemonServiceName); err == nil && running {
				logx.As().Info().Str("service", daemonServiceName).Msg(
					"solo-provisioner-daemon is running and keeps its current binary; " +
						"restart the service to run the newly installed one")
			}
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Installing co-located solo-provisioner-daemon binary")
			return ctx, nil
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Co-located solo-provisioner-daemon binary handled")
		})
}

// findColocatedDaemonBinary returns the path to a daemon binary in dir, or "" if
// none is present. The plain name is preferred over the platform-suffixed one so
// that a dir holding both (an install target that already has the daemon, plus
// leftover build artifacts) resolves to the canonical name.
func findColocatedDaemonBinary(dir string) string {
	candidates := []string{
		software.DaemonBinaryName,
		fmt.Sprintf("%s-%s-%s", software.DaemonBinaryName, runtime.GOOS, runtime.GOARCH),
	}
	for _, name := range candidates {
		p := filepath.Join(dir, name)
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		return p
	}
	return ""
}

const (
	sudoersTemplatePath = "files/weaver/sudoers"
	sudoersDstPath      = "/etc/sudoers.d/solo-provisioner"
)

// InstallSudoersStep writes the weaver sudoers entry to /etc/sudoers.d/solo-provisioner.
func InstallSudoersStep() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("install-sudoers").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			content, err := templates.Files.ReadFile(sudoersTemplatePath)
			if err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to read sudoers template")))
			}

			if err := sudoers.WriteEntry(sudoersDstPath, content); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to install sudoers entry")))
			}

			logx.As().Info().Str("path", sudoersDstPath).Msg("Sudoers entry installed")
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Installing sudoers entry")
			return ctx, nil
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			sudoers.Cleanup(sudoersDstPath)
			return automa.StepSuccessReport(stp.Id())
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to install sudoers entry")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Sudoers entry installed")
		})
}

// RemoveSudoersStep removes the weaver sudoers entry from /etc/sudoers.d/solo-provisioner.
func RemoveSudoersStep() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("remove-sudoers").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if err := os.Remove(sudoersDstPath); err != nil && !os.IsNotExist(err) {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to remove sudoers file %s", sudoersDstPath)))
			}
			logx.As().Info().Str("path", sudoersDstPath).Msg("Sudoers entry removed")
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Removing sudoers entry")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to remove sudoers entry")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Sudoers entry removed")
		})
}

func UninstallWeaver(binDir string) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("uninstall-weaver").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			destPath := filepath.Join(binDir, weaverBinaryName)

			if err := os.Remove(destPath); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.
						Wrap(err, "failed to remove solo-provisioner binary at %s", destPath)))
			}

			symlinkPath := filepath.Join("/usr/local/bin", weaverBinaryName)
			_ = os.Remove(symlinkPath) // ignore error

			logx.As().Info().
				Str("weaver_path", destPath).
				Msg("Solo Provisioner uninstalled successfully")

			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Uninstalling solo-provisioner")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to uninstall solo-provisioner")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Solo Provisioner uninstalled successfully")
		})
}
