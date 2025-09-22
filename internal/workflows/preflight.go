package workflows

import (
	"context"
	"os/user"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-provisioner/pkg/hardware"
)

// createNodeSpec creates the appropriate node spec based on node type and host profile
func createNodeSpec(nodeType string, hostProfile hardware.HostProfile) (hardware.Spec, error) {
	return hardware.CreateNodeSpec(nodeType, hostProfile)
}

// CheckHostProfileStep retrieves host profile and validates node type
func CheckHostProfileStep(nodeType string) automa.Builder {
	return automa.NewStepBuilder("validate-host-profile", automa.WithOnExecute(func(context.Context) (*automa.Report, error) {
		// Use the new HostProfile abstraction
		hostProfile := hardware.GetHostProfile()
		logx.As().Info().Interface("host_profile", hostProfile.String()).Msg("Retrieved host profile")

		// Validate node type is supported using centralized validation
		if !hardware.IsValidNodeType(nodeType) {
			return nil, errorx.IllegalArgument.New("unsupported node type: %s. Supported types: %v", nodeType, hardware.SupportedNodeTypes())
		}

		logx.As().Info().Str("node_type", nodeType).Msg("Host profile retrieved and node type validated")
		return automa.StepSuccessReport("validate-host-profile"), nil
	}))
}

// CheckPrivilegesStep validates that the current user has superuser privileges
func CheckPrivilegesStep() automa.Builder {
	return automa.NewStepBuilder("validate-privileges", automa.WithOnExecute(func(context.Context) (*automa.Report, error) {
		current, err := user.Current()
		if err != nil {
			return nil, errorx.IllegalState.Wrap(err, "failed to get current user")
		}

		if current.Uid != "0" {
			return nil, errorx.IllegalState.New("requires superuser privilege")
		}

		logx.As().Info().Msg("Superuser privilege validated")
		return automa.StepSuccessReport("validate-privileges"), nil
	}))
}

// CheckOSStep validates OS requirements for a specific node type
func CheckOSStep(nodeType string) automa.Builder {
	return automa.NewStepBuilder("validate-os", automa.WithOnExecute(func(context.Context) (*automa.Report, error) {
		hostProfile := hardware.GetHostProfile()
		nodeSpec, err := createNodeSpec(nodeType, hostProfile)
		if err != nil {
			return nil, err
		}

		if err := nodeSpec.ValidateOS(); err != nil {
			return nil, errorx.IllegalState.Wrap(err, "OS validation failed")
		}

		logx.As().Info().Str("node_type", nodeSpec.GetNodeType()).Msg("OS requirements validated")
		return automa.StepSuccessReport("validate-os"), nil
	}))
}

// CheckCPUStep validates CPU requirements for a specific node type
func CheckCPUStep(nodeType string) automa.Builder {
	return automa.NewStepBuilder("validate-cpu", automa.WithOnExecute(func(context.Context) (*automa.Report, error) {
		hostProfile := hardware.GetHostProfile()
		nodeSpec, err := createNodeSpec(nodeType, hostProfile)
		if err != nil {
			return nil, err
		}

		if err := nodeSpec.ValidateCPU(); err != nil {
			return nil, errorx.IllegalState.Wrap(err, "CPU validation failed")
		}

		logx.As().Info().Str("node_type", nodeSpec.GetNodeType()).Msg("CPU requirements validated")
		return automa.StepSuccessReport("validate-cpu"), nil
	}))
}

// CheckMemoryStep validates memory requirements for a specific node type
func CheckMemoryStep(nodeType string) automa.Builder {
	return automa.NewStepBuilder("validate-memory", automa.WithOnExecute(func(context.Context) (*automa.Report, error) {
		hostProfile := hardware.GetHostProfile()
		nodeSpec, err := createNodeSpec(nodeType, hostProfile)
		if err != nil {
			return nil, err
		}

		if err := nodeSpec.ValidateMemory(); err != nil {
			return nil, errorx.IllegalState.Wrap(err, "memory validation failed")
		}

		logx.As().Info().Str("node_type", nodeSpec.GetNodeType()).Msg("Memory requirements validated")
		return automa.StepSuccessReport("validate-memory"), nil
	}))
}

// CheckStorageStep validates storage requirements for a specific node type
func CheckStorageStep(nodeType string) automa.Builder {
	return automa.NewStepBuilder("validate-storage", automa.WithOnExecute(func(context.Context) (*automa.Report, error) {
		hostProfile := hardware.GetHostProfile()
		nodeSpec, err := createNodeSpec(nodeType, hostProfile)
		if err != nil {
			return nil, err
		}

		if err := nodeSpec.ValidateStorage(); err != nil {
			return nil, errorx.IllegalState.Wrap(err, "storage validation failed")
		}

		logx.As().Info().Str("node_type", nodeSpec.GetNodeType()).Msg("Storage requirements validated")
		return automa.StepSuccessReport("validate-storage"), nil
	}))
}

// NewNodeSafetyCheckWorkflow creates a safety check workflow for any node type
func NewNodeSafetyCheckWorkflow(nodeType string) automa.Builder {
	return automa.NewWorkFlowBuilder(nodeType+"-node-preflight").Steps(
		CheckPrivilegesStep(),
		CheckHostProfileStep(nodeType),
		CheckOSStep(nodeType),
		CheckCPUStep(nodeType),
		CheckMemoryStep(nodeType),
		CheckStorageStep(nodeType),
		//CheckDockerStep(),
	)
}
