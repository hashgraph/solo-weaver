// SPDX-License-Identifier: Apache-2.0

package common

import (
	"fmt"

	"github.com/hashgraph/solo-weaver/pkg/software"
)

// Flag descriptor factories for the `teleport cluster` command tree.
//
// FlagTeleportVersion is named with a subject prefix to disambiguate from the
// root --version flag (which is a bool). FlagTeleportValuesFile is similar to
// common.FlagValuesFile but has no shorthand and a teleport-specific description.

// teleportCatalogDefaultVersion reads the default Teleport chart version from
// the infrastructure catalog. The catalog is the source of truth for cluster
// chart versions; we surface the value in the flag's help text so operators
// can see what they'd get without the flag. If the catalog fails to load we
// return an empty string and let the install step surface a clearer error.
func teleportCatalogDefaultVersion() string {
	catalog, err := software.LoadInfrastructureCatalog()
	if err != nil {
		return ""
	}
	chart, err := catalog.GetClusterComponent("teleport-cluster-agent")
	if err != nil {
		return ""
	}
	v, _ := chart.GetDefaultVersion()
	return v
}

func FlagTeleportVersion() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "version",
		ShortName:   "",
		Description: fmt.Sprintf("Teleport Helm chart version (default: %s)", teleportCatalogDefaultVersion()),
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
