// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"context"
	"sync"
	"time"

	"github.com/automa-saga/logx"
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
	phaseReadyForProvisionerDaemon = "ReadyForProvisionerDaemon"

	backoffInitial = 2 * time.Second
	backoffMax     = 5 * time.Minute
	backoffFactor  = 2.0

	// watchTimeoutSeconds is the server-side watch timeout passed to the K8s API.
	// The server closes the channel cleanly after this window; the Run loop reconnects
	// after backoffInitial. Any premature close (network blip, proxy idle timeout) is
	// also subject to the same backoffInitial delay rather than an immediate hot-loop.
	watchTimeoutSeconds = int64(5 * 60)
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
}

// UpgradeMonitor watches the Kubernetes API for NetworkUpgradeExecute CRs
// whose status.phase transitions to ReadyForProvisionerDaemon and triggers the
// execute-phase workflow. It is self-healing: transient errors and auth failures
// are retried with exponential backoff so the daemon recovers without a restart.
type UpgradeMonitor struct {
	cfg    UpgradeMonitorConfig
	client dynamic.Interface

	mu         sync.Mutex
	activeOpID string // non-empty while handleExecute is running; guards the single execution slot

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
	return &UpgradeMonitor{cfg: cfg, client: client}, nil
}

// NewUpgradeMonitorWithClient constructs an UpgradeMonitor with an injected
// client — used in unit tests to avoid a real kubeconfig on disk.
func NewUpgradeMonitorWithClient(cfg UpgradeMonitorConfig, client dynamic.Interface) *UpgradeMonitor {
	return &UpgradeMonitor{cfg: cfg, client: client}
}

// Run blocks until ctx is cancelled. It continuously watches
// NetworkUpgradeExecute CRs and triggers handleExecute on
// ReadyForProvisionerDaemon transitions. Clean watch expiry (server-side
// timeout) reconnects immediately without backoff; real errors retry with
// exponential backoff; auth errors additionally rebuild the dynamic client
// from the kubeconfig on disk before retrying.
func (um *UpgradeMonitor) Run(ctx context.Context) error {
	logx.As().Info().
		Str("reason", "UpgradeMonitorStarted").
		Str("namespace", um.cfg.Namespace).
		Msg("Upgrade monitor started")

	backoff := backoffInitial

	for {
		logx.As().Debug().Str("reason", "UpgradeMonitorWatchAttempt").Msg("Starting watch attempt")
		err := um.runWatch(ctx)

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

// runWatch performs a single watch cycle. Returns nil when the watch channel
// closes cleanly (server-side expiry or context cancellation), or an error
// the caller should back off from and retry. Any panic is caught and converted
// to an error so Run() applies backoff rather than crashing the daemon.
func (um *UpgradeMonitor) runWatch(ctx context.Context) (retErr error) {
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
		// ResourceVersion "0" serves from the watch cache and replays all existing CRs
		// as ADDED events on every reconnect. This is intentional: NetworkUpgradeExecute
		// CRs are long-lived, so any CR in ReadyForProvisionerDaemon at reconnect time
		// is recovered automatically. The trade-off (no 410 Gone detection, full re-scan)
		// is acceptable for a single-namespace watch on a small CR set.
		ResourceVersion: "0",
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
// if so, triggers handleExecute — deduplicated by operationId.
func (um *UpgradeMonitor) handleEvent(ctx context.Context, cr *unstructured.Unstructured) {
	phase, _, _ := unstructured.NestedString(cr.Object, "status", "phase")
	if phase != phaseReadyForProvisionerDaemon {
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
			um.mu.Unlock()
		}()
		if err := um.handleExecute(ctx, cr); err != nil {
			logx.As().Error().Err(err).
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
