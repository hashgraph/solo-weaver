// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"os"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/daemon"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// This file holds the block-node-specific daemon wiring step. The generic daemon
// lifecycle steps (binary/service install, config removal, start/stop/check) live
// in step_daemon.go and are shared across the daemon's components; block-node and
// (future) consensus-node component-config steps are kept in their own files.

// BlockNodeDaemonConfigStepId is the step ID for WriteBlockNodeDaemonConfigStep.
const BlockNodeDaemonConfigStepId = "write-block-node-daemon-config"

// defaultBlockNodeOrbit mirrors deps.BLOCK_NODE_NAMESPACE. It is used only as a
// last-resort default so an enabled block_node config still validates when the
// resolved BN namespace is somehow empty.
const defaultBlockNodeOrbit = "block-node"

// State keys used by WriteBlockNodeDaemonConfigStep to reverse its write on
// rollback: whether daemon.yaml existed before the step, and its prior content.
const (
	daemonConfigPriorExistedKey = "daemon-config-prior-existed"
	daemonConfigPriorContentKey = "daemon-config-prior-content"
)

// WriteBlockNodeDaemonConfigStep records the block-node component's enablement
// in daemon.yaml. It loads the existing config (or starts a fresh one), merges in
// the block_node component block — enabled/monitors.traffic_shaper set to the
// requested state, the scoped daemon-bn.kubeconfig, and the BN orbit (namespace)
// — merges the operator-owned statusz block (see below), preserves the
// consensus_node block, then writes it back.
//
// enabled drives both Components.BlockNode.Enabled and Monitors.TrafficShaper:
//   - true  (install / reconfigure enable): the traffic-shaper monitor runs once
//     the daemon is up.
//   - false (reconfigure disable): the block-node component and its traffic-shaper
//     monitor are turned off WITHOUT uninstalling the daemon binary/service, so a
//     co-located component (e.g. consensus-node monitoring) that shares the same
//     daemon keeps running.
//
// statusz carries the operator-supplied overrides (from `block node install`/
// `reconfigure`'s --statusz-base-url / --statusz-poll-interval). The
// provisioner-owned fields above always win; the statusz block is merged
// per-field: starting from any block already on disk, each non-empty override
// overlays its field, and an empty override preserves the existing on-disk value.
// So a bare reconfigure/upgrade (a zero-value statusz) never clobbers a
// hand-edited statusz block, while an explicit flag updates just that field.
//
// The daemon binary and systemd service are installed separately by `daemon
// service install`; this step only records the enablement. The write is fully
// reversed on rollback (the file is restored to its prior content, or removed if
// it did not exist).
func WriteBlockNodeDaemonConfigStep(paths models.WeaverPaths, orbit string, statusz daemon.StatuszConfig, enabled bool) *automa.StepBuilder {
	cfgPath := paths.DaemonConfigPath
	kubeconfig := paths.DaemonBNKubeconfigPath
	if orbit == "" {
		orbit = defaultBlockNodeOrbit
	}

	startMsg := "Enabling traffic-shaper monitor in daemon.yaml"
	failMsg := "Failed to enable traffic-shaper monitor in daemon.yaml"
	doneMsg := "Traffic-shaper monitor enabled in daemon.yaml"
	if !enabled {
		startMsg = "Disabling traffic-shaper monitor in daemon.yaml"
		failMsg = "Failed to disable traffic-shaper monitor in daemon.yaml"
		doneMsg = "Traffic-shaper monitor disabled in daemon.yaml"
	}

	return automa.NewStepBuilder().WithId(BlockNodeDaemonConfigStepId).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, startMsg)
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, failMsg)
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, doneMsg)
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			prior, err := os.ReadFile(cfgPath)
			existed := true
			if os.IsNotExist(err) {
				existed = false
				prior = nil
			} else if err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errorx.ExternalError.Wrap(err, "failed to read daemon config at %s", cfgPath).
						WithProperty(models.ErrPropertyResolution, []string{
							"Check permissions on " + cfgPath,
						})))
			}

			var cfg daemon.DaemonConfig
			if existed {
				// Parse the same bytes captured for rollback (not a second read
				// of the file), so the merged config and the rollback snapshot
				// can never diverge if the file changes mid-install.
				loaded, lerr := daemon.ParseDaemonConfig(prior, cfgPath)
				if lerr != nil {
					return automa.FailureReport(stp, automa.WithError(
						errorx.Decorate(lerr, "existing daemon config at %s is invalid", cfgPath).
							WithProperty(models.ErrPropertyResolution, []string{
								"Fix or remove " + cfgPath + " and re-run",
							})))
				}
				cfg = loaded
			}

			// Provisioner-owned fields (enabled, kubeconfig, orbit, monitors) always
			// win. The operator-owned statusz block is merged per-field: start from
			// any block already on disk, then overlay each supplied override
			// (--statusz-base-url / --statusz-poll-interval). An unset override
			// preserves the existing on-disk value, so a bare reconfigure/upgrade
			// never clobbers a hand-edited statusz block.
			bn := &daemon.BlockNodeComponentConfig{
				Enabled:    enabled,
				Kubeconfig: kubeconfig,
				Orbit:      orbit,
				Monitors:   daemon.BlockNodeMonitors{TrafficShaper: enabled},
			}
			if cfg.Components.BlockNode != nil {
				bn.Statusz = cfg.Components.BlockNode.Statusz
			}
			if statusz.BaseURL != "" || statusz.PollInterval != "" {
				merged := daemon.StatuszConfig{}
				if bn.Statusz != nil {
					merged = *bn.Statusz
				}
				if statusz.BaseURL != "" {
					merged.BaseURL = statusz.BaseURL
				}
				if statusz.PollInterval != "" {
					merged.PollInterval = statusz.PollInterval
				}
				bn.Statusz = &merged
			}
			cfg.Components.BlockNode = bn

			if err := cfg.Validate(); err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errorx.Decorate(err, "daemon config is invalid after updating block-node monitor").
						WithProperty(models.ErrPropertyResolution, []string{
							"Inspect " + cfgPath,
						})))
			}
			if err := daemon.WriteDaemonConfig(cfgPath, cfg); err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errorx.Decorate(err, "failed to write daemon config").
						WithProperty(models.ErrPropertyResolution, []string{
							"Check permissions on " + cfgPath,
						})))
			}

			stp.State().Local().Set(daemonConfigPriorExistedKey, existed)
			stp.State().Local().Set(daemonConfigPriorContentKey, string(prior))
			logx.As().Info().
				Str("path", cfgPath).
				Str("orbit", orbit).
				Bool("enabled", enabled).
				Msg("Updated block-node traffic-shaper monitor in daemon.yaml")
			return automa.SuccessReport(stp)
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			existed, _ := stp.State().Local().Bool(daemonConfigPriorExistedKey)
			if !existed {
				if err := os.Remove(cfgPath); err != nil && !os.IsNotExist(err) {
					return automa.FailureReport(stp, automa.WithError(
						errorx.ExternalError.Wrap(err, "failed to remove daemon config at %s on rollback", cfgPath)))
				}
				return automa.SuccessReport(stp)
			}
			prior, _ := stp.State().Local().String(daemonConfigPriorContentKey)
			if err := os.WriteFile(cfgPath, []byte(prior), 0o644); err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errorx.ExternalError.Wrap(err, "failed to restore daemon config at %s on rollback", cfgPath)))
			}
			return automa.SuccessReport(stp)
		})
}
