// SPDX-License-Identifier: Apache-2.0

package helm

import (
	"os"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"
)

// This code is adapted from Helm v3 source code and examples: https://helm.sh/docs/sdk/examples/

// helmDriver is the Helm storage driver, default is "secrets"
var helmDriver string = os.Getenv("HELM_DRIVER")

func initActionConfig(settings *cli.EnvSettings, debug action.DebugLog) (*action.Configuration, error) {
	return initActionConfigList(settings, debug, false)
}

func initActionConfigList(settings *cli.EnvSettings, debug action.DebugLog, allNamespaces bool) (*action.Configuration, error) {

	actionConfig := new(action.Configuration)

	namespace := func() string {
		// For list action, you can pass an empty string instead of settings.Namespace() to list
		// all namespaces
		if allNamespaces {
			return ""
		}
		return settings.Namespace()
	}()

	if err := actionConfig.Init(
		settings.RESTClientGetter(),
		namespace,
		helmDriver,
		debug); err != nil {
		return nil, err
	}

	return actionConfig, nil
}

func newRegistryClient(settings *cli.EnvSettings) (*registry.Client, error) {
	opts := []registry.ClientOption{
		registry.ClientOptDebug(settings.Debug),
		registry.ClientOptEnableCache(true),
		registry.ClientOptWriter(os.Stderr),
		registry.ClientOptCredentialsFile(settings.RegistryConfig),
	}

	// Create a new registry client
	registryClient, err := registry.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	return registryClient, nil
}
