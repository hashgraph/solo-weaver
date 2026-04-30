// SPDX-License-Identifier: Apache-2.0

package models

import (
	"strconv"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
)

type UserInputs[T any] struct {
	Common CommonInputs
	Custom T
}

// Redacted returns a copy of UserInputs with sensitive fields masked.
// If the Custom type implements Redactable, its Redacted() method is called.
// Otherwise the inputs are returned as-is.
func (u UserInputs[T]) Redacted() UserInputs[T] {
	if r, ok := any(u.Custom).(Redactable[T]); ok {
		u.Custom = r.Redacted()
	} else if r, ok := any(&u.Custom).(Redactable[T]); ok {
		u.Custom = r.Redacted()
	}
	return u
}

// Redactable is implemented by input types that contain sensitive fields
// (e.g. tokens, passwords) to provide a safe-to-log copy.
type Redactable[T any] interface {
	Redacted() T
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

// Default retention thresholds for block node plugins.
// These match the Hiero Block Node defaults:
//   - Historic: 0 means no/unlimited retention (keep all historic blocks)
//   - Recent: 96000 blocks preserved on disk before deleting older files
const (
	DefaultHistoricRetention = "0"
	DefaultRecentRetention   = "96000"
)

type BlockNodeInputs struct {
	Profile             string
	Namespace           string
	Release             string // Helm release name
	Chart               string // Helm chart reference: OCI, URL, or repo/chart
	ChartName           string
	ChartVersion        string
	Storage             BlockNodeStorage
	ValuesFile          string
	ReuseValues         bool
	ResetStorage        bool
	SkipHardwareChecks  bool
	LoadBalancerEnabled bool   // true = inject metallb.io/address-pool annotation via Helm values
	HistoricRetention   string // FILES_HISTORIC_BLOCK_RETENTION_THRESHOLD (0 = unlimited)
	RecentRetention     string // FILES_RECENT_BLOCK_RETENTION_THRESHOLD (~96000 default)
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

		// check profile must be one of SupportedProfiles()
		if sanity.Contains[string](c.Profile, SupportedProfiles()) == false {
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

	// Validate retention thresholds (must be non-negative integers when set)
	if c.HistoricRetention != "" {
		n, err := strconv.ParseInt(c.HistoricRetention, 10, 64)
		if err != nil || n < 0 {
			return errorx.IllegalArgument.New("invalid historic retention threshold: %s (must be a non-negative integer)", c.HistoricRetention)
		}
	}
	if c.RecentRetention != "" {
		n, err := strconv.ParseInt(c.RecentRetention, 10, 64)
		if err != nil || n < 0 {
			return errorx.IllegalArgument.New("invalid recent retention threshold: %s (must be a non-negative integer)", c.RecentRetention)
		}
	}

	return nil
}

func (c *ClusterInputs) Validate() error {
	if c.Profile != "" {
		if err := sanity.ValidateIdentifier(c.Profile); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid profile: %s", c.Profile)
		}

		// check profile must be one of SupportedProfiles()
		if sanity.Contains[string](c.Profile, SupportedProfiles()) == false {
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

		// check profile must be one of SupportedProfiles()
		if sanity.Contains[string](c.Profile, SupportedProfiles()) == false {
			return errorx.IllegalArgument.New("invalid profile: %s", c.Profile)
		}
	}

	return nil
}

type TeleportNodeInputs struct {
	Token     string `json:"-" yaml:"-"`
	ProxyAddr string
}

func (c *TeleportNodeInputs) Validate() error {
	return nil
}

// Redacted returns a copy with the Token masked.
func (c TeleportNodeInputs) Redacted() TeleportNodeInputs {
	r := c
	if r.Token != "" {
		r.Token = "***"
	}
	return r
}

type TeleportClusterInputs struct {
	Version    string
	ValuesFile string
}

func (c *TeleportClusterInputs) Validate() error {
	if c.ValuesFile != "" {
		sanitizedPath, err := sanity.ValidateInputFile(c.ValuesFile)
		if err != nil {
			return err
		}
		if sanitizedPath != c.ValuesFile {
			return errorx.IllegalArgument.New("invalid values file path: %s", c.ValuesFile)
		}
	}
	return nil
}
