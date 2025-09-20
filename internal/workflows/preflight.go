package workflows

import (
	"context"
	"log"
	"os/user"
	"strings"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-provisioner/pkg/hardware"
)

func CheckSysInfoStep() automa.Builder {
	return CheckHostProfileStepForNodeType("local")
}

// CheckHostProfileStepForNodeType validates system requirements for a specific node type
func CheckHostProfileStepForNodeType(nodeType string) automa.Builder {
	return automa.NewStepBuilder("check-host-spec", automa.WithOnExecute(func(context.Context) (*automa.Report, error) {
		current, err := user.Current()
		if err != nil {
			log.Fatal(err)
		}

		if current.Uid != "0" {
			return nil, errorx.IllegalState.New("requires superuser privilege")
		}

		// Use the new HostProfile abstraction
		hostProfile := hardware.GetHostProfile()
		logx.As().Info().Interface("host_profile", hostProfile.String()).Msg("Retrieved host profile")

		// Create the appropriate node spec based on node type
		var nodeSpec hardware.Spec
		switch strings.ToLower(nodeType) {
		case "local":
			nodeSpec = hardware.NewLocalNodeSpec(hostProfile)
		case "block":
			nodeSpec = hardware.NewBlockNodeSpec(hostProfile)
		case "consensus":
			nodeSpec = hardware.NewConsensusNodeSpec(hostProfile)
		default:
			return nil, errorx.IllegalArgument.New("unsupported node type: %s. Supported types: block, consensus, local", nodeType)
		}

		if err := validateHostRequirements(nodeSpec); err != nil {
			return nil, err
		}

		logx.As().Info().Str("node_type", nodeSpec.GetNodeType()).Msg("All host requirements satisfied")

		return automa.StepSuccessReport("check-host-spec"), nil
	}))
}

// validateHostRequirements validates a node spec against hardware requirements
func validateHostRequirements(nodeSpec hardware.Spec) error {
	requirements := nodeSpec.GetBaselineRequirements()

	logx.As().Info().Str("checking", requirements.String()).Msg("Validating host requirements for node " + nodeSpec.GetNodeType())

	if err := nodeSpec.ValidateOS(); err != nil {
		return errorx.IllegalState.Wrap(err, "host validation failed")
	}

	if err := nodeSpec.ValidateCPU(); err != nil {
		return errorx.IllegalState.Wrap(err, "host validation failed")
	}

	if err := nodeSpec.ValidateMemory(); err != nil {
		return errorx.IllegalState.Wrap(err, "host validation failed")
	}

	if err := nodeSpec.ValidateStorage(); err != nil {
		return errorx.IllegalState.Wrap(err, "host validation failed")
	}

	return nil
}

func NewSystemSafetyCheckWorkflow() automa.Builder {
	return automa.NewWorkFlowBuilder("preflight").Steps(
		CheckSysInfoStep(),
		//CheckDockerStep(),
	)
}
