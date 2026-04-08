// SPDX-License-Identifier: Apache-2.0

package models

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
)

type UserInputs[T any] struct {
	Common CommonInputs
	Custom T
}

// WorkflowExecutionOptions defines options for setting up various components of the cluster
type WorkflowExecutionOptions struct {
	ExecutionMode automa.TypeMode
	RollbackMode  automa.TypeMode
}

type CommonInputs struct {
	Force            bool
	NodeType         string
	ExecutionOptions WorkflowExecutionOptions
}

type BlockNodeInputs struct {
	Profile            string
	Namespace          string
	Release            string // Helm release name
	Chart              string // Helm chart reference: OCI, URL, or repo/chart
	ChartName          string
	ChartVersion       string
	Storage            BlockNodeStorage
	ValuesFile         string
	ReuseValues        bool
	ResetStorage       bool
	SkipHardwareChecks bool
}

type ClusterInputs struct {
	Profile            string
	SkipHardwareChecks bool
}

type MachineInputs struct {
	Profile            string
	SkipHardwareChecks bool
}

func boolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// Validate validates all user inputs fields to ensure they are safe and secure.
func (u *UserInputs[T]) Validate() error {

	if err := u.Common.Validate(); err != nil {
		return err
	}

	// Try value receiver first, then pointer receiver (covers both method sets)
	if validator, ok := any(u.Custom).(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return err
		}
	}
	if validator, ok := any(&u.Custom).(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// Validate validates all common inputs fields to ensure they are safe and secure.
func (c *CommonInputs) Validate() error {
	if c.NodeType != "" {
		if sanity.Contains(c.NodeType, AllNodeTypes()) == false {
			return errorx.IllegalArgument.New("invalid node type: %s", c.NodeType)
		}
	}

	modes := AllExecutionModes()
	if sanity.Contains[automa.TypeMode](c.ExecutionOptions.ExecutionMode, modes) == false {
		return errorx.IllegalArgument.New("invalid execution mode: %s", c.ExecutionOptions.ExecutionMode)
	}
	if sanity.Contains[automa.TypeMode](c.ExecutionOptions.RollbackMode, modes) == false {
		return errorx.IllegalArgument.New("invalid rollback mode: %s", c.ExecutionOptions.RollbackMode)
	}

	return nil
}

// Validate validates all block node inputs fields to ensure they are safe and secure.
func (c *BlockNodeInputs) Validate() error {
	if c.Profile != "" {
		if err := sanity.ValidateIdentifier(c.Profile); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid profile: %s", c.Profile)
		}

		// check profile must be one of AllProfiles()
		if sanity.Contains[string](c.Profile, AllProfiles()) == false {
			return errorx.IllegalArgument.New("invalid profile: %s", c.Profile)
		}
	}

	// Validate namespace if provided (must be a valid Kubernetes identifier)
	if c.Namespace != "" {
		if err := sanity.ValidateIdentifier(c.Namespace); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid namespace: %s", c.Namespace)
		}
	}

	// Validate release name if provided (must be a valid Helm release identifier)
	if c.Release != "" {
		if err := sanity.ValidateIdentifier(c.Release); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid release name: %s", c.Release)
		}
	}

	// Validate chart name
	if c.ChartName != "" {
		if err := sanity.ValidateChartReference(c.ChartName); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid chart-name: %s", c.ChartName)
		}
	}

	// Validate chart if provided (Helm chart reference: OCI, URL, or repo/chart)
	if c.Chart != "" {
		if err := sanity.ValidateChartReference(c.Chart); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid chart reference: %s", c.Chart)
		}
	}

	// Validate chartVersion if provided (semantic version)
	if c.ChartVersion != "" {
		if err := sanity.ValidateVersion(c.ChartVersion); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid chart version: %s", c.ChartVersion)
		}
	}

	// Validate storage paths (sanitizes individual values that are present).
	// Completeness (basePath OR all individual paths) is validated later in
	// GetStoragePaths() after config and state values have been merged by the resolver.
	if err := c.Storage.Validate(); err != nil {
		return err
	}

	// Validate the values file path if provided
	if c.ValuesFile != "" {
		validatedValuesFile, err := sanity.ValidateInputFile(c.ValuesFile)
		if err != nil {
			return err
		}

		if validatedValuesFile != c.ValuesFile {
			return errorx.IllegalArgument.New("values file path is not valid: input=%s, validated=%s",
				c.ValuesFile, validatedValuesFile)
		}
	}

	return nil
}

func (c *ClusterInputs) Validate() error {
	if c.Profile != "" {
		if err := sanity.ValidateIdentifier(c.Profile); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid profile: %s", c.Profile)
		}

		// check profile must be one of AllProfiles()
		if sanity.Contains[string](c.Profile, AllProfiles()) == false {
			return errorx.IllegalArgument.New("invalid profile: %s", c.Profile)
		}
	}

	return nil
}

func (c *MachineInputs) Validate() error {
	if c.Profile != "" {
		if err := sanity.ValidateIdentifier(c.Profile); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid profile: %s", c.Profile)
		}

		// check profile must be one of AllProfiles()
		if sanity.Contains[string](c.Profile, AllProfiles()) == false {
			return errorx.IllegalArgument.New("invalid profile: %s", c.Profile)
		}
	}

	return nil
}

type TeleportNodeInputs struct {
	Token     string
	ProxyAddr string
}

func (c *TeleportNodeInputs) Validate() error {
	return nil
}

type TeleportClusterInputs struct {
	Version    string
	ValuesFile string
}

func (c *TeleportClusterInputs) Validate() error {
	if c.ValuesFile != "" {
		if _, err := sanity.ValidateInputFile(c.ValuesFile); err != nil {
			return err
		}
	}
	return nil
}
