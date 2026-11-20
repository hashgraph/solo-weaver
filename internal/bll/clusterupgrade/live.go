// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"context"

	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"

	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// LiveSources are what PlanLive reads.
type LiveSources struct {
	State   state.Manager
	Cluster reality.Checker[state.ClusterState]
	Machine reality.Checker[state.MachineState]
	Managed func() ([]reality.ManagedRelease, error)
	Catalog func() (*software.InfrastructureCatalog, error)
	// Probe must be the probe the cluster checker uses.
	Probe reality.ClusterProbe
}

// PlanLive plans the upgrade of the cluster as it runs now. It reads state and
// the cluster and writes nothing: the checkers return what they read without
// recording it. It takes no lock, because a plan is a snapshot that the next
// call recomputes anyway.
//
// The cluster is read directly, not through the runtime resolver, which keeps
// the recorded cluster when the read fails: the plan must see a failed read to
// refuse to plan from it.
func PlanLive(ctx context.Context, src LiveSources, opts Options) (Plan, error) {
	if src.State == nil || src.Cluster == nil || src.Machine == nil || src.Managed == nil || src.Catalog == nil || src.Probe == nil {
		return Plan{}, errorx.IllegalArgument.New("every live source must be set")
	}

	// The cluster checker falls back to recorded values, so load them first.
	if err := src.State.Refresh(); err != nil && !errorx.IsOfType(err, state.NotFoundError) {
		return Plan{}, errorx.IllegalState.Wrap(err, "failed to read the recorded state")
	}

	cat, err := src.Catalog()
	if err != nil {
		return Plan{}, errorx.IllegalState.Wrap(err, "failed to load the catalog")
	}
	m, err := MatrixFromCatalog(cat)
	if err != nil {
		return Plan{}, err
	}
	// With no kubeconfig the cluster checker returns the record as read, so the
	// plan would come from the record.
	if r := src.Probe(); !r.Observed() {
		return blockedPlan(m, opts, unreachableBlocker(r))
	}

	managed, err := src.Managed()
	if err != nil {
		return Plan{}, errorx.IllegalState.Wrap(err, "failed to list the managed releases")
	}

	var live state.State
	if live.MachineState, err = src.Machine.RefreshState(ctx); err != nil {
		return Plan{}, errorx.IllegalState.Wrap(err, "failed to read the host")
	}
	if live.ClusterState, err = src.Cluster.RefreshState(ctx); err != nil {
		return Plan{}, errorx.IllegalState.Wrap(err, "failed to read the cluster")
	}

	// The plan uses only live values; the record would only mark what changed.
	return Resolve(rsl.ResolveClusterShape(live, nil, managed), m, opts)
}

// unreachableBlocker is the blocker for a cluster the probe could not reach. It
// names the cause and what to do about it.
func unreachableBlocker(r reality.ClusterReachability) Blocker {
	if !r.Configured() {
		logx.As().Warn().Msg("No kubeconfig found; cannot plan the cluster upgrade")
		return Blocker{Code: BlockerNotObserved, Detail: "no kubeconfig was found in KUBECONFIG or $HOME/.kube/config; " +
			"run the command with sudo, or set KUBECONFIG to the cluster's admin kubeconfig"}
	}
	logx.As().Warn().Msg("The Kubernetes API server did not answer; cannot plan the cluster upgrade")
	return Blocker{Code: BlockerNotObserved, Detail: "the Kubernetes API server did not answer; " +
		"check it with 'kubectl get --raw /readyz', then run the command again"}
}
