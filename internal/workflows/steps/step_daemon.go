// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/daemon"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
	pkgos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/joomcode/errorx"
)

const (
	daemonServiceTemplatePath = "files/weaver/solo-provisioner-daemon.service"
	daemonServiceName         = "solo-provisioner-daemon"
)

// daemonVersionOutput is the JSON structure emitted by `solo-provisioner-daemon --version`.
type daemonVersionOutput struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

// DaemonBinarySource describes where to obtain the daemon binary.
// When BinPath is empty the binary is auto-downloaded from the URL embedded in
// the infrastructure catalog (pkg/software/infrastructure-catalog.yaml) and
// verified against its per-arch sha256 checksum.
// When BinPath is set, Checksum (sha256 hex) may be supplied to verify the
// binary before it is installed.
type DaemonBinarySource struct {
	// BinPath is the local path to the binary. Empty means auto-download.
	BinPath string
	// Checksum is an optional sha256 hex digest to verify BinPath.
	// Ignored when BinPath is empty (the catalog checksum is used instead).
	Checksum string
}

// InstallDaemonBinaryStep obtains, verifies, and installs the solo-provisioner-daemon
// binary at paths.BinDir/solo-provisioner-daemon.
//
// Resolution order:
//  1. src.BinPath == "": auto-download via the infrastructure catalog, verify
//     sha256 against the embedded per-arch checksum.
//  2. src.BinPath set + src.Checksum set: verify sha256 of BinPath before installing.
//  3. src.BinPath set (no checksum): copy as-is after confirming the file exists.
//
// Rollback removes the installed binary.
func InstallDaemonBinaryStep(src DaemonBinarySource, paths models.WeaverPaths) *automa.StepBuilder {
	dstPath := filepath.Join(paths.BinDir, software.DaemonBinaryName)

	return automa.NewStepBuilder().WithId("install-daemon-binary").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			// Refuse to overwrite a running daemon binary — copyBinaryFile uses
			// O_TRUNC which fails with "text file busy" on an active executable.
			// The operator must stop the service before installing a new binary.
			if running, _ := pkgos.IsServiceRunning(ctx, daemonServiceName); running {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.New(
						"daemon service '%s' is already running — stop it before installing", daemonServiceName).
						WithProperty(models.ErrPropertyResolution, []string{
							"Stop the service, re-run install, then verify:",
							"  sudo solo-provisioner daemon service stop",
							"  sudo solo-provisioner daemon service install [flags]",
							"  sudo solo-provisioner daemon service check",
						})))
			}

			if src.BinPath == "" {
				// ── Auto-download path: delegate entirely to daemonInstaller ────────
				installer, err := software.NewDaemonInstaller()
				if err != nil {
					return automa.StepFailureReport(stp.Id(),
						automa.WithError(errorx.InternalError.Wrap(err, "failed to initialise daemon installer").
							WithProperty(models.ErrPropertyResolution, []string{
								"The provisioner binary may be built without a catalog entry for solo-provisioner-daemon",
								"Re-install the provisioner: sudo solo-provisioner install",
							})))
				}

				installed, err := installer.IsInstalled()
				if err == nil && installed {
					logx.As().Info().
						Str("version", installer.Version()).
						Msg("Daemon binary already installed, skipping download")
					return automa.StepSuccessReport(stp.Id())
				}

				if err := installer.Download(); err != nil {
					version := installer.Version()
					releasesURL := "https://github.com/hashgraph/solo-weaver/releases/tag/daemon-v" + version
					return automa.StepFailureReport(stp.Id(),
						automa.WithError(errorx.InternalError.Wrap(err, "failed to download daemon binary version %s", version).
							WithProperty(models.ErrPropertyResolution, []string{
								fmt.Sprintf("Verify the release exists: %s", releasesURL),
								"Check network connectivity: curl -I https://github.com",
								fmt.Sprintf("Download manually from: %s", releasesURL),
								fmt.Sprintf("Then install with: sudo solo-provisioner daemon service install --daemon-bin=<path-to-binary>"),
							})))
				}

				if err := installer.Install(); err != nil {
					return automa.StepFailureReport(stp.Id(),
						automa.WithError(errorx.InternalError.Wrap(err, "failed to install daemon binary").
							WithProperty(models.ErrPropertyResolution, []string{
								"Check available disk space: df -h " + paths.BinDir,
								"Ensure the target directory is writable: ls -la " + paths.BinDir,
							})))
				}

				_ = installer.Cleanup()

				logx.As().Info().
					Str("dst", dstPath).
					Str("version", installer.Version()).
					Msg("Daemon binary installed")
				return automa.StepSuccessReport(stp.Id())
			}

			// ── Manual binary path ──────────────────────────────────────────────
			if _, err := os.Stat(src.BinPath); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalArgument.Wrap(err, "daemon binary not found at %s", src.BinPath).
						WithProperty(models.ErrPropertyResolution, []string{
							"Verify the path: ls -la " + src.BinPath,
							"Omit --daemon-bin to auto-download the official binary",
						})))
			}

			if src.Checksum != "" {
				if err := software.VerifyChecksum(src.BinPath, src.Checksum, "sha256"); err != nil {
					return automa.StepFailureReport(stp.Id(),
						automa.WithError(errorx.InternalError.Wrap(err,
							"daemon binary at %s failed sha256 verification", src.BinPath).
							WithProperty(models.ErrPropertyResolution, []string{
								"Ensure you supplied the correct binary and checksum",
								"Re-download the binary from the official releases page",
							})))
				}
				logx.As().Info().Str("path", src.BinPath).Msg("Daemon binary sha256 verified")
			}

			// Run --version to confirm the binary executes on this host's platform.
			// A wrong-arch binary (e.g. amd64 binary on arm64 host) will fail here
			// with a clear error rather than silently installing an unusable binary.
			out, err := exec.CommandContext(ctx, src.BinPath, "--version").Output() //nolint:gosec
			if err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err,
						"daemon binary at %s cannot execute on this host — wrong platform?", src.BinPath).
						WithProperty(models.ErrPropertyResolution, []string{
							"Ensure the binary targets this host's OS/arch (linux/amd64 or linux/arm64)",
							"Verify: file " + src.BinPath,
							"If built locally, rebuild with: task build:daemon GOOS=linux GOARCH=<arch>",
						})))
			}
			var vout daemonVersionOutput
			if err := json.Unmarshal(out, &vout); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err,
						"failed to parse --version output from %s: %s", src.BinPath, string(out)).
						WithProperty(models.ErrPropertyResolution, []string{
							"Ensure the binary is a valid solo-provisioner-daemon build",
							"Test manually: " + src.BinPath + " --version",
						})))
			}
			logx.As().Info().
				Str("path", src.BinPath).
				Str("version", vout.Version).
				Str("commit", vout.Commit).
				Msg("Daemon binary platform verified")

			if err := os.MkdirAll(paths.BinDir, 0o755); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to create bin directory %s", paths.BinDir).
						WithProperty(models.ErrPropertyResolution, []string{
							"Check available disk space: df -h " + paths.BinDir,
							"Ensure the parent directory is writable: ls -la " + filepath.Dir(paths.BinDir),
						})))
			}
			if err := copyBinaryFile(src.BinPath, dstPath); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to install daemon binary to %s", dstPath).
						WithProperty(models.ErrPropertyResolution, []string{
							"Check available disk space: df -h " + paths.BinDir,
							"Ensure the target directory is writable: ls -la " + paths.BinDir,
						})))
			}

			logx.As().Info().
				Str("src", src.BinPath).
				Str("dst", dstPath).
				Msg("Daemon binary installed")
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			if src.BinPath == "" {
				notify.As().StepStart(ctx, stp, "Downloading and installing solo-provisioner-daemon binary")
			} else {
				notify.As().StepStart(ctx, stp, "Installing solo-provisioner-daemon binary")
			}
			return ctx, nil
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			_ = os.Remove(dstPath)
			return automa.StepSuccessReport(stp.Id())
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to install daemon binary")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Daemon binary installed")
		})
}

// copyBinaryFile copies src to dst with executable permissions (0755).
func copyBinaryFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// installDaemonServiceFiles writes the unit template to the sandbox path and
// creates the /usr/lib/systemd/system symlink that points to it.
// sandboxPath — $home/sandbox/usr/lib/systemd/system/solo-provisioner-daemon.service
// symlinkPath — /usr/lib/systemd/system/solo-provisioner-daemon.service
func installDaemonServiceFiles(sandboxPath, symlinkPath string) error {
	content, err := templates.Files.ReadFile(daemonServiceTemplatePath)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to read daemon service template")
	}

	if err := os.MkdirAll(filepath.Dir(sandboxPath), 0o755); err != nil {
		return errorx.InternalError.Wrap(err, "failed to create sandbox systemd directory %s", filepath.Dir(sandboxPath))
	}

	if err := os.WriteFile(sandboxPath, content, 0o644); err != nil {
		return errorx.InternalError.Wrap(err, "failed to write daemon service file to %s", sandboxPath)
	}

	// Remove any stale symlink before creating the new one.
	_ = os.Remove(symlinkPath)
	if err := os.Symlink(sandboxPath, symlinkPath); err != nil {
		// Clean up the sandbox file so a failed install doesn't leave a half-installed state.
		_ = os.Remove(sandboxPath)
		return errorx.InternalError.Wrap(err, "failed to create systemd symlink %s -> %s", symlinkPath, sandboxPath)
	}

	return nil
}

// removeDaemonServiceFiles removes the /usr/lib/systemd/system symlink and the
// sandbox unit file. Errors for non-existent paths are ignored.
func removeDaemonServiceFiles(sandboxPath, symlinkPath string) {
	_ = os.Remove(symlinkPath)
	_ = os.Remove(sandboxPath)
}

// InstallDaemonServiceStep installs the solo-provisioner-daemon systemd service
// unit file into the weaver sandbox, creates a symlink at
// /usr/lib/systemd/system/solo-provisioner-daemon.service, runs daemon-reload,
// enables, and starts the service.
func InstallDaemonServiceStep(paths models.WeaverPaths) *automa.StepBuilder {
	sandboxPath := paths.DaemonServiceSandboxPath
	symlinkPath := paths.DaemonServiceSymlinkPath

	return automa.NewStepBuilder().WithId("install-daemon-service").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if err := installDaemonServiceFiles(sandboxPath, symlinkPath); err != nil {
				// installDaemonServiceFiles already returns an *errorx.Error; cast so we
				// can attach resolution hints without importing a second error package.
				var errWithHints error = err
				if ex := errorx.Cast(err); ex != nil {
					errWithHints = ex.WithProperty(models.ErrPropertyResolution, []string{
						fmt.Sprintf("Ensure directory is writable: ls -la %s", filepath.Dir(sandboxPath)),
						fmt.Sprintf("Check available disk space: df -h %s", filepath.Dir(sandboxPath)),
						"Re-run: sudo solo-provisioner daemon service install",
					})
				}
				return automa.StepFailureReport(stp.Id(), automa.WithError(errWithHints))
			}

			if err := pkgos.DaemonReload(ctx); err != nil {
				removeDaemonServiceFiles(sandboxPath, symlinkPath)
				errWithHints := errorx.InternalError.Wrap(err, "daemon-reload failed after writing daemon service file").
					WithProperty(models.ErrPropertyResolution, []string{
						"Run manually: sudo systemctl daemon-reload",
						"Check systemd status: sudo systemctl status",
						"Review journalctl: sudo journalctl -xe --no-pager | tail -30",
					})
				return automa.StepFailureReport(stp.Id(), automa.WithError(errWithHints))
			}

			if err := pkgos.EnableService(ctx, daemonServiceName); err != nil {
				removeDaemonServiceFiles(sandboxPath, symlinkPath)
				_ = pkgos.DaemonReload(ctx)
				errWithHints := errorx.InternalError.Wrap(err, "failed to enable service %s", daemonServiceName).
					WithProperty(models.ErrPropertyResolution, []string{
						fmt.Sprintf("Run manually: sudo systemctl enable %s", daemonServiceName),
						fmt.Sprintf("Check unit file: ls -la %s", symlinkPath),
						"Review journalctl: sudo journalctl -xe --no-pager | tail -30",
					})
				return automa.StepFailureReport(stp.Id(), automa.WithError(errWithHints))
			}

			if err := pkgos.RestartService(ctx, daemonServiceName); err != nil {
				errWithHints := errorx.InternalError.Wrap(err, "failed to start service %s", daemonServiceName).
					WithProperty(models.ErrPropertyResolution, []string{
						fmt.Sprintf("Check daemon logs: sudo journalctl -u %s -n 50 --no-pager", daemonServiceName),
						fmt.Sprintf("Check service status: sudo systemctl status %s", daemonServiceName),
						"Verify daemon binary exists: ls -la /opt/solo/weaver/bin/solo-provisioner-daemon",
						"Verify daemon config: cat /opt/solo/weaver/config/daemon.yaml",
						"Verify daemon kubeconfig: ls -la /opt/solo/weaver/config/daemon.kubeconfig",
						"Fix config issues then retry: sudo solo-provisioner daemon service start",
					})
				return automa.StepFailureReport(stp.Id(), automa.WithError(errWithHints))
			}

			logx.As().Info().
				Str("sandbox_path", sandboxPath).
				Str("symlink_path", symlinkPath).
				Msg("Solo Provisioner Daemon service installed, enabled, and started")
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Installing solo-provisioner-daemon systemd service")
			return ctx, nil
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			_ = pkgos.DisableService(ctx, daemonServiceName)
			removeDaemonServiceFiles(sandboxPath, symlinkPath)
			_ = pkgos.DaemonReload(ctx)
			return automa.StepSuccessReport(stp.Id())
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to install solo-provisioner-daemon service")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Solo Provisioner Daemon service installed and enabled")
		})
}

// RemoveDaemonServiceStep stops, disables, and removes the
// solo-provisioner-daemon systemd service — both the system symlink and the
// sandbox unit file.
func RemoveDaemonServiceStep(paths models.WeaverPaths) *automa.StepBuilder {
	sandboxPath := paths.DaemonServiceSandboxPath
	symlinkPath := paths.DaemonServiceSymlinkPath

	return automa.NewStepBuilder().WithId("remove-daemon-service").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			// Stop before disable — disabling a running unit does not stop it.
			_ = pkgos.StopService(ctx, daemonServiceName)
			_ = pkgos.DisableService(ctx, daemonServiceName)

			removeDaemonServiceFiles(sandboxPath, symlinkPath)

			if err := pkgos.DaemonReload(ctx); err != nil {
				logx.As().Warn().Err(err).Msg("daemon-reload failed after removing daemon service file")
			}

			logx.As().Info().
				Str("sandbox_path", sandboxPath).
				Str("symlink_path", symlinkPath).
				Msg("Solo Provisioner Daemon service removed")
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Removing solo-provisioner-daemon systemd service")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to remove solo-provisioner-daemon service")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Solo Provisioner Daemon service removed")
		})
}

// RemoveDaemonBinaryStep removes the solo-provisioner-daemon binary from
// paths.BinDir. This is a best-effort step — a missing binary is not an error.
func RemoveDaemonBinaryStep(paths models.WeaverPaths) *automa.StepBuilder {
	binPath := filepath.Join(paths.BinDir, software.DaemonBinaryName)

	return automa.NewStepBuilder().WithId("remove-daemon-binary").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if err := os.Remove(binPath); err != nil && !os.IsNotExist(err) {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to remove daemon binary at %s", binPath).
						WithProperty(models.ErrPropertyResolution, []string{
							"Remove manually: sudo rm " + binPath,
						})))
			}
			logx.As().Info().Str("path", binPath).Msg("Daemon binary removed")
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Removing solo-provisioner-daemon binary")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to remove solo-provisioner-daemon binary")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Solo Provisioner Daemon binary removed")
		})
}

// StartDaemonServiceStep starts the solo-provisioner-daemon systemd service.
func StartDaemonServiceStep() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("start-daemon-service").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if err := pkgos.StartService(ctx, daemonServiceName); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to start service %s", daemonServiceName).
						WithProperty(models.ErrPropertyResolution, []string{
							fmt.Sprintf("Check daemon logs: sudo journalctl -u %s -n 50 --no-pager", daemonServiceName),
							fmt.Sprintf("Check service status: sudo systemctl status %s", daemonServiceName),
							"Verify daemon binary: ls -la /opt/solo/weaver/bin/solo-provisioner-daemon",
							"Verify daemon config: cat /opt/solo/weaver/config/daemon.yaml",
							"If not yet installed: sudo solo-provisioner daemon service install",
						})))
			}
			logx.As().Info().Msgf("Service %s started", daemonServiceName)
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Starting solo-provisioner-daemon systemd service")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to start solo-provisioner-daemon service")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Solo Provisioner Daemon service started")
		})
}

// StopDaemonServiceStep stops the solo-provisioner-daemon systemd service and
// verifies the service is no longer running.
func StopDaemonServiceStep() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("stop-daemon-service").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if err := pkgos.StopService(ctx, daemonServiceName); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to stop service %s", daemonServiceName).
						WithProperty(models.ErrPropertyResolution, []string{
							fmt.Sprintf("Check service status: sudo systemctl status %s", daemonServiceName),
							fmt.Sprintf("Check daemon logs: sudo journalctl -u %s -n 50 --no-pager", daemonServiceName),
							fmt.Sprintf("Force-kill if stuck: sudo systemctl kill %s", daemonServiceName),
						})))
			}

			if running, _ := pkgos.IsServiceRunning(ctx, daemonServiceName); running {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.New("service %s is still running after stop", daemonServiceName).
						WithProperty(models.ErrPropertyResolution, []string{
							fmt.Sprintf("Force-kill: sudo systemctl kill %s", daemonServiceName),
							fmt.Sprintf("Check status: sudo systemctl status %s", daemonServiceName),
						})))
			}

			logx.As().Info().Msgf("Service %s stopped", daemonServiceName)
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Stopping solo-provisioner-daemon systemd service")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to stop solo-provisioner-daemon service")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Solo Provisioner Daemon service stopped")
		})
}

// CheckDaemonServiceStep verifies the daemon installation and runtime health:
//  1. Sandbox unit file exists at $home/sandbox/usr/lib/systemd/system/solo-provisioner-daemon.service
//  2. System symlink exists at /usr/lib/systemd/system/solo-provisioner-daemon.service → sandbox file
//  3. Service is enabled (systemctl is-enabled)
//  4. Service is active/running (systemctl is-active)
//  5. Daemon binary exists at /opt/solo/weaver/bin/solo-provisioner-daemon
//  6. Sudoers entry exists at /etc/sudoers.d/solo-provisioner
//  7. Unix socket responds to GET /health → HTTP 200
func CheckDaemonServiceStep(paths models.WeaverPaths, sockPath string) *automa.StepBuilder {
	sandboxPath := paths.DaemonServiceSandboxPath
	symlinkPath := paths.DaemonServiceSymlinkPath

	return automa.NewStepBuilder().WithId("check-daemon-service").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			// 1. Sandbox unit file
			if _, err := os.Stat(sandboxPath); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.New("daemon service unit file not found at %s", sandboxPath).
						WithProperty(models.ErrPropertyResolution, []string{
							"Run: sudo solo-provisioner daemon service install",
						})))
			}
			meta["unit_file"] = sandboxPath

			// 2. System symlink — must exist and point to the sandbox file
			linkTarget, err := os.Readlink(symlinkPath)
			if err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.New("daemon service symlink not found at %s", symlinkPath).
						WithProperty(models.ErrPropertyResolution, []string{
							"Run: sudo solo-provisioner daemon service install",
						})))
			}
			if filepath.Clean(linkTarget) != filepath.Clean(sandboxPath) {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.New("daemon service symlink %s points to %s, expected %s", symlinkPath, linkTarget, sandboxPath).
						WithProperty(models.ErrPropertyResolution, []string{
							"Remove the stale symlink and reinstall: sudo rm " + symlinkPath,
							"Then run: sudo solo-provisioner daemon service install",
						})))
			}
			meta["symlink"] = symlinkPath

			// 3. Service enabled
			enabled, err := pkgos.IsServiceEnabled(ctx, daemonServiceName)
			if err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to check if %s is enabled", daemonServiceName).
						WithProperty(models.ErrPropertyResolution, []string{
							"Check systemd status: sudo systemctl status " + daemonServiceName,
							"Review journalctl: sudo journalctl -xe --no-pager | tail -30",
						})))
			}
			meta["enabled"] = fmt.Sprintf("%v", enabled)
			if !enabled {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.New("daemon service %s is not enabled", daemonServiceName).
						WithProperty(models.ErrPropertyResolution, []string{
							"Enable and start the service: sudo solo-provisioner daemon service install",
							"Or manually: sudo systemctl enable --now " + daemonServiceName,
						})))
			}

			// 4. Service active
			running, err := pkgos.IsServiceRunning(ctx, daemonServiceName)
			if err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to check if %s is running", daemonServiceName).
						WithProperty(models.ErrPropertyResolution, []string{
							"Check service status: sudo systemctl status " + daemonServiceName,
							"Check daemon logs: sudo journalctl -u " + daemonServiceName + " -n 50 --no-pager",
						})))
			}
			meta["running"] = fmt.Sprintf("%v", running)
			if !running {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.New("daemon service %s is not running", daemonServiceName).
						WithProperty(models.ErrPropertyResolution, []string{
							"Start the service: sudo solo-provisioner daemon service start",
							"Check daemon logs: sudo journalctl -u " + daemonServiceName + " -n 50 --no-pager",
						})))
			}

			// 5. Daemon binary — path derived from WeaverPaths so tests and
			// future path changes don't require touching this step.
			daemonBinPath := filepath.Join(paths.BinDir, "solo-provisioner-daemon")
			if _, err := os.Stat(daemonBinPath); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.New("daemon binary not found at %s", daemonBinPath).
						WithProperty(models.ErrPropertyResolution, []string{
							"Reinstall the daemon: sudo solo-provisioner daemon service install --daemon-bin <path>",
						})))
			}
			meta["binary"] = daemonBinPath

			// 6. Sudoers entry
			if _, err := os.Stat(sudoersDstPath); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.New("sudoers entry not found at %s", sudoersDstPath).
						WithProperty(models.ErrPropertyResolution, []string{
							"Run: sudo solo-provisioner install",
						})))
			}
			meta["sudoers"] = sudoersDstPath

			// 7. Unix socket health — proxy is explicitly disabled so that
			// HTTP(S)_PROXY env vars don't redirect the request away from the
			// local Unix socket.
			client := &http.Client{
				Timeout: 5 * time.Second,
				Transport: &http.Transport{
					DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
						return (&net.Dialer{}).DialContext(ctx, "unix", sockPath)
					},
					Proxy: nil, // disable proxy for local Unix socket requests
				},
			}
			resp, err := client.Get("http://local/health")
			if err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.Wrap(err, "daemon socket not reachable at %s", sockPath).
						WithProperty(models.ErrPropertyResolution, []string{
							"Check the daemon is running: sudo systemctl status " + daemonServiceName,
							"Start if stopped: sudo solo-provisioner daemon service start",
							"Check daemon logs: sudo journalctl -u " + daemonServiceName + " -n 50 --no-pager",
						})))
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.New("daemon health check returned HTTP %d", resp.StatusCode).
						WithProperty(models.ErrPropertyResolution, []string{
							"Check daemon logs: sudo journalctl -u " + daemonServiceName + " -n 50 --no-pager",
							"Restart the daemon: sudo solo-provisioner daemon service stop && sudo solo-provisioner daemon service start",
						})))
			}
			meta["socket"] = sockPath
			meta["health"] = "ok"

			logx.As().Info().Fields(meta).Msg("Solo Provisioner Daemon service is healthy")
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking solo-provisioner-daemon service health")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Solo Provisioner Daemon service check failed")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Solo Provisioner Daemon service is healthy")
		})
}

// socketClient returns an HTTP client that connects to the daemon Unix socket.
func socketClient(sockPath string) *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", sockPath)
			},
			Proxy: nil,
		},
	}
}

// FetchDaemonStatus fetches GET /status from the daemon socket and returns the
// decoded response. Returns nil if the endpoint is unreachable or returns a
// non-200 status — callers treat nil as "status unavailable, skip".
func FetchDaemonStatus(sockPath string) *daemon.StatusResponse {
	resp, err := socketClient(sockPath).Get("http://local/status")
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil
	}
	defer resp.Body.Close()

	var status daemon.StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil
	}
	return &status
}

// CheckDaemonComponentPrerequisitesStep wraps CheckDaemonComponentPrerequisites
// as a workflow step so that install (and any future workflow) can surface probe
// failures in the TUI without post-workflow logic in the command RunE.
// The step succeeds immediately when no probe errors are present.
func CheckDaemonComponentPrerequisitesStep(sockPath string) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("check-daemon-component-prerequisites").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if warning := CheckDaemonComponentPrerequisites(sockPath); warning != "" {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.New(
						"daemon installed but component prerequisites are not satisfied — "+
							"fix the issues listed above and re-run: solo-provisioner daemon service check").
						WithProperty(models.ErrPropertyResolution, []string{warning})))
			}
			return automa.SuccessReport(stp, automa.WithMetadata(map[string]string{"status": "all prerequisites satisfied"}))
		})
}

// CheckDaemonComponentPrerequisites queries GET /status on the daemon socket at
// sockPath and returns a human-readable warning string when any component probe
// errors are present, or an empty string when all prerequisites are satisfied.
// It is called by `daemon service check` after the main health workflow passes,
// to surface disk prerequisite failures as a distinct non-zero exit.
func CheckDaemonComponentPrerequisites(sockPath string) string {
	status := FetchDaemonStatus(sockPath)
	if status == nil || len(status.ProbeErrors) == 0 {
		return ""
	}

	components := make([]string, 0, len(status.ProbeErrors))
	for c := range status.ProbeErrors {
		components = append(components, c)
	}
	sort.Strings(components)

	var sb strings.Builder
	sb.WriteString("Component prerequisites not yet satisfied — automation will not run until resolved:\n")
	for _, c := range components {
		sb.WriteString(fmt.Sprintf("  [NOT READY] %s: %s\n", c, status.ProbeErrors[c]))
	}
	sb.WriteString("\nFix the issues above then re-run: solo-provisioner daemon service check")
	return sb.String()
}
