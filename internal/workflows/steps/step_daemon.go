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
	"os/user"
	"path/filepath"
	"strconv"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/deps"
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

// DaemonBinarySource describes where to obtain the daemon binary and how to verify it.
// When BinPath is empty the binary is auto-downloaded from the URL in the embedded
// daemon_config.yaml and verified against its embedded checksum.
// When BinPath is set, Checksum (sha256 hex) and/or Commit (git SHA) may be supplied
// to verify the binary before it is installed.
type DaemonBinarySource struct {
	// BinPath is the local path to the binary. Empty means auto-download.
	BinPath string
	// Checksum is an optional sha256 hex digest to verify BinPath.
	// Ignored when BinPath is empty (the embedded checksum is used instead).
	Checksum string
	// Commit is an optional git commit SHA to verify via `<bin> --version`.
	// Ignored when BinPath is empty (the embedded commit is used instead).
	Commit string
}

// InstallDaemonBinaryStep obtains, verifies, and installs the solo-provisioner-daemon
// binary at paths.BinDir/solo-provisioner-daemon.
//
// Resolution order:
//  1. src.BinPath == "": auto-download from the URL in pkg/deps/daemon_config.yaml,
//     verify sha256 against the embedded checksum, verify commit via --version.
//  2. src.BinPath set + src.Checksum set: verify sha256 of BinPath before installing.
//  3. src.BinPath set + src.Commit set: run --version and compare reported commit.
//  4. src.BinPath set (no extra flags): still verify the version string via --version.
//
// Rollback removes the installed binary.
func InstallDaemonBinaryStep(src DaemonBinarySource, paths models.WeaverPaths) *automa.StepBuilder {
	dstPath := filepath.Join(paths.BinDir, "solo-provisioner-daemon")

	return automa.NewStepBuilder().WithId("install-daemon-binary").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			// Load the embedded release spec — needed in all code paths.
			spec, err := deps.LoadDaemonReleaseSpec()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			srcPath := src.BinPath

			if srcPath == "" {
				// ── Auto-download path ──────────────────────────────────────────────
				if spec.DownloadURL == "" {
					return automa.StepFailureReport(stp.Id(),
						automa.WithError(errorx.IllegalState.New(
							"auto-download not configured: daemon_config.yaml has no download_url").
							WithProperty(models.ErrPropertyResolution, []string{
								"Supply the binary manually: sudo solo-provisioner daemon service install --daemon-bin=<path>",
							})))
				}

				downloadDest := filepath.Join(paths.DownloadsDir, "solo-provisioner-daemon")
				logx.As().Info().
					Str("url", spec.DownloadURL).
					Str("dst", downloadDest).
					Msg("Downloading daemon binary")

				downloader := software.NewDownloader(
					software.WithBasePath(paths.HomeDir),
				)
				if err := downloader.Download(spec.DownloadURL, downloadDest); err != nil {
					return automa.StepFailureReport(stp.Id(),
						automa.WithError(errorx.InternalError.Wrap(err, "failed to download daemon binary from %s", spec.DownloadURL).
							WithProperty(models.ErrPropertyResolution, []string{
								"Check network connectivity: curl -I " + spec.DownloadURL,
								fmt.Sprintf("Download manually and supply with: --daemon-bin=<path>"),
							})))
				}

				// Verify sha256 of downloaded binary (mandatory — embedded checksum is
				// the release's authoritative integrity proof).
				if spec.Checksum != "" {
					if err := software.VerifyChecksum(downloadDest, spec.Checksum, spec.Algorithm); err != nil {
						_ = os.Remove(downloadDest)
						return automa.StepFailureReport(stp.Id(),
							automa.WithError(errorx.InternalError.Wrap(err,
								"downloaded daemon binary failed %s verification", spec.Algorithm).
								WithProperty(models.ErrPropertyResolution, []string{
									"The downloaded file may be corrupt or tampered with",
									"Retry the install; if it fails again, report the issue",
									fmt.Sprintf("Expected %s: %s", spec.Algorithm, spec.Checksum),
								})))
					}
				}

				// Make downloaded file executable before running --version.
				if err := os.Chmod(downloadDest, 0o755); err != nil {
					return automa.StepFailureReport(stp.Id(),
						automa.WithError(errorx.InternalError.Wrap(err, "failed to chmod downloaded binary")))
				}

				srcPath = downloadDest
			} else {
				// ── Manual binary path ──────────────────────────────────────────────
				if _, err := os.Stat(srcPath); err != nil {
					return automa.StepFailureReport(stp.Id(),
						automa.WithError(errorx.IllegalArgument.Wrap(err, "daemon binary not found at %s", srcPath).
							WithProperty(models.ErrPropertyResolution, []string{
								"Verify the path: ls -la " + srcPath,
								"Omit --daemon-bin to auto-download the official binary",
							})))
				}

				// Optional sha256 verification.
				if src.Checksum != "" {
					if err := software.VerifyChecksum(srcPath, src.Checksum, "sha256"); err != nil {
						return automa.StepFailureReport(stp.Id(),
							automa.WithError(errorx.InternalError.Wrap(err,
								"daemon binary at %s failed sha256 verification", srcPath).
								WithProperty(models.ErrPropertyResolution, []string{
									"Ensure you supplied the correct binary and checksum",
									"Re-download the binary from the official releases page",
								})))
					}
					logx.As().Info().Str("path", srcPath).Msg("Daemon binary sha256 verified")
				}
			}

			// ── Run --version and verify version + optional commit ──────────────
			out, err := exec.CommandContext(ctx, srcPath, "--version").Output() //nolint:gosec
			if err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to query daemon binary version at %s", srcPath).
						WithProperty(models.ErrPropertyResolution, []string{
							"Ensure the binary is executable: chmod +x " + srcPath,
							"Ensure it is a Linux amd64 binary matching this host",
						})))
			}
			var vout daemonVersionOutput
			if err := json.Unmarshal(out, &vout); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err,
						"failed to parse daemon binary --version output: %s", string(out))))
			}

			// Version must match the release spec.
			if vout.Version != spec.Version {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalArgument.New(
						"daemon binary version mismatch: got %s, required %s", vout.Version, spec.Version).
						WithProperty(models.ErrPropertyResolution, []string{
							fmt.Sprintf("Download version %s: %s", spec.Version, spec.DownloadURL),
							"Or omit --daemon-bin to let the provisioner auto-download the correct version",
						})))
			}

			// Commit verification: prefer explicitly-passed commit, fall back to
			// embedded spec (only for auto-download path where spec.Commit is set).
			expectedCommit := src.Commit
			if expectedCommit == "" && src.BinPath == "" {
				// Auto-download: always verify commit when spec has one.
				expectedCommit = spec.Commit
			}
			if expectedCommit != "" && vout.Commit != expectedCommit {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalArgument.New(
						"daemon binary commit mismatch: got %s, expected %s", vout.Commit, expectedCommit).
						WithProperty(models.ErrPropertyResolution, []string{
							"Ensure you are using the official release binary for version " + spec.Version,
							"Omit --daemon-commit if you built the binary locally",
						})))
			}

			// ── Ensure destination dir and copy ─────────────────────────────────
			if err := os.MkdirAll(paths.BinDir, 0o755); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to create bin directory %s", paths.BinDir)))
			}
			if err := copyBinaryFile(srcPath, dstPath); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to install daemon binary to %s", dstPath)))
			}

			logx.As().Info().
				Str("src", srcPath).
				Str("dst", dstPath).
				Str("version", vout.Version).
				Str("commit", vout.Commit).
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

// EnsureDaemonHgcAppDirStep creates the CN upgrade staging directory (and all
// parent paths, including /opt/hgcapp) if they do not already exist, then sets
// ownership to hedera:hedera with setgid 2775 so the weaver service account
// (which is a member of the hedera group) can write to it.
//
// This step is required because the systemd unit declares
// ReadWritePaths=/opt/solo /opt/hgcapp, and systemd's namespace setup fails
// with ENOENT if /opt/hgcapp does not exist on the host.
func EnsureDaemonHgcAppDirStep(upgradeDir string) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("ensure-hgcapp-dir").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			// Create the full path tree (includes /opt/hgcapp as a parent).
			if err := os.MkdirAll(upgradeDir, 0o755); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to create upgrade directory %s", upgradeDir).
						WithProperty(models.ErrPropertyResolution, []string{
							"Ensure /opt is writable by root: ls -la /opt",
							"Create manually: sudo mkdir -p " + upgradeDir,
						})))
			}

			// Resolve hedera group.
			hederaGroupName := config.HederaGroupName()
			grp, err := user.LookupGroup(hederaGroupName)
			if err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.Wrap(err, "hedera group %q not found — run: sudo solo-provisioner install first", hederaGroupName)))
			}
			gid, err := strconv.Atoi(grp.Gid)
			if err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.Wrap(err, "invalid GID for group %q: %s", hederaGroupName, grp.Gid)))
			}

			// Walk from /opt/hgcapp down to upgradeDir, applying hedera:hedera
			// ownership and setgid 2775 on every directory we just created.
			dirToChown := upgradeDir
			for {
				if err := os.Chown(dirToChown, 0, gid); err != nil {
					logx.As().Warn().Err(err).Str("path", dirToChown).Msg("failed to chown directory to hedera group")
				}
				if err := os.Chmod(dirToChown, models.DefaultStorageDirPerm); err != nil {
					logx.As().Warn().Err(err).Str("path", dirToChown).Msg("failed to chmod directory")
				}
				parent := filepath.Dir(dirToChown)
				if parent == dirToChown || parent == "/" {
					break
				}
				// Stop once we've processed /opt/hgcapp (don't touch /opt itself).
				if dirToChown == "/opt/hgcapp" {
					break
				}
				dirToChown = parent
			}

			logx.As().Info().Str("upgrade_dir", upgradeDir).Msg("CN upgrade directory ensured")
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Ensuring CN upgrade directory exists")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to ensure CN upgrade directory")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "CN upgrade directory ready")
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
					automa.WithError(errorx.IllegalState.New("daemon service unit file not found at %s; run: solo-provisioner daemon service install", sandboxPath)))
			}
			meta["unit_file"] = sandboxPath

			// 2. System symlink — must exist and point to the sandbox file
			linkTarget, err := os.Readlink(symlinkPath)
			if err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.New("daemon service symlink not found at %s; run: solo-provisioner daemon service install", symlinkPath)))
			}
			if filepath.Clean(linkTarget) != filepath.Clean(sandboxPath) {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.New("daemon service symlink %s points to %s, expected %s", symlinkPath, linkTarget, sandboxPath)))
			}
			meta["symlink"] = symlinkPath

			// 3. Service enabled
			enabled, err := pkgos.IsServiceEnabled(ctx, daemonServiceName)
			if err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to check if %s is enabled", daemonServiceName)))
			}
			meta["enabled"] = fmt.Sprintf("%v", enabled)
			if !enabled {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.New("daemon service is not enabled; run: systemctl enable %s", daemonServiceName)))
			}

			// 4. Service active
			running, err := pkgos.IsServiceRunning(ctx, daemonServiceName)
			if err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to check if %s is running", daemonServiceName)))
			}
			meta["running"] = fmt.Sprintf("%v", running)
			if !running {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.New("daemon service is not running; run: systemctl start %s", daemonServiceName)))
			}

			// 5. Daemon binary — path derived from WeaverPaths so tests and
			// future path changes don't require touching this step.
			daemonBinPath := filepath.Join(paths.BinDir, "solo-provisioner-daemon")
			if _, err := os.Stat(daemonBinPath); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.New("daemon binary not found at %s", daemonBinPath)))
			}
			meta["binary"] = daemonBinPath

			// 6. Sudoers entry
			if _, err := os.Stat(sudoersDstPath); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.New("sudoers entry not found at %s; run: solo-provisioner install", sudoersDstPath)))
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
					automa.WithError(errorx.IllegalState.Wrap(err, "daemon socket not reachable at %s; daemon may not be running", sockPath)))
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.New("daemon health check returned HTTP %d", resp.StatusCode)))
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
