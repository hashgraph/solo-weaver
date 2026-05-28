// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	pkgos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/joomcode/errorx"
)

const (
	daemonServiceTemplatePath = "files/weaver/solo-provisioner-daemon.service"
	daemonServiceDstPath      = "/etc/systemd/system/solo-provisioner-daemon.service"
	daemonServiceName         = "solo-provisioner-daemon"
)

// InstallDaemonServiceStep installs the solo-provisioner-daemon systemd service unit file,
// runs daemon-reload, and enables the service.
func InstallDaemonServiceStep() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("install-daemon-service").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			content, err := templates.Files.ReadFile(daemonServiceTemplatePath)
			if err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to read daemon service template")))
			}

			if err := os.WriteFile(daemonServiceDstPath, content, 0o644); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to write daemon service file to %s", daemonServiceDstPath)))
			}

			if err := pkgos.DaemonReload(ctx); err != nil {
				_ = os.Remove(daemonServiceDstPath)
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "daemon-reload failed after writing daemon service file")))
			}

			if err := pkgos.EnableService(ctx, daemonServiceName); err != nil {
				_ = os.Remove(daemonServiceDstPath)
				_ = pkgos.DaemonReload(ctx)
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to enable service %s", daemonServiceName)))
			}

			if err := pkgos.RestartService(ctx, daemonServiceName); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to start service %s", daemonServiceName)))
			}

			logx.As().Info().Str("path", daemonServiceDstPath).Msg("Solo Provisioner Daemon service installed, enabled, and started")
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Installing solo-provisioner-daemon systemd service")
			return ctx, nil
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			_ = pkgos.DisableService(ctx, daemonServiceName)
			_ = pkgos.DaemonReload(ctx)
			_ = os.Remove(daemonServiceDstPath)
			return automa.StepSuccessReport(stp.Id())
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to install solo-provisioner-daemon service")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Solo Provisioner Daemon service installed and enabled")
		})
}

// RemoveDaemonServiceStep disables and removes the solo-provisioner-daemon systemd service unit file.
func RemoveDaemonServiceStep() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("remove-daemon-service").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			// Stop before disable — disabling a running unit does not stop it.
			_ = pkgos.StopService(ctx, daemonServiceName)
			_ = pkgos.DisableService(ctx, daemonServiceName)

			if err := os.Remove(daemonServiceDstPath); err != nil && !os.IsNotExist(err) {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to remove daemon service file %s", daemonServiceDstPath)))
			}

			if err := pkgos.DaemonReload(ctx); err != nil {
				logx.As().Warn().Err(err).Msg("daemon-reload failed after removing daemon service file")
			}

			logx.As().Info().Str("path", daemonServiceDstPath).Msg("Solo Provisioner Daemon service removed")
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
//  1. Unit file exists at /etc/systemd/system/solo-provisioner-daemon.service
//  2. Service is enabled (systemctl is-enabled)
//  3. Service is active/running (systemctl is-active)
//  4. Daemon binary exists at /opt/solo/weaver/bin/solo-provisioner-daemon
//  5. Sudoers entry exists at /etc/sudoers.d/solo-provisioner
//  6. Unix socket responds to GET /health → {"status":"ok"}
func CheckDaemonServiceStep(sockPath string) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("check-daemon-service").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			// 1. Unit file
			if _, err := os.Stat(daemonServiceDstPath); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.New("daemon service unit file not found at %s; run: solo-provisioner daemon service install", daemonServiceDstPath)))
			}
			meta["unit_file"] = daemonServiceDstPath

			// 2. Service enabled
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

			// 3. Service active
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

			// 4. Daemon binary
			daemonBinPath := "/opt/solo/weaver/bin/solo-provisioner-daemon"
			if _, err := os.Stat(daemonBinPath); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.New("daemon binary not found at %s", daemonBinPath)))
			}
			meta["binary"] = daemonBinPath

			// 5. Sudoers entry
			if _, err := os.Stat(sudoersDstPath); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.New("sudoers entry not found at %s; run: solo-provisioner install", sudoersDstPath)))
			}
			meta["sudoers"] = sudoersDstPath

			// 6. Unix socket health — proxy is explicitly disabled so that
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
