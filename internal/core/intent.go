package core

type ActionType string

// All actions must be idempotent meaning it only applies if system is not at the desired end state of the action.
const (
	// ActionSetup sets up the base system required for further actions.
	// This includes installing dependencies, configuring system settings, etc.
	// It should be safe to run multiple times without adverse effects.
	ActionSetup ActionType = "setup"

	// ActionInstall installs the application or component on the system.
	// It should check if the application is already installed and skip installation if so.
	// Running this action multiple times should not result in multiple installations.
	ActionInstall ActionType = "install"

	// ActionUninstall removes the application or component from the system.
	// It should ensure that all related files and configurations are cleaned up.
	// Running this action multiple times should not cause errors if the application is already uninstalled.
	ActionUninstall ActionType = "uninstall"

	// ActionReset resets the system or application to a clean state as if it were newly installed.
	// It should remove any user data, configurations, or changes made after installation.
	// Running this action multiple times should consistently return the system to the clean state.
	ActionReset ActionType = "reset"

	// ActionDeploy deploys the application or component to the target cluster
	ActionDeploy ActionType = "deploy"

	// ActionDestroy removes the application or component from the target cluster
	ActionDestroy ActionType = "destroy"

	// ActionUpgrade upgrades the application or component to a newer version
	ActionUpgrade ActionType = "upgrade"

	// ActionMigrate migrates the application or component to a different environment or configuration
	ActionMigrate ActionType = "migrate"
)

type TargetType string

const (
	// TargetMachine represents individual machines/servers
	TargetMachine TargetType = "machine"

	// TargetSystem represents the overall system configuration in a machine
	// This may include multiple applications and services running on the machine
	TargetSystem TargetType = "system"

	// TargetApplication represents a single application on the system
	TargetApplication TargetType = "application"

	// TargetCluster represents a Kubernetes cluster runnig on the machine
	TargetCluster TargetType = "cluster"

	// TargetBlocknode represents a blocknode component of the Hedera network
	TargetBlocknode TargetType = "blocknode"

	// TargetConsensusNode represents a consensus node component of the Hedera network
	TargetConsensusNode TargetType = "consensus"

	// TargetMirrorNode represents a mirror node component of the Hedera network
	TargetMirrorNode TargetType = "mirrornode"

	// TargetRelayNode represents a relay node component of the Hedera network
	TargetRelayNode TargetType = "relaynode"

	// TargetOperator represents solo-operator component
	TargetOperator TargetType = "operator"
)

// allowedOperations maps each action to the valid target types it can be performed on.
var allowedOperations = map[ActionType][]TargetType{
	ActionSetup:     {TargetMachine, TargetSystem, TargetCluster},
	ActionReset:     {TargetMachine, TargetSystem, TargetCluster, TargetApplication},
	ActionInstall:   {TargetApplication},
	ActionUninstall: {TargetApplication},
	ActionDeploy:    {TargetBlocknode, TargetConsensusNode, TargetMirrorNode, TargetRelayNode, TargetOperator},
	ActionDestroy:   {TargetBlocknode, TargetConsensusNode, TargetMirrorNode, TargetRelayNode, TargetOperator},
	ActionUpgrade:   {TargetBlocknode, TargetConsensusNode, TargetMirrorNode, TargetRelayNode, TargetOperator},
	ActionMigrate:   {TargetSystem, TargetCluster, TargetBlocknode, TargetConsensusNode, TargetMirrorNode, TargetRelayNode, TargetOperator},
}

// Intent defines the desired action to be performed given certain parameters and configuration.
type Intent struct {
	Action     ActionType            `yaml:"action" json:"action"`
	Target     TargetType            `yaml:"target" json:"target"`
	Parameters map[string]*Parameter `yaml:"parameters" json:"parameters"`
}

// Clone creates a deep copy of the Intent.
func (i *Intent) Clone() *Intent {
	newIntent := Intent{
		Action:     i.Action,
		Parameters: make(map[string]*Parameter, len(i.Parameters)),
	}
	for k, v := range i.Parameters {
		newIntent.Parameters[k] = v.Clone()
	}

	return &newIntent
}

// IsValid checks if the Intent is valid based on allowed actions and targets.
// An intent is considered valid if:
// - The action is recognized.
// - The target is recognized.
// - The action-target combination is allowed.
//
// It returns true if the intent is valid, false otherwise.
// Even if an intent is valid, it may still be rejected during execution based on current state, configuration, or other factors.
func (i *Intent) IsValid() bool {
	if i.Action == "" || i.Target == "" {
		return false
	}

	if i.Parameters == nil {
		return false
	}

	allowedTargets, exists := allowedOperations[i.Action]
	if !exists {
		return false // Action not recognized
	}

	for _, t := range allowedTargets {
		if t == i.Target {
			return true // Valid action-target combination
		}
	}

	return false
}
