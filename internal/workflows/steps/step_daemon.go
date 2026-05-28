// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
	pkgos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/joomcode/errorx"
)

const (
	daemonServiceTemplatePath = "files/weaver/solo-provisioner-daemon.service"
	daemonServiceName         = "solo-provisioner-daemon"
)

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
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if err := pkgos.DaemonReload(ctx); err != nil {
				removeDaemonServiceFiles(sandboxPath, symlinkPath)
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "daemon-reload failed after writing daemon service file")))
			}

			if err := pkgos.EnableService(ctx, daemonServiceName); err != nil {
				removeDaemonServiceFiles(sandboxPath, symlinkPath)
				_ = pkgos.DaemonReload(ctx)
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to enable service %s", daemonServiceName)))
			}

			if err := pkgos.RestartService(ctx, daemonServiceName); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to start service %s", daemonServiceName)))
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
