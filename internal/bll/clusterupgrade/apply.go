// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"context"
	"slices"
	"strings"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"

	"github.com/hashgraph/solo-weaver/internal/state"
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

// WorkflowRunner runs a workflow and returns its failure. The command passes
// one that also renders and saves the report.
type WorkflowRunner func(ctx context.Context, wb *automa.WorkflowBuilder) error

// recordHop runs run under the state locks of ids with an upgrade entry in the
// action history, then applies patch. When ids include the cluster, it also
// records the live cluster; a cluster that cannot be read keeps its record.
func recordHop(ctx context.Context, src LiveSources, inputs any, ids []state.ComponentID, run func() error, patch func(*state.State)) error {
	entry := state.NewEntry(ctx, models.Intent{Action: models.ActionUpgrade, Target: models.TargetCluster}, inputs)
	return state.RunOwning(src.State, entry, ids, run, func(st *state.State) error {
		if patch != nil {
			patch(st)
		}
		if !slices.Contains(ids, state.ComponentCluster) {
			return nil
		}
		cs, err := src.Cluster.RefreshState(ctx)
		if err != nil {
			return errorx.IllegalState.Wrap(err, "failed to read the cluster after the hop")
		}
		if cs.Unobserved {
			logx.As().Warn().Msg("Could not read the cluster after the hop; keeping its recorded state")
			return nil
		}
		st.ClusterState = cs
		return nil
	})
}
