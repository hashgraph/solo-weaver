// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/daemon/consensus"
	"github.com/hashgraph/solo-weaver/pkg/eventlog"
	"github.com/hashgraph/solo-weaver/pkg/filepruner"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"golang.org/x/sync/errgroup"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	upgradeEventLayout = "20060102T150405Z"
	upgradeEventMaxAge = 365 * 24 * time.Hour
	upgradeEventKeep   = 50
	upgradeEventGlob   = "consensus-upgrade-*.jsonl"

	kubeProbeInterval    = 2 * time.Second
	kubeProbeRESTTimeout = 30 * time.Second // per-attempt REST timeout; matches upgrade_monitor.go and criteria.go

	// networkUpgradeExecuteGroup and resource are the exact RBAC verbs the daemon
	// needs to watch NetworkUpgradeExecute CRs. Probing these ensures the
	// daemon's ServiceAccount has the required permissions before signalling READY.
	networkUpgradeExecuteGroup    = "hedera.com"
	networkUpgradeExecuteResource = "networkupgradeexecutes"
)

// Daemon is the controller for solo-provisioner-daemon. It composes the
// sub-systems and owns their lifecycle via Run.
//
// Goroutine map:
//   - Socket server       — always on; HTTP control plane on daemon.sock
//   - UpgradeMonitor      — always on; K8s watch for ReadyForProvisionerDaemon (#519)
//   - MigrationMonitor    — dispatch loop always on; per-activation goroutine on demand (#520)
type Daemon struct {
	paths            models.WeaverPaths
	cfg              DaemonConfig
	server           *Server
	upgradeMonitor   *consensus.UpgradeMonitor
	migrationMonitor *consensus.MigrationMonitor
	migrateLogger    *eventlog.EventLogger
}

// New constructs a Daemon from WeaverPaths. It reads daemon.yaml from
// paths.DaemonConfigPath and fails fast if the config is missing or invalid —
// the daemon must not start without a valid node_id, kubeconfig, and orbit.
// Use NewFromConfig when the caller has already resolved the config (e.g. with
// CLI flag overrides applied).
func New(paths models.WeaverPaths) (*Daemon, error) {
	pruneUpgradeEventLogs(paths.HomeDir, paths.DaemonConsensusUpgradeEventsDir)

	cfg, err := LoadDaemonConfig(paths.DaemonConfigPath)
	if err != nil {
		return nil, err
	}
	return NewFromConfig(paths, cfg)
}

// NewFromConfig constructs a Daemon from a pre-resolved DaemonConfig. The
// caller is responsible for loading and validating cfg (e.g. via LoadDaemonConfig
// followed by applying CLI flag overrides). cfg must pass Validate() before
// this function is called.
func NewFromConfig(paths models.WeaverPaths, cfg DaemonConfig) (*Daemon, error) {
	cn := cfg.Components.ConsensusNode
	um, err := consensus.NewUpgradeMonitor(consensus.UpgradeMonitorConfig{
		NodeID:         cn.NodeID,
		KubeconfigPath: cn.Kubeconfig,
		Namespace:      cn.Orbit,
	})
	if err != nil {
		return nil, err
	}

	var migrateLogger *eventlog.EventLogger
	if ml, err := eventlog.NewAppend(paths.DaemonConsensusMigrateEventsDir, "consensus-migrate-events.jsonl"); err != nil {
		logx.As().Warn().Err(err).
			Str("reason", "MigrateLoggerInitFailed").
			Str("dir", paths.DaemonConsensusMigrateEventsDir).
			Msg("Failed to open migrate event logger — migration events will not be persisted")
	} else {
		migrateLogger = ml
	}

	mm := consensus.NewMigrationMonitorWith(
		cn.NodeID,
		migrateLogger,
		&consensus.NoopDecommissioner{},
		consensus.MigrationMonitorConfig{},
		paths.DaemonConsensusMigrateEventsDir,
	).WithCriteria(
		consensus.SoakDuration{}, // zero Period → defaults to DefaultSoakPeriod (48h)
		consensus.UploaderBacklogCleared{},
		&consensus.NoPodRestarts{
			KubeconfigPath: cn.Kubeconfig,
			Namespace:      cn.Orbit,
			PodLabelSelector: fmt.Sprintf(
				"operator.solo.hedera.com/orbit=%s,operator.solo.hedera.com/node-id=%s",
				cn.Orbit, cn.NodeID,
			),
		},
		consensus.ConsensusParticipationNominal{},
	)

	d := &Daemon{
		paths:            paths,
		cfg:              cfg,
		upgradeMonitor:   um,
		migrationMonitor: mm,
		migrateLogger:    migrateLogger,
	}
	d.server = NewServer(paths.DaemonSockPath, mm, ServerConfig{}) // zero value → all defaults
	return d, nil
}

// pruneUpgradeEventLogs applies the retention policy to per-operation upgrade
// JSONL files on daemon startup. A failure is logged as a warning and does not
// block startup — a retained extra file is less harmful than a failed daemon.
// homeDir is used to verify dir is within the weaver home tree, preventing
// accidental pruning of arbitrary filesystem paths.
func pruneUpgradeEventLogs(homeDir, dir string) {
	if dir == "" || !filepath.IsAbs(dir) {
		logx.As().Warn().
			Str("reason", "UpgradeEventLogPruneSkipped").
			Str("dir", dir).
			Msg("Skipping upgrade event log pruning — dir is empty or relative")
		return
	}
	if _, err := sanity.ValidatePathWithinBase(homeDir, dir); err != nil {
		logx.As().Warn().Err(err).
			Str("reason", "UpgradeEventLogPruneSkipped").
			Str("dir", dir).
			Str("home", homeDir).
			Msg("Skipping upgrade event log pruning — dir is outside weaver home")
		return
	}
	p := filepruner.New(filepruner.FilenameTimestampStrategy{
		Layout: upgradeEventLayout,
		MaxAge: upgradeEventMaxAge,
	})
	if err := p.Prune(dir, upgradeEventGlob, upgradeEventKeep); err != nil {
		logx.As().Warn().Err(err).
			Str("reason", "UpgradeEventLogPruneFailed").
			Str("dir", dir).
			Msg("Failed to prune upgrade event logs on startup — continuing")
	}
}

// probeKubeRBAC issues SelfSubjectAccessReview calls to verify that the daemon's
// ServiceAccount has the `list` and `watch` verbs on networkupgradeexecutes in
// the given namespace. Returns nil only when both verbs are allowed.
func probeKubeRBAC(ctx context.Context, kubeconfigPath, namespace string) error {
	restCfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return fmt.Errorf("build kubeconfig: %w", err)
	}
	// Cap each REST call so a hung API server doesn't block a single probe
	// attempt indefinitely. Matches the timeout used in upgrade_monitor.go and
	// criteria.go.
	restCfg.Timeout = kubeProbeRESTTimeout
	client, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("build kube client: %w", err)
	}

	for _, verb := range []string{"list", "watch"} {
		review := &authorizationv1.SelfSubjectAccessReview{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace: namespace,
					Verb:      verb,
					Group:     networkUpgradeExecuteGroup,
					Resource:  networkUpgradeExecuteResource,
				},
			},
		}
		result, err := client.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, review, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("SelfSubjectAccessReview(%s): %w", verb, err)
		}
		if !result.Status.Allowed {
			return fmt.Errorf("RBAC denied: verb=%s resource=%s.%s namespace=%s", verb, networkUpgradeExecuteResource, networkUpgradeExecuteGroup, namespace)
		}
	}
	return nil
}

// probeKubeRBACWithRetry retries probeKubeRBAC every kubeProbeInterval until
// it succeeds or ctx is cancelled. Returns nil on success, ctx.Err() if the
// context is cancelled before the probe succeeds.
//
// There is intentionally no internal timeout: if RBAC is misconfigured the
// probe will never succeed, and the caller must NOT send READY=1. Systemd's
// TimeoutStartSec (default 90 s) will cancel the context and mark the service
// as failed — which is the correct loud startup failure for a broken config.
func probeKubeRBACWithRetry(ctx context.Context, kubeconfigPath, namespace string) error {
	attempt := 0
	for {
		attempt++
		if err := probeKubeRBAC(ctx, kubeconfigPath, namespace); err == nil {
			logx.As().Info().
				Str("reason", "KubeRBACProbeSuccess").
				Int("attempt", attempt).
				Msg("Kubernetes RBAC probe succeeded — daemon has required permissions")
			return nil
		} else {
			logx.As().Warn().Err(err).
				Str("reason", "KubeRBACProbeFailed").
				Int("attempt", attempt).
				Msg("Kubernetes RBAC probe failed — retrying")
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(kubeProbeInterval):
		}
	}
}

// Run starts all sub-systems and blocks until ctx is cancelled or a critical
// sub-system exits with an error. It is the single entry point called from
// cmd/daemon/main.go.
//
// A top-level recover logs any unhandled panic with a structured message before
// calling os.Exit(2), ensuring the reason is captured in the daemon log before
// systemd restarts the process. Sub-system panics are caught earlier (runWatch,
// handleExecute) and converted to errors so this path is a last resort only.
func (d *Daemon) Run(ctx context.Context) error {
	// Signal systemd that we are stopping on any return path (ctx cancel or error).
	defer func() { _ = sdNotify(sdStopping) }()

	defer func() {
		if r := recover(); r != nil {
			logx.As().Error().
				Str("reason", "DaemonPanic").
				Interface("panic", r).
				Msg("Unhandled panic in daemon — exiting for systemd restart")
			// sdStopping must be sent explicitly here because os.Exit(2) bypasses
			// all pending defers. The sdNotify(sdStopping) defer was registered
			// first (line 237), so in LIFO order it would run last — after this
			// panic recovery returns. But os.Exit never returns, so that defer
			// is never reached.
			_ = sdNotify(sdStopping)
			os.Exit(2)
		}
	}()
	if d.migrateLogger != nil {
		defer func() {
			if err := d.migrateLogger.Close(); err != nil {
				logx.As().Warn().Err(err).Str("reason", "MigrateLoggerCloseFailed").Msg("Failed to close migrate event logger")
			}
		}()
	}

	// Preflight: consensus-node kubeconfig must exist and be parseable before we
	// start anything. A missing or invalid file is a configuration error that will
	// never self-heal, so we fail fast here rather than burning systemd's
	// TimeoutStartSec on pointless retries.
	if _, err := clientcmd.BuildConfigFromFlags("", d.cfg.Components.ConsensusNode.Kubeconfig); err != nil {
		return fmt.Errorf("kubeconfig preflight failed — daemon cannot start: %w", err)
	}

	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error { return d.server.Start(ctx) })
	if d.upgradeMonitor != nil {
		eg.Go(func() error { return d.upgradeMonitor.Run(ctx) })
	}
	eg.Go(func() error { return d.migrationMonitor.Run(ctx) })

	// Probe K8s RBAC concurrently so the server starts immediately without
	// blocking. The probe only retries transient API connectivity failures —
	// the kubeconfig is already validated above. READY=1 is sent only on
	// success; if RBAC is misconfigured the probe retries until systemd cancels
	// the context via TimeoutStartSec, marking the service failed.
	go func() {
		if err := probeKubeRBACWithRetry(ctx, d.cfg.Components.ConsensusNode.Kubeconfig, d.cfg.Components.ConsensusNode.Orbit); err != nil {
			logx.As().Error().Err(err).
				Str("reason", "KubeRBACProbeAborted").
				Msg("RBAC probe aborted — not sending READY=1; systemd will time out and mark service failed")
			return
		}
		if err := sdNotify(sdReady); err != nil {
			logx.As().Warn().Err(err).Str("reason", "SdNotifyReadyFailed").Msg("Failed to send READY=1 to systemd")
		}
	}()

	return eg.Wait()
}
