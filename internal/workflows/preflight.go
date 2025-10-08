package workflows

import (
	"context"
	"golang.hedera.com/solo-provisioner/internal/workflows/notify"
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
	return automa.NewStepBuilder().WithId("validate-host-profile").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			// Use the new HostProfile abstraction
			hostProfile := hardware.GetHostProfile()
			logx.As().Info().Interface("host_profile", hostProfile.String()).Msg("Retrieved host profile")

			// Validate node type is supported using centralized validation
			if !hardware.IsValidNodeType(nodeType) {
				return automa.FailureReport(stp,
					automa.WithError(
						errorx.IllegalArgument.New("unsupported node type: %s. Supported types: %v",
							nodeType, hardware.SupportedNodeTypes())))
			}

			logx.As().Info().Str("node_type", nodeType).Msg("Host profile retrieved and node type validated")
			return automa.SuccessReport(stp)
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			logx.As().Error().Err(rpt.Error).Msg("Failed to validate host profile")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			if rpt.IsSuccess() {
				logx.As().Info().Msg("Host profile validation step completed successfully")
			}
		})
}

// CheckPrivilegesStep validates that the current user has superuser privileges
func CheckPrivilegesStep() automa.Builder {
	return automa.NewStepBuilder().WithId("validate-privileges").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			current, err := user.Current()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(errorx.IllegalState.Wrap(err, "failed to get current user")))
			}

			if current.Uid != "0" {
				return automa.FailureReport(stp,
					automa.WithError(errorx.IllegalState.New("requires superuser privilege")))
			}

			logx.As().Info().Msg("Superuser privilege validated")
			return automa.SuccessReport(stp)
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Privilege validation failed")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Privilege validation step completed successfully")
		})
}

// CheckOSStep validates OS requirements for a specific node type
func CheckOSStep(nodeType string) automa.Builder {
	return automa.NewStepBuilder().WithId("validate-os").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			hostProfile := hardware.GetHostProfile()
			nodeSpec, err := createNodeSpec(nodeType, hostProfile)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			if err := nodeSpec.ValidateOS(); err != nil {
				return automa.FailureReport(stp,
					automa.WithError(errorx.IllegalState.Wrap(err, "OS validation failed")))
			}

			logx.As().Info().Str("node_type", nodeSpec.GetNodeType()).Msg("OS requirements validated")
			return automa.SuccessReport(stp)
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "OS validation failed")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "OS validation step completed successfully")
		})
}

// CheckCPUStep validates CPU requirements for a specific node type
func CheckCPUStep(nodeType string) automa.Builder {
	return automa.NewStepBuilder().WithId("validate-cpu").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			hostProfile := hardware.GetHostProfile()
			nodeSpec, err := createNodeSpec(nodeType, hostProfile)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			if err := nodeSpec.ValidateCPU(); err != nil {
				return automa.FailureReport(stp,
					automa.WithError(
						automa.StepExecutionError.Wrap(err, "CPU validation failed")))
			}

			logx.As().Info().Str("node_type", nodeSpec.GetNodeType()).Msg("CPU requirements validated")
			return automa.SuccessReport(stp)
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "CPU validation failed")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "CPU validation step completed successfully")
		})

}

// CheckMemoryStep validates memory requirements for a specific node type
func CheckMemoryStep(nodeType string) automa.Builder {
	return automa.NewStepBuilder().WithId("validate-memory").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			hostProfile := hardware.GetHostProfile()
			nodeSpec, err := createNodeSpec(nodeType, hostProfile)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			if err := nodeSpec.ValidateMemory(); err != nil {
				return automa.FailureReport(stp, automa.WithError(errorx.IllegalState.Wrap(err, "memory validation failed")))
			}

			logx.As().Info().Str("node_type", nodeSpec.GetNodeType()).Msg("Memory requirements validated")
			return automa.SuccessReport(stp)
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Memory validation failed")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Memory validation step completed successfully")
		})
}

// CheckStorageStep validates storage requirements for a specific node type
func CheckStorageStep(nodeType string) automa.Builder {
	return automa.NewStepBuilder().WithId("validate-storage").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			hostProfile := hardware.GetHostProfile()
			nodeSpec, err := createNodeSpec(nodeType, hostProfile)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			if err := nodeSpec.ValidateStorage(); err != nil {
				return automa.FailureReport(stp,
					automa.WithError(errorx.IllegalState.Wrap(err, "storage validation failed")))
			}

			logx.As().Info().Str("node_type", nodeSpec.GetNodeType()).Msg("Storage requirements validated")
			return automa.SuccessReport(stp)
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Storage validation failed")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Storage validation step completed successfully")
		})
}

// NewNodeSafetyCheckWorkflow creates a safety check workflow for any node type
func NewNodeSafetyCheckWorkflow(nodeType string) automa.Builder {
	return automa.NewWorkflowBuilder().
		WithId(nodeType+"-node-preflight").Steps(
		CheckPrivilegesStep(),
		CheckHostProfileStep(nodeType),
		CheckOSStep(nodeType),
		CheckCPUStep(nodeType),
		CheckMemoryStep(nodeType),
		CheckStorageStep(nodeType),
		//CheckDockerStep(),
	).WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
		notify.As().StepFailure(ctx, stp, rpt, "Node preflight checks failed")
	}).WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
		notify.As().StepCompletion(ctx, stp, rpt, "Node preflight checks completed successfully")
	})
}
