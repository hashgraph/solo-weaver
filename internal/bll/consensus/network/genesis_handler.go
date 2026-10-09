// SPDX-License-Identifier: Apache-2.0

package network

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
)

// genesisNetworkFileName is the pre-built genesis the operator writes verbatim
// into the genesis-config ConfigMap when present in the deployment package.
const genesisNetworkFileName = "genesis-network.json"

// GenesisHandler handles the ActionGenesis intent for a consensus network.
type GenesisHandler struct {
	bll.BaseHandler[models.ConsensusNetworkInputs]
}

// NewGenesisHandler creates a new GenesisHandler.
func NewGenesisHandler(base bll.BaseHandler[models.ConsensusNetworkInputs]) (*GenesisHandler, error) {
	return &GenesisHandler{BaseHandler: base}, nil
}

// PrepareEffectiveInputs resolves the orbit name and the pre-built genesis
// contents. A pre-built genesis is authoritative and skips the operator's
// cluster roster discovery; an explicit --genesis-file overrides the deployment
// package's genesis-network.json when both are provided.
func (h *GenesisHandler) PrepareEffectiveInputs(
	_ models.Intent,
	inputs models.UserInputs[models.ConsensusNetworkInputs],
) (*models.UserInputs[models.ConsensusNetworkInputs], error) {
	resolved := inputs
	custom := &resolved.Custom

	// Namespace doubles as the Orbit name by convention.
	custom.Orbit = custom.Namespace

	switch {
	case strings.TrimSpace(custom.GenesisFile) != "":
		path := strings.TrimSpace(custom.GenesisFile)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, errx.Decorate(
				errorx.ExternalError.Wrap(err, "reading genesis override %s", path),
				reasons.FileMissing,
				fmt.Sprintf("Ensure %s exists and is readable, or omit --genesis-file to use the deployment package's %s", path, genesisNetworkFileName))
		}
		custom.GenesisNetworkJSON = string(data)
	case strings.TrimSpace(custom.DeploymentPackageDir) != "":
		path := filepath.Join(custom.DeploymentPackageDir, genesisNetworkFileName)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, errx.Decorate(
				errorx.ExternalError.Wrap(err, "reading pre-built genesis %s", path),
				reasons.FileMissing,
				fmt.Sprintf("Ensure %s exists in the deployment package, or omit --deployment-package-dir to discover the roster from the cluster", genesisNetworkFileName))
		}
		custom.GenesisNetworkJSON = string(data)
	}

	return &resolved, nil
}

// BuildWorkflow constructs the genesis workflow from resolved inputs. When
// ReadyTimeout is 0 the readiness waits are skipped (apply-only).
func (h *GenesisHandler) BuildWorkflow(
	_ state.State,
	inputs models.UserInputs[models.ConsensusNetworkInputs],
) (*automa.WorkflowBuilder, error) {
	ins := inputs.Custom

	stepList := []automa.Builder{
		steps.EnsureNetworkGenesis(ins.Namespace, ins.Orbit, ins.GenesisNetworkJSON, steps.DefaultCapsuleKubeProvider),
	}
	if ins.ReadyTimeout > 0 {
		// After genesis unblocks the network, confirm the nodes actually come up —
		// otherwise a fresh install's readiness wait is skipped (no genesis yet) and
		// nothing verifies the nodes reach Active.
		stepList = append(stepList,
			steps.WaitNetworkGenesisReady(ins.Namespace, steps.DefaultCapsuleKubeProvider, ins.ReadyTimeout),
			steps.WaitConsensusNetworkReady(ins.Namespace, steps.DefaultCapsuleKubeProvider, ins.ReadyTimeout),
		)
	}

	return automa.NewWorkflowBuilder().WithId("consensus-network-genesis").Steps(stepList...), nil
}

// HandleIntent delegates to the shared BaseHandler orchestration. Genesis
// persists no network-level state section of its own (the NetworkGenesis CR and
// capsule state are owned by the cluster), so it passes a nil state-patch
// callback.
func (h *GenesisHandler) HandleIntent(
	ctx context.Context,
	intent models.Intent,
	inputs models.UserInputs[models.ConsensusNetworkInputs],
) (*automa.Report, error) {
	return h.BaseHandler.HandleIntent(ctx, intent, inputs, h, nil)
}
