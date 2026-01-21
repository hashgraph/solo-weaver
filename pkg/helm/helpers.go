// SPDX-License-Identifier: Apache-2.0

package helm

import (
	"os"
	"path/filepath"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/registry"
)

// This code is adapted from Helm v3 source code and examples: https://helm.sh/docs/sdk/examples/

// helmDriver is the Helm storage driver, default is "secrets"
var helmDriver string = os.Getenv("HELM_DRIVER")

func init() {
	// Fix for fieldManager length issue - Kubernetes API rejects field managers > 128 bytes.
	// When running via `go test` or IntelliJ, os.Args[0] can be a very long temp path like
	// "/private/var/folders/.../go_build_github_com_hashgraph_solo_weaver_..." which exceeds 128 bytes.
	// We truncate to ensure it stays under the limit while preserving as much useful info as possible.
	kube.ManagedFieldsManager = truncateFieldManager(os.Args[0], 128)
}

// truncateFieldManager ensures the field manager name doesn't exceed maxLen bytes.
// It uses the base name of the path and truncates if necessary.
func truncateFieldManager(path string, maxLen int) string {
	if path == "" {
		return "helm"
	}

	// Use base name to get a shorter, more meaningful name
	name := filepath.Base(path)

	// If still too long, truncate from the beginning to keep the end (usually more meaningful)
	if len(name) > maxLen {
		name = name[len(name)-maxLen:]
	}

	return name
}

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
