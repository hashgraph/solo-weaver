// SPDX-License-Identifier: Apache-2.0

package common

import (
	"fmt"

	"github.com/hashgraph/solo-weaver/pkg/deps"
)

// Flag descriptor factories for the `teleport cluster` command tree.
//
// FlagTeleportVersion is named with a subject prefix to disambiguate from the
// root --version flag (which is a bool). FlagTeleportValuesFile is similar to
// common.FlagValuesFile but has no shorthand and a teleport-specific description.

func FlagTeleportVersion() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "version",
		ShortName:   "",
		Description: fmt.Sprintf("Teleport Helm chart version (default: %s)", deps.TELEPORT_VERSION),
		Default:     "",
	}
}

func FlagTeleportValuesFile() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "values",
		ShortName:   "",
		Description: "Path to Teleport Helm values file (required)",
		Default:     "",
	}
}
