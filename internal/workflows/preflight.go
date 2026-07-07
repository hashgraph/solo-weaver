// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"strings"

	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/config"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/pkg/hardware"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// createNodeSpec creates the appropriate node spec based on a DeploymentSpec and host profile
func createNodeSpec(spec hardware.DeploymentSpec, hostProfile hardware.HostProfile) (hardware.Spec, error) {
	return hardware.CreateNodeSpec(spec, hostProfile)
}

// CheckHostProfileStep retrieves host profile and validates node type and profile
func CheckHostProfileStep(spec hardware.DeploymentSpec) automa.Builder {
	return automa.NewStepBuilder().WithId("validate-host-profile").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			// Use the new HostProfile abstraction
			hostProfile := hardware.GetHostProfile()
			logx.As().Info().Msgf("host: %s", hostProfile.String())

			// Validate node type is supported using centralized validation
			if !hardware.IsValidNodeType(spec.NodeType) {
				return automa.FailureReport(stp,
					automa.WithError(
						errorx.IllegalArgument.New("unsupported node type: %q. Supported types: %v",
							spec.NodeType, hardware.SupportedNodeTypes())))
			}

			// Validate profile
			if !hardware.IsValidProfile(spec.Profile) {
				return automa.FailureReport(stp,
					automa.WithError(
						errorx.IllegalArgument.New("unsupported profile: %q. Supported profiles: %v",
							spec.Profile, models.SupportedProfiles())))
			}

			logx.As().Info().Msgf("node type: %s, profile: %s", spec.NodeType, spec.Profile)
			return automa.SuccessReport(stp)
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Validating host profile")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Validating host profile")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Validating host profile")
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

			weaverUID := config.WeaverUserId()
			if current.Uid != "0" && current.Uid != weaverUID {
				return automa.FailureReport(stp,
					automa.WithError(
						errorx.IllegalState.New("requires superuser privilege").
							WithProperty(doctor.ErrPropertyResolution,
								fmt.Sprintf("Run the command with 'sudo' or as root user: `sudo %s`",
									strings.Join(os.Args, " ")))))
			}

			logx.As().Info().Msg("Superuser privilege validated")
			return automa.SuccessReport(stp)
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Validating privileges")
			return ctx, nil

		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Validating privileges")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Validating privileges")
		})
}

// CheckWeaverUserStep validates that the provisioner user and group exist with the correct IDs
func CheckWeaverUserStep() automa.Builder {
	return automa.NewStepBuilder().WithId("validate-weaver-user").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			weaverUsername := config.WeaverUserName()
			weaverUserId := config.WeaverUserId()
			weaverGroupName := config.WeaverGroupName()
			weaverGroupId := config.WeaverGroupId()

			// Check if user exists
			weaverUser, userErr := user.Lookup(weaverUsername)
			userExists := userErr == nil

			// Check if group exists
			weaverGroup, groupErr := user.LookupGroup(weaverGroupName)
			groupExists := groupErr == nil

			// Collect validation errors for user and group before returning
			var errors []error
			var instructions string

			// Track mismatches in meta
			if userExists && weaverUser.Uid != weaverUserId {
				meta["user_exists"] = "true"
				meta["user_id_mismatch"] = "true"
				meta["expected_user_id"] = weaverUserId
				meta["actual_user_id"] = weaverUser.Uid
				errors = append(errors, errorx.IllegalState.New("provisioner user exists with incorrect UID: expected %s, got %s", weaverUserId, weaverUser.Uid))
				instructions += fmt.Sprintf("User '%s' exists but has incorrect UID.\n", weaverUsername)
				instructions += fmt.Sprintf("Expected: %s, Found: %s\n\n", weaverUserId, weaverUser.Uid)
			}

			if groupExists && weaverGroup.Gid != weaverGroupId {
				meta["group_exists"] = "true"
				meta["group_id_mismatch"] = "true"
				meta["expected_group_id"] = weaverGroupId
				meta["actual_group_id"] = weaverGroup.Gid
				errors = append(errors, errorx.IllegalState.New("provisioner group exists with incorrect GID: expected %s, got %s", weaverGroupId, weaverGroup.Gid))
				instructions += fmt.Sprintf("Group '%s' exists but has incorrect GID.\n", weaverGroupName)
				instructions += fmt.Sprintf("Expected: %s, Found: %s\n\n", weaverGroupId, weaverGroup.Gid)
			}

			// If there are any errors, provide combined instructions
			if len(errors) > 0 {
				instructions += "Please update the user and/or group IDs as follows:\n\n"
				// Suggest groupmod if group exists but has wrong GID
				if groupExists && weaverGroup.Gid != weaverGroupId {
					instructions += fmt.Sprintf("  sudo groupmod -g %s %s\n", weaverGroupId, weaverGroupName)
				}
				// Suggest usermod if user exists but has wrong UID
				if userExists && weaverUser.Uid != weaverUserId {
					instructions += fmt.Sprintf("  sudo usermod -u %s -g %s %s\n", weaverUserId, weaverGroupId, weaverUsername)
				}
				instructions += "\nNote: After changing user/group IDs, you may need to update file ownerships accordingly.\n"
				meta["instructions"] = instructions

				// Combine errors into one error message
				var errMsg string
				for i, err := range errors {
					if i > 0 {
						errMsg += "; "
					}
					errMsg += err.Error()
				}

				return automa.FailureReport(stp,
					automa.WithError(errorx.IllegalState.New("%s", errMsg)),
					automa.WithMetadata(meta))
			}

			// If either user or group doesn't exist, provide creation instructions
			if !userExists || !groupExists {
				meta["user_exists"] = fmt.Sprintf("%t", userExists)
				meta["group_exists"] = fmt.Sprintf("%t", groupExists)

				if !userExists && !groupExists {
					instructions = fmt.Sprintf("The weaver service account ('%s'/'%s') has not been provisioned on this host.\n", weaverUsername, weaverGroupName)
					instructions += "Run the installer to set it up:\n\n"
					instructions += "  sudo solo-provisioner install"
				} else if !userExists {
					instructions = fmt.Sprintf("The weaver user '%s' does not exist.\n", weaverUsername)
					instructions += "The service account is provisioned by the installer — run:\n\n"
					instructions += "  sudo solo-provisioner install"
				} else {
					instructions = fmt.Sprintf("The weaver group '%s' does not exist.\n", weaverGroupName)
					instructions += "The service account is provisioned by the installer — run:\n\n"
					instructions += "  sudo solo-provisioner install"
				}

				meta["instructions"] = instructions

				var errMsg string
				if !userExists && !groupExists {
					errMsg = fmt.Sprintf("provisioner user '%s' and group '%s' do not exist", weaverUsername, weaverGroupName)
				} else if !userExists {
					errMsg = fmt.Sprintf("provisioner user '%s' does not exist", weaverUsername)
				} else {
					errMsg = fmt.Sprintf("provisioner group '%s' does not exist", weaverGroupName)
				}

				return automa.FailureReport(stp,
					automa.WithError(errorx.IllegalState.New("%s", errMsg)),
					automa.WithMetadata(meta))
			}

			// Both weaver user and group exist with correct IDs.
			// hedera:2000 is not validated here — EnsureHederaOwnerStep handles
			// hedera user/group creation and wires weaver into the hedera supplementary
			// group. That step runs as part of block-node install and will run as part
			// of CN deploy when that workflow is implemented.
			meta["user_exists"] = "true"
			meta["group_exists"] = "true"
			meta["user_id"] = weaverUserId
			meta["group_id"] = weaverGroupId

			logx.As().Info().
				Str("user", weaverUsername).
				Str("user_id", weaverUserId).
				Str("group", weaverGroupName).
				Str("group_id", weaverGroupId).
				Msg("Solo Provisioner user and group validated")

			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Validating service account")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Validating service account")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Validating service account")
		})
}

// CheckOSStep validates OS requirements for a specific node type
func CheckOSStep(spec hardware.DeploymentSpec) automa.Builder {
	return automa.NewStepBuilder().WithId("validate-os").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			hostProfile := hardware.GetHostProfile()
			nodeSpec, err := createNodeSpec(spec, hostProfile)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			reqs := nodeSpec.GetBaselineRequirements()
			logx.As().Info().Msgf("detected: %s %s, required: %v", hostProfile.GetOSVendor(), hostProfile.GetOSVersion(), reqs.MinSupportedOS)

			if err := nodeSpec.ValidateOS(); err != nil {
				return automa.FailureReport(stp,
					automa.WithError(errorx.IllegalState.Wrap(err, "OS validation failed")))
			}
			return automa.SuccessReport(stp)
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Validating OS requirements")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Validating OS requirements")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Validating OS requirements")
		})
}

// CheckCPUStep validates CPU requirements for a specific node type
func CheckCPUStep(spec hardware.DeploymentSpec) automa.Builder {
	return automa.NewStepBuilder().WithId("validate-cpu").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			hostProfile := hardware.GetHostProfile()
			nodeSpec, err := createNodeSpec(spec, hostProfile)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			reqs := nodeSpec.GetBaselineRequirements()
			logx.As().Info().Msgf("detected: %d cores, required: %d cores", hostProfile.GetCPUCores(), reqs.MinCpuCores)

			if err := nodeSpec.ValidateCPU(); err != nil {
				baseErr := automa.StepExecutionError.Wrap(err, "CPU validation failed")
				if p, ok := hardware.Providers()[spec.NodeType]; ok {
					if _, whyMap, e := p.ComputeWithWhy(spec); e == nil && whyMap["cpu"] != "" {
						baseErr = baseErr.WithProperty(models.ErrPropertyWhyFloor, whyMap["cpu"])
					}
				}
				return automa.FailureReport(stp, automa.WithError(baseErr))
			}
			return automa.SuccessReport(stp)
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Validating CPU requirements")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Validating CPU requirements")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Validating CPU requirements")
		})

}

// CheckMemoryStep validates memory requirements for a specific node type
func CheckMemoryStep(spec hardware.DeploymentSpec) automa.Builder {
	return automa.NewStepBuilder().WithId("validate-memory").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			hostProfile := hardware.GetHostProfile()
			nodeSpec, err := createNodeSpec(spec, hostProfile)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			reqs := nodeSpec.GetBaselineRequirements()
			logx.As().Info().Msgf("detected: %d GB, required: %d GB", hostProfile.GetTotalMemoryGB(), reqs.MinMemoryGB)

			if err := nodeSpec.ValidateMemory(); err != nil {
				baseErr := errorx.IllegalState.Wrap(err, "memory validation failed")
				if p, ok := hardware.Providers()[spec.NodeType]; ok {
					if _, whyMap, e := p.ComputeWithWhy(spec); e == nil && whyMap["memory"] != "" {
						baseErr = baseErr.WithProperty(models.ErrPropertyWhyFloor, whyMap["memory"])
					}
				}
				return automa.FailureReport(stp, automa.WithError(baseErr))
			}
			return automa.SuccessReport(stp)
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Validating memory requirements")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Validating memory requirements")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Validating memory requirements")
		})
}

// CheckStorageStep validates storage requirements for a specific node type
func CheckStorageStep(spec hardware.DeploymentSpec) automa.Builder {
	return automa.NewStepBuilder().WithId("validate-storage").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {

			hostProfile := hardware.GetHostProfile()
			nodeSpec, err := createNodeSpec(spec, hostProfile)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			reqs := nodeSpec.GetBaselineRequirements()
			logx.As().Info().Msgf("detected: %d GB total (%d GB SSD, %d GB HDD), required: %d GB",
				hostProfile.GetTotalStorageGB(), hostProfile.GetSSDStorageGB(), hostProfile.GetHDDStorageGB(), reqs.MinStorageGB)

			if err := nodeSpec.ValidateStorage(); err != nil {
				baseErr := errorx.IllegalState.Wrap(err, "storage validation failed")
				if p, ok := hardware.Providers()[spec.NodeType]; ok {
					if _, whyMap, e := p.ComputeWithWhy(spec); e == nil && whyMap["storage"] != "" {
						baseErr = baseErr.WithProperty(models.ErrPropertyWhyFloor, whyMap["storage"])
					}
				}
				return automa.FailureReport(stp, automa.WithError(baseErr))
			}
			return automa.SuccessReport(stp)
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Validating storage requirements")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Validating storage requirements")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Validating storage requirements")
		})
}

// NewNodeSafetyCheckWorkflow creates a safety check workflow for any node type.
// If skipHardwareChecks is true, hardware validation steps (OS, CPU, memory, storage) are excluded.
func NewNodeSafetyCheckWorkflow(spec hardware.DeploymentSpec, skipHardwareChecks bool) *automa.WorkflowBuilder {
	preflightSteps := []automa.Builder{
		CheckPrivilegesStep(),
		CheckWeaverUserStep(),
		CheckHostProfileStep(spec),
	}

	if skipHardwareChecks {
		logx.As().Warn().Msg("Hardware validation steps (OS, CPU, memory, storage) will be skipped due to --skip-hardware-checks flag")
	} else {
		preflightSteps = append(preflightSteps,
			CheckOSStep(spec),
			CheckCPUStep(spec),
			CheckMemoryStep(spec),
			CheckStorageStep(spec),
		)
	}

	return automa.NewWorkflowBuilder().
		WithId(spec.NodeType + "-node-preflight").
		Steps(preflightSteps...).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().PhaseStart(ctx, stp, "Preflight Checks")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().PhaseFailure(ctx, stp, rpt, "Preflight Checks")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().PhaseCompletion(ctx, stp, rpt, "Preflight Checks")
		})
}

// CheckSubstrateHardwareStep validates the Kubernetes substrate hardware floor
// (OS, CPU, memory, storage) in a single step, independent of any workload node
// type or profile. It resolves requirements from the "k8s-substrate" provider via
// hardware.CreateSubstrateSpec, which bypasses the node-type/profile validation gates.
func CheckSubstrateHardwareStep() automa.Builder {
	return automa.NewStepBuilder().WithId("validate-substrate-hardware").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			hostProfile := hardware.GetHostProfile()
			substrateSpec, err := hardware.CreateSubstrateSpec(hostProfile)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errorx.Decorate(err, "failed to resolve Kubernetes substrate requirements").
						WithProperty(doctor.ErrPropertyResolution, []string{
							"This is an internal error: verify the \"k8s-substrate\" requirements provider is registered.",
						})))
			}

			reqs := substrateSpec.GetBaselineRequirements()
			logx.As().Info().Msgf("substrate floor — required: %d cores, %d GB RAM, %d GB disk, OS %v",
				reqs.MinCpuCores, reqs.MinMemoryGB, reqs.MinStorageGB, reqs.MinSupportedOS)

			// Resolve the why-map once for operator-facing attribution on failure.
			whyMap := map[string]string{}
			if p, ok := hardware.Providers()[hardware.NodeTypeSubstrate]; ok {
				if _, w, e := p.ComputeWithWhy(hardware.DeploymentSpec{NodeType: hardware.NodeTypeSubstrate}); e == nil {
					whyMap = w
				}
			}

			resolution := []string{
				fmt.Sprintf("Provision a host that meets the Kubernetes control-plane minimum: %d CPU cores, %d GB RAM, %d GB free disk.",
					reqs.MinCpuCores, reqs.MinMemoryGB, reqs.MinStorageGB),
				"Re-run with --skip-hardware-checks to bypass substrate validation (not recommended).",
			}

			checks := []struct {
				validate func() error
				why      string
				what     string
			}{
				{substrateSpec.ValidateOS, "", "OS"},
				{substrateSpec.ValidateCPU, whyMap["cpu"], "CPU"},
				{substrateSpec.ValidateMemory, whyMap["memory"], "memory"},
				{substrateSpec.ValidateStorage, whyMap["storage"], "storage"},
			}
			for _, c := range checks {
				if err := c.validate(); err != nil {
					baseErr := errorx.IllegalState.Wrap(err, "%s validation failed for Kubernetes substrate", c.what).
						WithProperty(doctor.ErrPropertyResolution, resolution)
					if c.why != "" {
						baseErr = baseErr.WithProperty(models.ErrPropertyWhyFloor, c.why)
					}
					return automa.FailureReport(stp, automa.WithError(baseErr))
				}
			}
			return automa.SuccessReport(stp)
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Validating Kubernetes substrate hardware")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Validating Kubernetes substrate hardware")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Validating Kubernetes substrate hardware")
		})
}

// NewSubstrateSafetyCheckWorkflow creates a safety check workflow for the Kubernetes
// substrate — the hardware floor Kubernetes itself needs, independent of any workload
// node type or profile. Unlike NewNodeSafetyCheckWorkflow it omits CheckHostProfileStep
// (there is no node type / profile to validate) and validates hardware via the single
// CheckSubstrateHardwareStep. If skipHardwareChecks is true, hardware validation is excluded.
func NewSubstrateSafetyCheckWorkflow(skipHardwareChecks bool) *automa.WorkflowBuilder {
	preflightSteps := []automa.Builder{
		CheckPrivilegesStep(),
		CheckWeaverUserStep(),
	}

	if skipHardwareChecks {
		logx.As().Warn().Msg("Substrate hardware validation (OS, CPU, memory, storage) will be skipped due to --skip-hardware-checks flag")
	} else {
		preflightSteps = append(preflightSteps, CheckSubstrateHardwareStep())
	}

	return automa.NewWorkflowBuilder().
		WithId("substrate-preflight").
		Steps(preflightSteps...).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().PhaseStart(ctx, stp, "Preflight Checks")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().PhaseFailure(ctx, stp, rpt, "Preflight Checks")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().PhaseCompletion(ctx, stp, rpt, "Preflight Checks")
		})
}
