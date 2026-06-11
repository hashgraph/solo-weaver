// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"context"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/daemon/probes"
	"github.com/hashgraph/solo-weaver/pkg/filepruner"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

var networkUpgradeExecuteGVR = schema.GroupVersionResource{
	Group:    "hedera.com",
	Version:  "v1alpha1",
	Resource: "networkupgradeexecutes",
}

const (
	// NetworkUpgradeExecute status.phase values, as defined in the CRD.
	executePhasePending                   = "Pending"
	executePhaseReadyForProvisionerDaemon = "ReadyForProvisionerDaemon"
	executePhasePendingInfraUpgrade       = "PendingInfraUpgrade"
	executePhasePendingNodeUpgrade        = "PendingNodeUpgrade"
	executePhaseSucceeded                 = "Succeeded"
	executePhaseFailed                    = "Failed"

	backoffInitial = 2 * time.Second
	backoffMax     = 5 * time.Minute
	backoffFactor  = 2.0

	// watchTimeoutSeconds is the server-side watch timeout passed to the K8s API.
	// The server closes the channel cleanly after this window; the Run loop reconnects
	// after backoffInitial. Any premature close (network blip, proxy idle timeout) is
	// also subject to the same backoffInitial delay rather than an immediate hot-loop.
	watchTimeoutSeconds = int64(5 * 60)

	// Upgrade event log retention policy — applied at Run() start and after each
	// handleExecute so that both startup pruning and long-running daemon pruning
	// are covered without an extra daemon-level call.
	upgradeEventLayout = "20060102T150405Z"
	upgradeEventMaxAge = 365 * 24 * time.Hour
	upgradeEventKeep   = 50
	upgradeEventGlob   = "consensus-upgrade-*.jsonl"
)

// networkUpgradeExecuteGroup and networkUpgradeExecuteResource are the RBAC
// coordinates the daemon needs to watch NetworkUpgradeExecute CRs. Used by
// RequiredProbe to build the KubeRBACProbe for this monitor.
const (
	networkUpgradeExecuteGroup    = "hedera.com"
	networkUpgradeExecuteResource = "networkupgradeexecutes"
)

// UpgradeMonitorConfig holds configuration for the UpgradeMonitor.
type UpgradeMonitorConfig struct {
	// KubeconfigPath is the path to the daemon's scoped kubeconfig. Built once
	// from daemon.yaml at startup; systemd restarts the daemon if the credential
	// changes after a cluster rebuild.
	KubeconfigPath string

	// Namespace is the orbit namespace to watch for NetworkUpgradeExecute CRs.
	// One daemon manages one CN, so one namespace is sufficient.
	Namespace string

	// NodeID is the Hedera node identifier for the consensus node managed by
	// this daemon (e.g. "0.0.3"). Populated as nodeId in all JSONL event log
	// entries emitted by handleExecute.
	NodeID string

	// UpgradeEventsDir is the directory where per-operation consensus-upgrade-*.jsonl
	// files are written by handleExecute. The monitor prunes this directory at the
	// start of each Run() invocation (covers both startup and post-crash restarts).
	// Empty string disables pruning.
	UpgradeEventsDir string

	// HomeDir is the weaver home directory used as a safety base for path
	// validation during pruning. Pruning is skipped when UpgradeEventsDir falls
	// outside this tree.
	HomeDir string

	// UpgradeDir is the active upgrade staging directory (the `current`
	// subdirectory under the CN's upgrade path). RequiredProbe verifies
	// ownership and write-access on this directory and its parent before the
	// monitor is allowed to start. Defaults to the CN standard path when empty.
	//
	// Example: /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current
	UpgradeDir string
}

// UpgradeMonitor watches the Kubernetes API for NetworkUpgradeExecute CRs
// whose status.phase transitions to ReadyForProvisionerDaemon and triggers the
// execute-phase workflow. It is self-healing: transient errors and auth failures
// are retried with exponential backoff so the daemon recovers without a restart.
type UpgradeMonitor struct {
	cfg    UpgradeMonitorConfig
	client dynamic.Interface

	mu sync.Mutex
	// activeOpID is non-empty while handleExecute is running; guards the single
	// execution slot. No two active CRs with the same operationId can execute
	// concurrently — historical reuse of an operationId is fine provided the prior
	// CR is no longer in a terminal phase visible to the cluster List.
	activeOpID string
	// completedOpIDs holds operationIds that completed successfully in this process
	// lifetime. It guards the patch round-trip window: the gap between when the
	// execute goroutine finishes and when the CR's status.phase is observed as
	// Succeeded/Failed by the watch loop. Seeded at every Run() entry from the
	// cluster List so surviving restarts does not require disk persistence — the
	// CR's phase is the durable source of truth.
	completedOpIDs map[string]struct{}

	// onExecute is called synchronously at the start of each handleExecute invocation.
	// Nil in production; set in tests to observe invocations without sleeping.
	onExecute func(operationID string)
}

// NewUpgradeMonitor constructs an UpgradeMonitor and builds the Kubernetes
// dynamic client from the kubeconfig at cfg.KubeconfigPath.
func NewUpgradeMonitor(cfg UpgradeMonitorConfig) (*UpgradeMonitor, error) {
	client, err := buildDynamicClient(cfg.KubeconfigPath)
	if err != nil {
		return nil, ErrK8sClient.Wrap(err, "upgrade monitor: build k8s client")
	}
	return &UpgradeMonitor{cfg: cfg, client: client, completedOpIDs: make(map[string]struct{})}, nil
}

// NewUpgradeMonitorWithClient constructs an UpgradeMonitor with an injected
// client — used in unit tests to avoid a real kubeconfig on disk.
func NewUpgradeMonitorWithClient(cfg UpgradeMonitorConfig, client dynamic.Interface) *UpgradeMonitor {
	return &UpgradeMonitor{cfg: cfg, client: client, completedOpIDs: make(map[string]struct{})}
}

// Name implements daemon.MonitorRunner.
func (um *UpgradeMonitor) Name() string { return "upgrade-monitor" }

// RequiredProbe implements daemon.ProbableMonitor. It returns a composite probe
// that verifies the disk prerequisites that must be correct before the first
// upgrade CR arrives. Catching these at startup avoids silent failures mid-automation
// when human intervention is no longer practical.
//
// Kube RBAC / CRD availability is intentionally excluded: the watch loop already
// retries those on its own backoff schedule, so a missing CRD at startup is not a
// problem — it resolves automatically once the CRD is installed.
//
//  1. Parent dir ownership — the upgrade staging root (e.g. .../data/upgrade) is
//     owned by hedera:hedera with at least rwxr-xr-x (0755).
//  2. Current dir ownership — the active staging subdir (UpgradeDir) is owned by
//     hedera:hedera with at least rwxrwxr-x (0775).
//  3. Current dir write access — the running daemon process can actually write to
//     UpgradeDir, proving `usermod -aG hedera weaver` was applied and mount flags,
//     ACLs, and SELinux/AppArmor policies all allow writes.
func (um *UpgradeMonitor) RequiredProbe() probes.Probe {
	upgradeDir := um.cfg.UpgradeDir         // e.g. .../data/upgrade/current
	upgradeRoot := filepath.Dir(upgradeDir) // e.g. .../data/upgrade

	return probes.NewCompositeProbe(
		// 1. Parent dir: hedera installer must have created this with 0755.
		&probes.DiskOwnershipProbe{
			Path:       upgradeRoot,
			User:       "hedera",
			Group:      "hedera",
			Permission: 0o755,
		},
		// 2. Current dir ownership: cluster install must have run chmod g+rwx.
		&probes.DiskOwnershipProbe{
			Path:       upgradeDir,
			User:       "hedera",
			Group:      "hedera",
			Permission: 0o775,
		},
		// 3. Write test: proves effective write access under real process credentials.
		&probes.DiskWriteTestProbe{
			Dir: upgradeDir,
		},
	)
}

// pruneUpgradeEventLogs removes stale per-operation upgrade JSONL files from
// cfg.UpgradeEventsDir. Called at the start of each Run() invocation so
// pruning happens both at daemon startup and after any supervised restart.
// A failure is logged as a warning and does not block the monitor from starting.
func (um *UpgradeMonitor) pruneUpgradeEventLogs() {
	dir := um.cfg.UpgradeEventsDir
	if dir == "" {
		return
	}
	if um.cfg.HomeDir != "" {
		if _, err := sanity.ValidatePathWithinBase(um.cfg.HomeDir, dir); err != nil {
			logx.As().Warn().Err(err).
				Str("reason", "UpgradeEventLogPruneSkipped").
				Str("dir", dir).
				Str("home", um.cfg.HomeDir).
				Msg("Skipping upgrade event log pruning — dir is outside weaver home")
			return
		}
	}
	p := filepruner.New(filepruner.FilenameTimestampStrategy{
		Layout: upgradeEventLayout,
		MaxAge: upgradeEventMaxAge,
	})
	if err := p.Prune(dir, upgradeEventGlob, upgradeEventKeep); err != nil {
		logx.As().Warn().Err(err).
			Str("reason", "UpgradeEventLogPruneFailed").
			Str("dir", dir).
			Msg("Failed to prune upgrade event logs — continuing")
	}
}

// Run blocks until ctx is cancelled. On every iteration it lists all
// NetworkUpgradeExecute CRs to seed completedOpIDs and dispatch any CRs
// already at ReadyForProvisionerDaemon (recovery after a crash or restart),
// then watches from the List's ResourceVersion so no events are missed between
// the two calls. Clean watch expiry reconnects after backoffInitial; real
// errors retry with exponential backoff; auth errors additionally rebuild the
// dynamic client from the kubeconfig on disk before retrying.
func (um *UpgradeMonitor) Run(ctx context.Context) error {
	// Prune stale upgrade event logs on every Run() entry — covers both the
	// initial startup and any subsequent supervised restarts.
	um.pruneUpgradeEventLogs()

	logx.As().Info().
		Str("reason", "UpgradeMonitorStarted").
		Str("namespace", um.cfg.Namespace).
		Msg("Upgrade monitor started")

	backoff := backoffInitial

	for {
		logx.As().Debug().Str("reason", "UpgradeMonitorWatchAttempt").Msg("Starting watch attempt")

		listRV, listErr := um.listAndSeed(ctx)
		if ctx.Err() != nil {
			logx.As().Info().Str("reason", "UpgradeMonitorStopped").Msg("Upgrade monitor stopped")
			return nil
		}
		if listErr != nil {
			logx.As().Warn().Err(listErr).
				Str("reason", "UpgradeMonitorListError").
				Dur("retry_in", backoff).
				Msg("List error — retrying")
			select {
			case <-ctx.Done():
				logx.As().Info().Str("reason", "UpgradeMonitorStopped").Msg("Upgrade monitor stopped")
				return nil
			case <-time.After(backoff):
			}
			backoff = minDuration(time.Duration(float64(backoff)*backoffFactor), backoffMax)
			continue
		}

		err := um.runWatch(ctx, listRV)

		if ctx.Err() != nil {
			logx.As().Info().Str("reason", "UpgradeMonitorStopped").Msg("Upgrade monitor stopped")
			return nil
		}

		if err == nil {
			// Clean channel close (server-side expiry or premature proxy/network drop).
			// Always sleep backoffInitial before reconnecting to prevent hot-looping
			// if the server or an intermediary proxy silently drops the connection.
			// Trade-off: if a proxy has an idle timeout shorter than watchTimeoutSeconds
			// (300s), backoff never grows — reconnects happen every backoffInitial (2s).
			// This is noisy but harmless; the correct fix is to configure the proxy's
			// idle timeout above 300s, not to grow the backoff on clean closes.
			logx.As().Debug().
				Str("reason", "UpgradeMonitorWatchClosed").
				Dur("retry_in", backoffInitial).
				Msg("Watch channel closed — reconnecting")
			backoff = backoffInitial
			select {
			case <-ctx.Done():
				logx.As().Info().Str("reason", "UpgradeMonitorStopped").Msg("Upgrade monitor stopped")
				return nil
			case <-time.After(backoffInitial):
			}
			continue
		}

		if isAuthError(err) {
			logx.As().Warn().Err(err).
				Str("reason", "UpgradeMonitorAuthError").
				Str("kubeconfig", um.cfg.KubeconfigPath).
				Dur("retry_in", backoff).
				Msg("Auth error watching NetworkUpgradeExecute — re-reading kubeconfig")

			if client, rebuildErr := buildDynamicClient(um.cfg.KubeconfigPath); rebuildErr != nil {
				// Kubeconfig is missing or unreadable. The daemon retries with the
				// stale credential; backoff grows to backoffMax (5 min) and the daemon
				// remains non-functional for watch indefinitely — systemd does NOT
				// restart it because the process is still running. Recovery requires
				// the operator to restore the kubeconfig at KubeconfigPath (or run
				// `provisioner daemon install` to rotate credentials) so that the next
				// auth-error retry successfully rebuilds the client.
				logx.As().Error().Err(rebuildErr).
					Str("reason", "UpgradeMonitorKubeconfigError").
					Msg("Failed to re-read kubeconfig after auth error — will retry with existing client")
			} else {
				um.client = client
				logx.As().Info().
					Str("reason", "UpgradeMonitorClientRebuilt").
					Msg("K8s client rebuilt with refreshed kubeconfig")
			}
		} else {
			logx.As().Warn().Err(err).
				Str("reason", "UpgradeMonitorWatchError").
				Dur("retry_in", backoff).
				Msg("Watch error — retrying")
		}

		select {
		case <-ctx.Done():
			logx.As().Info().Str("reason", "UpgradeMonitorStopped").Msg("Upgrade monitor stopped")
			return nil
		case <-time.After(backoff):
		}

		backoff = minDuration(time.Duration(float64(backoff)*backoffFactor), backoffMax)
	}
}

// listAndSeed lists all NetworkUpgradeExecute CRs in the namespace, seeds
// completedOpIDs from terminal-phase CRs, and dispatches any CR already at
// ReadyForProvisionerDaemon so upgrades that arrived while the daemon was
// offline are recovered without waiting for a new watch event. Returns the
// List's ResourceVersion so the caller can start a gapless watch from that
// point.
func (um *UpgradeMonitor) listAndSeed(ctx context.Context) (string, error) {
	resource := um.client.Resource(networkUpgradeExecuteGVR).Namespace(um.cfg.Namespace)
	list, err := resource.List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", ErrWatchFailed.Wrap(err, "list NetworkUpgradeExecute")
	}

	um.mu.Lock()
	for i := range list.Items {
		cr := &list.Items[i]
		phase, _, _ := unstructured.NestedString(cr.Object, "status", "phase")
		opID, _, _ := unstructured.NestedString(cr.Object, "spec", "operationId")
		if opID == "" {
			continue
		}
		switch phase {
		case executePhaseSucceeded, executePhaseFailed:
			um.completedOpIDs[opID] = struct{}{}
		}
	}
	um.mu.Unlock()

	// Collect and sort ReadyForProvisionerDaemon CRs oldest-first so that when
	// multiple are pending (orchestrator bug, but handled defensively) the
	// longest-waiting operation always acquires the single execution slot first.
	// Newer ones are rejected by UpgradeMonitorBusy and retried on the next
	// reconnect — still in chronological order.
	var pending []*unstructured.Unstructured
	for i := range list.Items {
		cr := &list.Items[i]
		phase, _, _ := unstructured.NestedString(cr.Object, "status", "phase")
		if phase == executePhaseReadyForProvisionerDaemon {
			pending = append(pending, cr)
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		ti := pending[i].GetCreationTimestamp()
		tj := pending[j].GetCreationTimestamp()
		return ti.Before(&tj)
	})
	for _, cr := range pending {
		um.handleEvent(ctx, cr)
	}

	return list.GetResourceVersion(), nil
}

// runWatch performs a single watch cycle starting from resourceVersion (the RV
// returned by listAndSeed). This guarantees no gap between the List snapshot
// and the watch stream. Returns nil when the watch channel closes cleanly
// (server-side expiry or context cancellation), or an error the caller should
// back off from and retry. Any panic is caught and converted to an error so
// Run() applies backoff rather than crashing the daemon.
func (um *UpgradeMonitor) runWatch(ctx context.Context, resourceVersion string) (retErr error) {
	defer func() {
		if r := recover(); r != nil {
			logx.As().Error().
				Str("reason", "UpgradeMonitorWatchPanic").
				Interface("panic", r).
				Msg("Panic in watch loop — recovering, will retry with backoff")
			retErr = ErrWatchFailed.New("recovered panic: %v", r)
		}
	}()
	resource := um.client.Resource(networkUpgradeExecuteGVR).Namespace(um.cfg.Namespace)

	logx.As().Debug().
		Str("reason", "UpgradeMonitorWatchCalling").
		Str("namespace", um.cfg.Namespace).
		Str("kubeconfig", um.cfg.KubeconfigPath).
		Msg("Calling Watch on NetworkUpgradeExecute CRs")

	timeoutSec := watchTimeoutSeconds
	watcher, err := resource.Watch(ctx, metav1.ListOptions{
		// Start from the ResourceVersion of the preceding List call so there is no
		// gap between the snapshot and the watch stream. Recovery of CRs already at
		// ReadyForProvisionerDaemon is handled by listAndSeed, not by watch replay.
		ResourceVersion: resourceVersion,
		TimeoutSeconds:  &timeoutSec,
	})
	if err != nil {
		return ErrWatchFailed.Wrap(err, "watch NetworkUpgradeExecute")
	}
	defer watcher.Stop()

	logx.As().Debug().
		Str("reason", "UpgradeMonitorWatchEstablished").
		Str("namespace", um.cfg.Namespace).
		Msg("Watch established on NetworkUpgradeExecute CRs")

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-watcher.ResultChan():
			if !ok {
				// Server closed the watch stream (expected timeout); caller reconnects immediately.
				return nil
			}
			if event.Type == watch.Error {
				// Preserve typed status so isAuthError can detect 401/403.
				return ErrWatchFailed.Wrap(k8serrors.FromObject(event.Object), "watch error event")
			}
			if event.Type != watch.Added && event.Type != watch.Modified {
				continue
			}
			cr, ok := event.Object.(*unstructured.Unstructured)
			if !ok {
				continue
			}
			um.handleEvent(ctx, cr)
		}
	}
}

// handleEvent checks whether the CR has entered ReadyForProvisionerDaemon and,
// if so, triggers handleExecute — deduplicated by operationId across three layers:
//  1. completedOpIDs: guards the CR patch round-trip window and CRs seeded from
//     the cluster List; catches same-session re-delivery after completion.
//  2. activeOpID: guards the single execution slot while handleExecute is in-flight.
//  3. Upstream: the CR's status.phase (Succeeded/Failed) filters historical CRs
//     that are no longer at ReadyForProvisionerDaemon, making (1) restart-safe.
func (um *UpgradeMonitor) handleEvent(ctx context.Context, cr *unstructured.Unstructured) {
	phase, _, _ := unstructured.NestedString(cr.Object, "status", "phase")
	if phase != executePhaseReadyForProvisionerDaemon {
		return
	}

	operationID, _, _ := unstructured.NestedString(cr.Object, "spec", "operationId")
	orbit, _, _ := unstructured.NestedString(cr.Object, "spec", "orbit")

	if operationID == "" {
		logx.As().Warn().
			Str("reason", "UpgradeMonitorMissingOperationID").
			Str("cr_name", cr.GetName()).
			Msg("NetworkUpgradeExecute CR has no spec.operationId — ignoring event")
		return
	}

	um.mu.Lock()
	_, alreadyDone := um.completedOpIDs[operationID]
	if alreadyDone {
		um.mu.Unlock()
		logx.As().Debug().
			Str("reason", "UpgradeMonitorDuplicateEvent").
			Str("operation_id", operationID).
			Msg("Ignoring ReadyForProvisionerDaemon event — operationId already completed in this session")
		return
	}
	if um.activeOpID != "" {
		active := um.activeOpID
		um.mu.Unlock()
		if active == operationID {
			logx.As().Debug().
				Str("reason", "UpgradeMonitorDuplicateEvent").
				Str("operation_id", operationID).
				Msg("Ignoring duplicate ReadyForProvisionerDaemon event for active operation")
		} else {
			logx.As().Warn().
				Str("reason", "UpgradeMonitorBusy").
				Str("active_operation_id", active).
				Str("incoming_operation_id", operationID).
				Msg("Another upgrade is already running — rejecting incoming event")
		}
		return
	}
	um.activeOpID = operationID
	um.mu.Unlock()

	logx.As().Info().
		Str("reason", "ReadyForProvisionerDaemon").
		Str("operation_id", operationID).
		Str("orbit", orbit).
		Str("cr_name", cr.GetName()).
		Msg("NetworkUpgradeExecute entered ReadyForProvisionerDaemon — triggering execute workflow")

	go func() {
		var execErr error
		defer func() {
			if r := recover(); r != nil {
				logx.As().Error().
					Str("reason", "ExecuteWorkflowPanic").
					Str("operation_id", operationID).
					Interface("panic", r).
					Msg("Execute workflow panicked — daemon continues")
			}
			um.mu.Lock()
			if um.activeOpID == operationID {
				um.activeOpID = ""
			}
			// Mark completed only on success so a failed operation can be retried.
			// Retry in this process lifetime requires a new watch event or a
			// listAndSeed reconnect — no automatic requeue is implemented. If
			// handleExecute patched the CR to InProgress before failing (the
			// intended first action once the stub is implemented), an external
			// actor (orchestrator or operator) must re-advance the CR to
			// ReadyForProvisionerDaemon before this daemon will pick it up again.
			// Panic edge case: a panic after the InProgress patch leaves the CR
			// stuck in InProgress across restarts — listAndSeed does not seed
			// InProgress CRs and they are not ReadyForProvisionerDaemon, so
			// recovery requires an external patch to a retryable phase.
			// Manual operator remedy:
			//   kubectl patch networkupgradeexecute <name> -n <namespace> \
			//     --subresource=status --type=merge \
			//     -p '{"status":{"phase":"ReadyForProvisionerDaemon"}}'
			if execErr == nil {
				um.completedOpIDs[operationID] = struct{}{}
			}
			um.mu.Unlock()
		}()
		execErr = um.handleExecute(ctx, cr)
		if execErr != nil {
			logx.As().Error().Err(execErr).
				Str("reason", "ExecuteWorkflowFailed").
				Str("operation_id", operationID).
				Msg("Execute workflow failed")
		}
	}()
}

// handleExecute runs the execute-phase automa workflow for the given CR.
// Stub — implemented in subsequent stories:
//
//   - InfraConfig file placement (atomic move from upgrade dir to host filesystem)
//   - Infra upgrade if infrastructure-versions.yaml requires it (PendingInfraUpgrade)
//   - ConsensusConfig CR creation and reconciliation wait
//   - Patch NetworkUpgradeExecute status to PendingNodeUpgrade + DaemonResult=Succeeded
//
// IMPORTANT — timeout requirement for implementors: each step must use a
// context derived from ctx with an explicit timeout (e.g. context.WithTimeout).
// If any step hangs indefinitely, activeOpID stays set and the daemon will
// reject all future upgrades without crashing or logging an error.
//
// IMPORTANT — event log pruning: after the per-operation EventLogger is closed
// at the end of this function, call filepruner to prune old consensus-upgrade-*.jsonl
// files from paths.DaemonEventsDir (FilenameTimestampStrategy, maxAge=365d, keep=50).
// Pruning at startup covers the initial case; pruning here covers long-running daemons
// where startup pruning never re-runs. See pkg/filepruner and #555.
//
// Once implemented handleExecute must:
//   - Patch CR to InProgress immediately
//   - Patch to Succeeded or Failed on terminal outcome
func (um *UpgradeMonitor) handleExecute(_ context.Context, cr *unstructured.Unstructured) error {
	operationID, _, _ := unstructured.NestedString(cr.Object, "spec", "operationId")
	if um.onExecute != nil {
		um.onExecute(operationID)
	}
	logx.As().Info().
		Str("reason", "ExecuteWorkflowStarted").
		Str("operation_id", operationID).
		Msg("Execute workflow stub — full implementation in subsequent stories")
	return nil
}

// buildDynamicClient builds a dynamic Kubernetes client from the kubeconfig at path.
// restCfg.Timeout is set to 30 s to bound the client-side HTTP request dial.
// Without it, a TCP-level hang (SYN sent, no reply) blocks Watch() for the OS
// TCP timeout (~20 min) regardless of ListOptions.TimeoutSeconds, which is
// server-side only.
func buildDynamicClient(kubeconfigPath string) (dynamic.Interface, error) {
	restCfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, ErrK8sClient.Wrap(err, "load kubeconfig %s", kubeconfigPath)
	}
	restCfg.Timeout = 30 * time.Second
	client, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return nil, ErrK8sClient.Wrap(err, "build dynamic client")
	}
	return client, nil
}

// isAuthError returns true for HTTP 401/403 responses from the K8s API.
// It checks both the error itself and its errorx cause, since errorx.Wrap
// does not expose the inner error via errors.As.
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	if k8serrors.IsUnauthorized(err) || k8serrors.IsForbidden(err) {
		return true
	}
	// Unwrap one level of errorx to reach the typed *StatusError.
	if e := errorx.Cast(err); e != nil {
		cause := e.Cause()
		return k8serrors.IsUnauthorized(cause) || k8serrors.IsForbidden(cause)
	}
	return false
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
