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
	"github.com/hashgraph/solo-weaver/pkg/security"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/pkg/hardware"
	"github.com/joomcode/errorx"
)

// createNodeSpec creates the appropriate node spec based on node type, profile and host profile
func createNodeSpec(nodeType string, profile string, hostProfile hardware.HostProfile) (hardware.Spec, error) {
	return hardware.CreateNodeSpec(nodeType, profile, hostProfile)
}

// CheckHostProfileStep retrieves host profile and validates node type and profile
func CheckHostProfileStep(nodeType string, profile string) automa.Builder {
	return automa.NewStepBuilder().WithId("validate-host-profile").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			// Use the new HostProfile abstraction
			hostProfile := hardware.GetHostProfile()
			logx.As().Info().Interface("host_profile", hostProfile.String()).Msg("Retrieved host profile")

			// Validate node type is supported using centralized validation
			if !hardware.IsValidNodeType(nodeType) {
				return automa.FailureReport(stp,
					automa.WithError(
						errorx.IllegalArgument.New("unsupported node type: %q. Supported types: %v",
							nodeType, hardware.SupportedNodeTypes())))
			}

			// Validate profile
			if !hardware.IsValidProfile(profile) {
				return automa.FailureReport(stp,
					automa.WithError(
						errorx.IllegalArgument.New("unsupported profile: %q. Supported profiles: %v",
							profile, hardware.SupportedProfiles())))
			}

			logx.As().Info().
				Str("node_type", nodeType).
				Str("profile", profile).
				Msg("Host profile retrieved, node type and profile validated")
			return automa.SuccessReport(stp)
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			logx.As().Info().Msg("Starting host profile validation")
			return ctx, nil
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
			notify.As().StepStart(ctx, stp, "Starting privilege validation")
			return ctx, nil

		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Privilege validation failed")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Privilege validation step completed successfully")
		})
}

// CheckWeaverUserStep validates that the provisioner user and group exist with the correct IDs
func CheckWeaverUserStep() automa.Builder {
	return automa.NewStepBuilder().WithId("validate-weaver-user").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			weaverUsername := security.ServiceAccountUserName()
			weaverUserId := security.ServiceAccountUserId()
			weaverGroupName := security.ServiceAccountGroupName()
			weaverGroupId := security.ServiceAccountGroupId()

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
					instructions = fmt.Sprintf("The user '%s' and group '%s' do not exist.\n\n", weaverUsername, weaverGroupName)
					instructions += "Please create them with the following commands:\n\n"
					instructions += fmt.Sprintf("  sudo groupadd -g %s %s\n", weaverGroupId, weaverGroupName)
					instructions += fmt.Sprintf("  sudo useradd -u %s -g %s -m -s /bin/bash %s\n\n", weaverUserId, weaverGroupId, weaverUsername)
					instructions += "These commands will:\n"
					instructions += fmt.Sprintf("  • Create group '%s' with GID %s\n", weaverGroupName, weaverGroupId)
					instructions += fmt.Sprintf("  • Create user '%s' with UID %s\n", weaverUsername, weaverUserId)
					instructions += "  • Create a home directory (-m)\n"
					instructions += "  • Set bash as the default shell"
				} else if !userExists {
					instructions = fmt.Sprintf("The user '%s' does not exist.\n\n", weaverUsername)
					instructions += "Please create it with the following command:\n\n"
					instructions += fmt.Sprintf("  sudo useradd -u %s -g %s -m -s /bin/bash %s\n\n", weaverUserId, weaverGroupId, weaverUsername)
					instructions += "This will create the user with:\n"
					instructions += fmt.Sprintf("  • UID: %s\n", weaverUserId)
					instructions += fmt.Sprintf("  • Primary group: %s (GID %s)\n", weaverGroupName, weaverGroupId)
					instructions += "  • Home directory with bash shell"
				} else {
					instructions = fmt.Sprintf("The provisioner group '%s' does not exist.\n\n", weaverGroupName)
					instructions += "Please create it with the following command:\n\n"
					instructions += fmt.Sprintf("  sudo groupadd -g %s %s\n\n", weaverGroupId, weaverGroupName)
					instructions += fmt.Sprintf("This will create the group with GID %s.", weaverGroupId)
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

			// Both user and group exist with correct IDs
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
			notify.As().StepStart(ctx, stp, "Starting Solo Provisioner user validation")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Solo Provisioner user validation failed")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Solo Provisioner user validation step completed successfully")
		})
}

// CheckOSStep validates OS requirements for a specific node type
func CheckOSStep(nodeType string, profile string) automa.Builder {
	return automa.NewStepBuilder().WithId("validate-os").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			hostProfile := hardware.GetHostProfile()
			nodeSpec, err := createNodeSpec(nodeType, profile, hostProfile)
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
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Starting OS validation")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "OS validation failed")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "OS validation step completed successfully")
		})
}

// CheckCPUStep validates CPU requirements for a specific node type
func CheckCPUStep(nodeType string, profile string) automa.Builder {
	return automa.NewStepBuilder().WithId("validate-cpu").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			hostProfile := hardware.GetHostProfile()
			nodeSpec, err := createNodeSpec(nodeType, profile, hostProfile)
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
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Starting CPU validation")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "CPU validation failed")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "CPU validation step completed successfully")
		})

}

// CheckMemoryStep validates memory requirements for a specific node type
func CheckMemoryStep(nodeType string, profile string) automa.Builder {
	return automa.NewStepBuilder().WithId("validate-memory").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			hostProfile := hardware.GetHostProfile()
			nodeSpec, err := createNodeSpec(nodeType, profile, hostProfile)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			if err := nodeSpec.ValidateMemory(); err != nil {
				return automa.FailureReport(stp, automa.WithError(errorx.IllegalState.Wrap(err, "memory validation failed")))
			}

			logx.As().Info().Str("node_type", nodeSpec.GetNodeType()).Msg("Memory requirements validated")
			return automa.SuccessReport(stp)
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Starting memory validation")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Memory validation failed")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Memory validation step completed successfully")
		})
}

// CheckStorageStep validates storage requirements for a specific node type
func CheckStorageStep(nodeType string, profile string) automa.Builder {
	return automa.NewStepBuilder().WithId("validate-storage").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			hostProfile := hardware.GetHostProfile()
			nodeSpec, err := createNodeSpec(nodeType, profile, hostProfile)
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
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Starting storage validation")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Storage validation failed")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Storage validation step completed successfully")
		})
}

// NewNodeSafetyCheckWorkflow creates a safety check workflow for any node type
func NewNodeSafetyCheckWorkflow(nodeType string, profile string) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().
		WithId(nodeType+"-node-preflight").Steps(
		CheckPrivilegesStep(),
		CheckWeaverUserStep(),
		CheckHostProfileStep(nodeType, profile),
		CheckOSStep(nodeType, profile),
		CheckCPUStep(nodeType, profile),
		CheckMemoryStep(nodeType, profile),
		CheckStorageStep(nodeType, profile),
		//CheckDockerStep(),
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Starting node preflight checks")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Node preflight checks failed")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Node preflight checks completed successfully")
		})
}
