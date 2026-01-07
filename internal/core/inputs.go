// SPDX-License-Identifier: Apache-2.0

package core

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/config"
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

type BlocknodeInputs struct {
	Profile      string
	Version      string
	Namespace    string
	ReleaseName  string
	ChartName    string
	ChartRepo    string
	ChartVersion string
	Storage      config.BlockNodeStorage
	ValuesFile   string
	ReuseValues  bool
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
func (c *BlocknodeInputs) Validate() error {
	if c.Profile != "" {
		if err := sanity.ValidateIdentifier(c.Profile); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid profile: %s", c.Profile)
		}

		// check profile must be one of AllProfiles()
		if sanity.Contains[string](c.Profile, AllProfiles()) == false {
			return errorx.IllegalArgument.New("invalid profile: %s", c.Profile)
		}
	}

	// Validate version if provided (semantic version)
	if c.Version != "" {
		if err := sanity.ValidateVersion(c.Version); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid version: %s", c.Version)
		}
	}

	// Validate namespace if provided (must be a valid Kubernetes identifier)
	if c.Namespace != "" {
		if err := sanity.ValidateIdentifier(c.Namespace); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid namespace: %s", c.Namespace)
		}
	}

	// Validate release name if provided (must be a valid Helm release identifier)
	if c.ReleaseName != "" {
		if err := sanity.ValidateIdentifier(c.ReleaseName); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid release name: %s", c.ReleaseName)
		}
	}

	// Validate chart name
	if c.ChartName != "" {
		if err := sanity.ValidateIdentifier(c.ChartName); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid chart-name: %s", c.ChartName)
		}
	}

	// Validate chart if provided (Helm chart reference: OCI, URL, or repo/chart)
	if c.ChartRepo != "" {
		if err := sanity.ValidateChartReference(c.ChartRepo); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid chart reference: %s", c.ChartRepo)
		}
	}

	// Validate chartVersion if provided (semantic version)
	if c.ChartVersion != "" {
		if err := sanity.ValidateVersion(c.ChartVersion); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid chart version: %s", c.ChartVersion)
		}
	}

	// Validate storage paths
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
			return errorx.IllegalArgument.New("values file is not validi [ input = %s, validated = %s",
				c.ValuesFile, validatedValuesFile)
		}
	}

	if err := c.Storage.Validate(); err != nil {
		return err
	}

	return nil
}
