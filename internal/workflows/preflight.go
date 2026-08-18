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
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/pkg/hardware"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
)

// createNodeSpec creates the appropriate node spec based on a DeploymentSpec and host profile.
// The Kubernetes substrate is resolved via CreateSubstrateSpec, which bypasses the
// node-type/profile validation gates (the substrate is not a user-facing node type and
// carries no profile); everything else goes through the standard CreateNodeSpec path.
func createNodeSpec(spec hardware.DeploymentSpec, hostProfile hardware.HostProfile) (hardware.Spec, error) {
	if spec.NodeType == hardware.NodeTypeSubstrate {
		return hardware.CreateSubstrateSpec(hostProfile)
	}
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

			uidWrong := userExists && weaverUser.Uid != weaverUserId
			gidWrong := groupExists && weaverGroup.Gid != weaverGroupId

			// The message carries the observed vs expected IDs; the hints carry
			// only the commands that fix them.
			var mismatches []string

			if uidWrong {
				meta["user_exists"] = "true"
				meta["user_id_mismatch"] = "true"
				meta["expected_user_id"] = weaverUserId
				meta["actual_user_id"] = weaverUser.Uid
				mismatches = append(mismatches, fmt.Sprintf(
					"user %q has UID %s, expected %s", weaverUsername, weaverUser.Uid, weaverUserId))
			}

			if gidWrong {
				meta["group_exists"] = "true"
				meta["group_id_mismatch"] = "true"
				meta["expected_group_id"] = weaverGroupId
				meta["actual_group_id"] = weaverGroup.Gid
				mismatches = append(mismatches, fmt.Sprintf(
					"group %q has GID %s, expected %s", weaverGroupName, weaverGroup.Gid, weaverGroupId))
			}

			if len(mismatches) > 0 {
				var hints []string
				// groupmod first: the usermod -g below needs the target GID to exist.
				if gidWrong {
					hints = append(hints, fmt.Sprintf("sudo groupmod -g %s %s", weaverGroupId, weaverGroupName))
				}
				if uidWrong {
					hints = append(hints, fmt.Sprintf("sudo usermod -u %s -g %s %s", weaverUserId, weaverGroupId, weaverUsername))
				}
				hints = append(hints, "Update file ownerships under the provisioner directories to match the new IDs.")

				return automa.FailureReport(stp,
					automa.WithError(errx.Decorate(
						errorx.IllegalState.New("provisioner service account has incorrect IDs: %s",
							strings.Join(mismatches, "; ")),
						reasons.PreconditionNotMet, hints...)),
					automa.WithMetadata(meta))
			}

			// The service account is provisioned by the installer, so a missing
			// user or group is a "not installed" state, not something to fix by hand.
			if !userExists || !groupExists {
				meta["user_exists"] = fmt.Sprintf("%t", userExists)
				meta["group_exists"] = fmt.Sprintf("%t", groupExists)

				var errMsg string
				switch {
				case !userExists && !groupExists:
					errMsg = fmt.Sprintf("provisioner user %q and group %q do not exist", weaverUsername, weaverGroupName)
				case !userExists:
					errMsg = fmt.Sprintf("provisioner user %q does not exist", weaverUsername)
				default:
					errMsg = fmt.Sprintf("provisioner group %q does not exist", weaverGroupName)
				}

				return automa.FailureReport(stp,
					automa.WithError(errx.Decorate(
						errorx.IllegalState.New("%s", errMsg),
						reasons.NotInstalled,
						"sudo solo-provisioner install")),
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
					automa.WithError(errorx.IllegalState.Wrap(err, "OS validation failed").
						WithProperty(doctor.ErrPropertyResolution, []string{
							fmt.Sprintf("Install or upgrade to a supported OS: %v.", reqs.MinSupportedOS),
							"Or re-run with --skip-hardware-checks to bypass hardware validation (not recommended).",
						})))
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
				baseErr := automa.StepExecutionError.Wrap(err, "CPU validation failed").
					WithProperty(doctor.ErrPropertyResolution, []string{
						fmt.Sprintf("Provision a host with at least %d CPU cores.", reqs.MinCpuCores),
						"Or re-run with --skip-hardware-checks to bypass hardware validation (not recommended).",
					})
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
				baseErr := errorx.IllegalState.Wrap(err, "memory validation failed").
					WithProperty(doctor.ErrPropertyResolution, []string{
						fmt.Sprintf("Provision a host with at least %d GB of RAM.", reqs.MinMemoryGB),
						"Or re-run with --skip-hardware-checks to bypass hardware validation (not recommended).",
					})
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
				baseErr := errorx.IllegalState.Wrap(err, "storage validation failed").
					WithProperty(doctor.ErrPropertyResolution, []string{
						fmt.Sprintf("Provision a host with at least %d GB of total disk capacity.", reqs.MinStorageGB),
						"Or re-run with --skip-hardware-checks to bypass hardware validation (not recommended).",
					})
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

// NewSubstrateSafetyCheckWorkflow creates a safety check workflow for the Kubernetes
// substrate — the hardware floor Kubernetes itself needs, independent of any workload
// node type or profile. It reuses the same per-resource hardware steps as the node
// preflight (CheckOSStep, CheckCPUStep, CheckMemoryStep, CheckStorageStep) so each
// resource gets its own TUI entry and failure report; it only omits CheckHostProfileStep
// (there is no node type / profile to validate). The steps resolve the substrate floor
// via createNodeSpec -> CreateSubstrateSpec. If skipHardwareChecks is true, the hardware
// validation steps are excluded.
func NewSubstrateSafetyCheckWorkflow(skipHardwareChecks bool) *automa.WorkflowBuilder {
	spec := hardware.DeploymentSpec{NodeType: hardware.NodeTypeSubstrate}

	preflightSteps := []automa.Builder{
		CheckPrivilegesStep(),
		CheckWeaverUserStep(),
	}

	if skipHardwareChecks {
		logx.As().Warn().Msg("Substrate hardware validation (OS, CPU, memory, storage) will be skipped due to --skip-hardware-checks flag")
	} else {
		preflightSteps = append(preflightSteps,
			CheckOSStep(spec),
			CheckCPUStep(spec),
			CheckMemoryStep(spec),
			CheckStorageStep(spec),
		)
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
