// SPDX-License-Identifier: Apache-2.0

// Package labels provides a shared label profile registry used by both
// the alloy rendering engine (internal/alloy) and config validation (pkg/models).
package labels

import (
	"sort"
	"strings"
)

// LabelInput holds all runtime data sources available for label resolution.
// Each profile's Labels method picks only the fields it needs.
type LabelInput struct {
	ClusterName   string // cluster name (e.g. "lfh02-previewnet-blocknode")
	DeployProfile string // deployment profile / environment (e.g. "previewnet")
	MachineIP     string // host's primary IP address (may be empty if unavailable)
}

// Profiler defines the contract for a label profile.
// Each implementation returns the full set of labels for that profile,
// always including the "cluster" label.
//
// To add a new profile:
//  1. Copy ops.go to a new file (e.g. sre.go)
//  2. Implement Profiler (Name + Labels)
//  3. Register it via Register() in an init() function
type Profiler interface {
	// Name returns the canonical lowercase name of this profile (e.g. "ops").
	Name() string

	// Labels returns the complete set of labels for this profile.
	Labels(input LabelInput) map[string]string
}

// DefaultProfile is the label profile used when none is specified on a remote.
const DefaultProfile = "eng"

var registry = map[string]Profiler{}

// Register adds a label profile to the global registry.
// Called from init() in profile implementation files (e.g. ops.go).
func Register(p Profiler) {
	registry[p.Name()] = p
}

// IsValid checks whether the given name is a recognized label profile.
func IsValid(name string) bool {
	_, ok := registry[strings.ToLower(name)]
	return ok
}

// ValidNames returns all recognized label profile names (sorted).
func ValidNames() []string {
	names := make([]string, 0, len(registry))
	for k := range registry {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// Resolve returns the full label map for a given label profile.
// Uses DefaultProfile when labelProfile is empty.
func Resolve(labelProfile string, input LabelInput) map[string]string {
	if labelProfile == "" {
		labelProfile = DefaultProfile
	}

	if profiler, ok := registry[strings.ToLower(labelProfile)]; ok {
		return profiler.Labels(input)
	}

	return nil
}
