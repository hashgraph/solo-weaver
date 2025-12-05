package steps

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-weaver/internal/doctor"
	"golang.hedera.com/solo-weaver/internal/version"
)

const weaverBinaryName = "weaver"

var errWeaverInstallationRequired = errorx.IllegalState.
	New("weaver installation or re-installation required").
	WithProperty(doctor.ErrPropertyResolution, "install or re-install weaver binary, run `sudo weaver install`")

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
				logx.As().Error().
					Str("exePath", exePath).
					Str("expectedPath", expectedPath).
					Msg("Weaver installation check failed: current executable is not in the expected bin directory")
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errWeaverInstallationRequired))
			}

			meta := map[string]string{
				"weaver_path":       exePath,
				"installed_version": version.Number(),
				"installed_commit":  version.Commit(),
			}

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		})
}

// InstallWeaver installs weaver at the given binDir by copying the current executable to that location.
// It also attempts to create a symlink in /usr/local/bin for easier access.
// Note: This step require elevated permissions to write to the target binDir.
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
				return automa.StepFailureReport(stp.Id(), automa.WithError(errorx.InternalError.Wrap(err, "failed to open source executable %s: %w", srcPath, err)))
			}
			defer src.Close()

			// write to a temp file in the destination dir then rename
			tmpFile, err := os.CreateTemp(binDir, weaverBinaryName+".tmp.*")
			if err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.
						Wrap(err, "failed to create temp file in %s: %w", binDir, err)))
			}
			tmpPath := tmpFile.Name()

			if _, err := io.Copy(tmpFile, src); err != nil {
				_ = tmpFile.Close()
				_ = os.Remove(tmpPath)
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to copy binary: %w", err)))
			}

			if err := tmpFile.Close(); err != nil {
				_ = os.Remove(tmpPath)
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to finalize temp file: %w", err)))
			}

			// ensure executable permission
			if err := os.Chmod(tmpPath, 0o755); err != nil {
				_ = os.Remove(tmpPath)
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.
						Wrap(err, "failed to set executable permission: %w", err)))
			}

			// atomically move into place
			if err := os.Rename(tmpPath, destPath); err != nil {
				_ = os.Remove(tmpPath)
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.
						Wrap(err, "failed to install binary to %s: %w", destPath, err)))
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
