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

// WriteBlockNodeDaemonConfigStep enables the block-node traffic-shaper monitor
// in daemon.yaml at install time. It loads the existing config (or starts a
// fresh one), merges in the block_node component block — enabled, the scoped
// daemon-bn.kubeconfig, the BN orbit (namespace), and monitors.traffic_shaper —
// while preserving any operator-set statusz block and the consensus_node block,
// then writes it back.
//
// The daemon binary and systemd service are installed separately by `daemon
// service install`; this step only records the enablement so the traffic-shaper
// monitor starts once the daemon runs. The write is fully reversed on rollback
// (the file is restored to its prior content, or removed if it did not exist).
func WriteBlockNodeDaemonConfigStep(paths models.WeaverPaths, orbit string) *automa.StepBuilder {
	cfgPath := paths.DaemonConfigPath
	kubeconfig := paths.DaemonBNKubeconfigPath
	if orbit == "" {
		orbit = defaultBlockNodeOrbit
	}

	return automa.NewStepBuilder().WithId(BlockNodeDaemonConfigStepId).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Enabling traffic-shaper monitor in daemon.yaml")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to enable traffic-shaper monitor in daemon.yaml")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Traffic-shaper monitor enabled in daemon.yaml")
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

			// Preserve an operator-set statusz block (local-fallback source) if
			// one already exists; this step only owns the enablement fields.
			bn := &daemon.BlockNodeComponentConfig{
				Enabled:    true,
				Kubeconfig: kubeconfig,
				Orbit:      orbit,
				Monitors:   daemon.BlockNodeMonitors{TrafficShaper: true},
			}
			if cfg.Components.BlockNode != nil {
				bn.Statusz = cfg.Components.BlockNode.Statusz
			}
			cfg.Components.BlockNode = bn

			if err := cfg.Validate(); err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errorx.Decorate(err, "daemon config is invalid after enabling block-node monitor").
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
				Msg("Enabled block-node traffic-shaper monitor in daemon.yaml")
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
