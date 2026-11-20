// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
)

// Check is one pre-hop check: what was checked, whether it passed and why not.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// Checks are the pre-hop checks of one hop, in order.
type Checks []Check

func (cs *Checks) add(name string, ok bool, detail string) {
	if ok {
		detail = ""
	}
	*cs = append(*cs, Check{Name: name, OK: ok, Detail: detail})
}

// Failed returns the checks that did not pass.
func (cs Checks) Failed() Checks {
	var out Checks
	for _, c := range cs {
		if !c.OK {
			out = append(out, c)
		}
	}
	return out
}

// Err returns an error naming every failed check, or nil when all passed.
func (cs Checks) Err() error {
	failed := cs.Failed()
	if len(failed) == 0 {
		return nil
	}
	parts := make([]string, len(failed))
	for i, c := range failed {
		parts[i] = c.Name + ": " + c.Detail
	}
	return errx.Decorate(
		errorx.RejectedOperation.New("the hop cannot run: %s", strings.Join(parts, "; ")),
		reasons.PreconditionNotMet,
		"Run 'solo-provisioner kube cluster upgrade --plan --experimental' to see the next hop for this cluster.")
}

// WorkflowRunner runs a hop's workflow through run, which builds and executes
// it and returns its report, and returns the hop's failure. The command passes
// common.RunWorkflow, which also renders and saves the report.
type WorkflowRunner func(ctx context.Context, run func() (*automa.Report, error)) error

// Keys of a hop's action history inputs.
const (
	inputComponent = "component"
	inputFrom      = "from"
	inputTo        = "to"
	inputCiliumCLI = "cilium-cli"
)

// NodesReadyStepID is the id of the step that checks the node after a hop.
const NodesReadyStepID = "check-nodes-ready"

// How long a hop waits for the cluster to be fully readable before it records
// it, and how often it looks. Variables so tests can shorten them.
var (
	readableTimeout  = 2 * time.Minute
	readableInterval = 5 * time.Second
)

// Seam so tests need no cluster for the node check.
var nodesReadyStep = func() automa.Builder {
	return steps.CheckClusterNodesReady(NodesReadyStepID, kube.ClientFromContext)
}

// heldFor checks that locks cover ids in src's state directory. A hop reads and
// writes its records only under locks its caller holds for the whole run.
func heldFor(src LiveSources, locks *state.HeldLocks, ids ...state.ComponentID) error {
	if src.State == nil {
		return errx.Decorate(errorx.IllegalArgument.New("every live source must be set"), reasons.Internal)
	}
	return locks.Covers(filepath.Dir(src.State.State().StateFile), ids...)
}

// readHopLive is readLive for a hop. A cluster the probe cannot reach is an
// error with what to do about it, and every error is decorated for the
// operator.
func readHopLive(ctx context.Context, src LiveSources) (rsl.ClusterShape, Matrix, error) {
	shape, m, reach, err := readLive(ctx, src)
	switch {
	case errorx.IsOfType(err, errorx.IllegalArgument), errorx.IsOfType(err, errorx.IllegalFormat):
		return rsl.ClusterShape{}, Matrix{}, errx.Decorate(err, reasons.Internal)
	case err != nil:
		return rsl.ClusterShape{}, Matrix{}, errx.Decorate(err, reasons.StateOperationFailed,
			"Run the command with sudo, check that 'kubectl get nodes' works on this host, and see the log for the cause.")
	case !reach.Observed():
		cause, remedy := unreachable(reach)
		logx.As().Warn().Msg("Cannot reach the cluster; the hop will not run: " + cause)
		return rsl.ClusterShape{}, Matrix{}, errx.Decorate(
			errorx.RejectedOperation.New("the hop cannot run: %s", cause),
			reasons.PreconditionNotMet, sentence(remedy))
	}
	return shape, m, nil
}

// clusterReadable is the finish gate's read: nil once the probe reaches the
// cluster and the checker reads all of it.
func clusterReadable(src LiveSources) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		if r := src.Probe(); !r.Observed() {
			cause, _ := unreachable(r)
			return errorx.IllegalState.New("%s", cause)
		}
		cs, err := src.Cluster.RefreshState(ctx)
		if err != nil {
			return err
		}
		if cs.Unobserved {
			return errorx.IllegalState.New("not every node and release of the cluster could be read")
		}
		return nil
	}
}

// hopRecord is the RecordState write of a hop: the cluster as the checker reads
// it when ids include the cluster, then patch, persisted for ids only.
func hopRecord(src LiveSources, ids []state.ComponentID, patch func(*state.State)) func(ctx context.Context) ([]state.ChangedFile, error) {
	return func(ctx context.Context) ([]state.ChangedFile, error) {
		st := src.State.State()
		if slices.Contains(ids, state.ComponentCluster) {
			cs, err := src.Cluster.RefreshState(ctx)
			if err != nil {
				return nil, errx.Decorate(errorx.InternalError.Wrap(err, "unexpected error from the cluster checker"),
					reasons.Internal)
			}
			st.ClusterState = cs
		}
		if patch != nil {
			patch(&st)
		}
		return src.State.Set(st).Persist(ids...)
	}
}

// runHop runs wb as one hop through run: the hop's steps, a check that the node
// is ready, a wait until the cluster is fully readable, then RecordState with
// write. One upgrade entry with inputs is appended once the workflow has run.
func runHop(ctx context.Context, src LiveSources, run WorkflowRunner, inputs map[string]string,
	wb *automa.WorkflowBuilder, write func(ctx context.Context) ([]state.ChangedFile, error)) error {
	intent := models.Intent{Action: models.ActionUpgrade, Target: models.TargetCluster}
	return run(ctx, func() (*automa.Report, error) {
		wf, err := wb.Steps(
			nodesReadyStep(),
			steps.WaitClusterReadable(clusterReadable(src), readableTimeout, readableInterval),
			steps.RecordState(write),
		).Build()
		if err != nil {
			return nil, errx.Decorate(errorx.InternalError.Wrap(err, "failed to build the hop workflow"), reasons.Internal)
		}
		report := wf.Execute(ctx)
		steps.RecordEntry(src.State, steps.WorkflowEntry(ctx, intent, inputs, report), report)
		return report, nil
	})
}

// recordSoftware patches st with name installed at version.
func recordSoftware(st *state.State, name, version string) {
	if st.MachineState.Software == nil {
		st.MachineState.Software = map[string]state.SoftwareState{}
	}
	sw := st.MachineState.Software[name]
	sw.Name, sw.Version, sw.Installed = name, version, true
	st.MachineState.Software[name] = sw
}

// sentence makes s a sentence: a capital first letter and a full stop.
func sentence(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:] + "."
}
