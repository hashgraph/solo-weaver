// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"os"

	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/joomcode/errorx"
)

// InstallCrioRegistriesConf installs the custom registries.conf with registry mirror configuration
// This enables CRI-O to use a local registry mirror for caching Kubernetes images
// This is typically called during integration test setup when cache proxy is available
func InstallCrioRegistriesConf() error {
	// Read the custom registries.conf template
	content, err := templates.Read("files/crio/registries.conf")
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to read registries.conf template")
	}

	// Write to the sandbox registries.conf.d directory
	err = os.WriteFile(software.GetRegistriesConfPath(), []byte(content), core.DefaultFilePerm)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to write custom registries.conf")
	}

	return nil
}
