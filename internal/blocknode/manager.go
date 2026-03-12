// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"
	oslib "os"
	"path"
	"time"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/cli/values"
)

const (
	// Kubernetes resources
	ResourceNameSuffix = "-block-node-server"
	PodLabelSelector   = "app.kubernetes.io/name=block-node-server"

	// Template paths
	NamespacePath       = "files/block-node/namespace.yaml"
	StorageConfigPath   = "files/block-node/storage-config.yaml"
	OptionalStoragePath = "files/block-node/optional-storage.yaml"
	ValuesPath          = "files/block-node/full-values.yaml"
	NanoValuesPath      = "files/block-node/nano-values.yaml"

	// Timeouts
	PodReadyTimeoutSeconds = 300
)

// Manager handles block node setup and management operations
type Manager struct {
	fsManager   fsx.Manager
	helmManager helm.Manager
	kubeClient  *kube.Client
	logger      *zerolog.Logger
	blockConfig models.BlockNodeInputs
}

// NewManager creates a new block node manager
func NewManager(blockConfig models.BlockNodeInputs) (*Manager, error) {
	l := logx.As()

	// File system manager
	fsManager, err := fsx.NewManager()
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create file system manager")
	}

	// Helm manager
	helmManager, err := helm.NewManager(helm.WithLogger(*l))
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create helm manager")
	}

	// Kubernetes client
	kubeClient, err := kube.NewClient()
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create kubernetes client")
	}

	return &Manager{
		fsManager:   fsManager,
		helmManager: helmManager,
		kubeClient:  kubeClient,
		logger:      l,
		blockConfig: blockConfig,
	}, nil
}

// SetupStorage creates the required directories for block node storage
func (m *Manager) SetupStorage(ctx context.Context) error {
	// Get storage paths (already validated by GetStoragePaths)
	archivePath, livePath, logPath, optionalPaths, err := m.GetStoragePaths()
	if err != nil {
		return err
	}

	// Core storage paths are always required
	storagePaths := []string{
		archivePath,
		livePath,
		logPath,
	}

	// Append applicable optional storage paths
	for _, p := range optionalPaths {
		if p != "" {
			storagePaths = append(storagePaths, p)
		}
	}

	for _, dirPath := range storagePaths {
		_, exists, err := m.fsManager.PathExists(dirPath)
		if err != nil {
			return errorx.IllegalState.Wrap(err, "failed to check path existence: %s", dirPath)
		}

		if exists {
			m.logger.Info().Str("path", dirPath).Msg("Directory already exists, skipping")
			continue
		}

		if err := m.fsManager.CreateDirectory(dirPath, true); err != nil {
			return errorx.IllegalState.Wrap(err, "failed to create directory: %s", dirPath)
		}

		if err := m.fsManager.WritePermissions(dirPath, models.DefaultDirOrExecPerm, true); err != nil {
			return errorx.IllegalState.Wrap(err, "failed to set permissions on: %s", dirPath)
		}
	}

	return nil
}

// CreateNamespace creates the block-node namespace if it doesn't exist
func (m *Manager) CreateNamespace(ctx context.Context, tempDir string) error {
	// Prepare template data
	data := struct {
		Namespace string
	}{
		Namespace: m.blockConfig.Namespace,
	}

	// Render the namespace template
	namespaceContent, err := templates.Render(NamespacePath, data)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to render namespace template")
	}

	// Write to temp file
	manifestFilePath := path.Join(tempDir, "block-node-namespace.yaml")
	if err := oslib.WriteFile(manifestFilePath, []byte(namespaceContent), models.DefaultFilePerm); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to write namespace manifest to temp file")
	}

	// Apply the manifest - ApplyManifest is idempotent, so it will create or update
	if err := m.kubeClient.ApplyManifest(ctx, manifestFilePath); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to apply namespace manifest")
	}

	m.logger.Info().Msgf("Applied namespace manifest for: %s", m.blockConfig.Namespace)
	return nil
}

// DeleteNamespace deletes the block-node namespace
func (m *Manager) DeleteNamespace(ctx context.Context, tempDir string) error {
	manifestFilePath := path.Join(tempDir, "block-node-namespace.yaml")
	return m.kubeClient.DeleteManifest(ctx, manifestFilePath)
}

// CreatePersistentVolumes creates PVs and PVCs from the storage config
func (m *Manager) CreatePersistentVolumes(ctx context.Context, tempDir string) error {
	// Get the computed storage paths
	archivePath, livePath, logPath, optionalPaths, err := m.GetStoragePaths()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to get storage paths")
	}

	applicable := GetApplicableOptionalStorages(m.blockConfig.Version)
	includeVerification := false
	includePlugins := false
	verificationPath := ""
	pluginsPath := ""
	verificationSize := m.blockConfig.Storage.VerificationSize
	pluginsSize := m.blockConfig.Storage.PluginsSize

	for i, os := range applicable {
		switch os.Name {
		case "verification":
			includeVerification = true
			if i < len(optionalPaths) {
				verificationPath = optionalPaths[i]
			}
		case "plugins":
			includePlugins = true
			if i < len(optionalPaths) {
				pluginsPath = optionalPaths[i]
			}
		}
	}

	m.logger.Debug().
		Str("archivePath", archivePath).
		Str("livePath", livePath).
		Str("logPath", logPath).
		Str("verificationPath", verificationPath).
		Str("pluginsPath", pluginsPath).
		Bool("includeVerification", includeVerification).
		Bool("includePlugins", includePlugins).
		Msg("Storage paths computed")

	// Prepare template data
	data := struct {
		Namespace           string
		LivePath            string
		ArchivePath         string
		LogPath             string
		VerificationPath    string
		PluginsPath         string
		LiveSize            string
		ArchiveSize         string
		LogSize             string
		VerificationSize    string
		PluginsSize         string
		IncludeVerification bool
		IncludePlugins      bool
	}{
		Namespace:           m.blockConfig.Namespace,
		LivePath:            livePath,
		ArchivePath:         archivePath,
		LogPath:             logPath,
		VerificationPath:    verificationPath,
		PluginsPath:         pluginsPath,
		LiveSize:            m.blockConfig.Storage.LiveSize,
		ArchiveSize:         m.blockConfig.Storage.ArchiveSize,
		LogSize:             m.blockConfig.Storage.LogSize,
		VerificationSize:    verificationSize,
		PluginsSize:         pluginsSize,
		IncludeVerification: includeVerification,
		IncludePlugins:      includePlugins,
	}

	// Render the storage config template
	m.logger.Debug().Str("template", StorageConfigPath).Msg("Rendering storage config template")
	storageConfig, err := templates.Render(StorageConfigPath, data)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to render storage config template")
	}

	// Write to temp file
	configFilePath := path.Join(tempDir, "block-node-storage-config.yaml")
	m.logger.Debug().Str("configFile", configFilePath).Msg("Writing storage config to temp file")
	if err := oslib.WriteFile(configFilePath, []byte(storageConfig), models.DefaultFilePerm); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to write storage config to temp file")
	}

	// Apply the configuration
	m.logger.Debug().Str("configFile", configFilePath).Msg("Applying storage manifest")
	if err := m.kubeClient.ApplyManifest(ctx, configFilePath); err != nil {
		m.logger.Error().Err(err).Str("configFile", configFilePath).Msg("Failed to apply storage manifest")
		return errorx.IllegalState.Wrap(err, "failed to apply storage configuration")
	}
	m.logger.Debug().Msg("Storage manifest applied successfully")

	// Wait for all PVCs to be bound
	pvcNames := []string{"live-storage-pvc", "archive-storage-pvc", "logging-storage-pvc"}

	// Add PVCs for applicable optional storages
	for _, os := range applicable {
		pvcNames = append(pvcNames, os.PVCName)
	}

	timeout := 2 * time.Minute

	for _, pvcName := range pvcNames {
		m.logger.Info().Str("pvc", pvcName).Msg("Waiting for PVC to be bound...")
		if err := m.kubeClient.WaitForResource(ctx, kube.KindPVC, m.blockConfig.Namespace, pvcName, kube.IsPVCBound, timeout); err != nil {
			return errorx.IllegalState.Wrap(err, "PVC %s did not become bound in time", pvcName)
		}
		m.logger.Info().Str("pvc", pvcName).Msg("PVC is bound")
	}

	return nil
}

// DeletePersistentVolumes deletes PVs and PVCs
func (m *Manager) DeletePersistentVolumes(ctx context.Context, tempDir string) error {
	configFilePath := path.Join(tempDir, "block-node-storage-config.yaml")
	return m.kubeClient.DeleteManifest(ctx, configFilePath)
}

// CreateOptionalStorage creates a single optional PV/PVC using the unified template.
// Used during migration when other PVs already exist.
func (m *Manager) CreateOptionalStorage(ctx context.Context, tempDir string, optStor OptionalStorage) error {
	// Derive the storage path using the same logic as GetStoragePaths
	// (handles basePath derivation when individual paths are not set)
	storagePath, storageSize, err := m.resolveOptionalStoragePathAndSize(optStor)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to resolve %s storage path", optStor.Name)
	}

	data := struct {
		Namespace string
		PVName    string
		PVCName   string
		Path      string
		Size      string
	}{
		Namespace: m.blockConfig.Namespace,
		PVName:    optStor.PVName,
		PVCName:   optStor.PVCName,
		Path:      storagePath,
		Size:      storageSize,
	}

	m.logger.Debug().
		Str("name", optStor.Name).
		Str("path", storagePath).
		Str("size", storageSize).
		Msg("Creating optional storage")

	// Render the optional storage template
	storageConfig, err := templates.Render(OptionalStoragePath, data)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to render %s storage template", optStor.Name)
	}

	// Write to temp file
	configFilePath := path.Join(tempDir, "block-node-"+optStor.Name+"-storage.yaml")
	if err := oslib.WriteFile(configFilePath, []byte(storageConfig), models.DefaultFilePerm); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to write %s storage config", optStor.Name)
	}

	// Apply the configuration
	if err := m.kubeClient.ApplyManifest(ctx, configFilePath); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to apply %s storage", optStor.Name)
	}

	// Wait for PVC to be bound
	m.logger.Info().Str("pvc", optStor.PVCName).Msgf("Waiting for %s PVC to be bound...", optStor.Name)
	timeout := 2 * time.Minute
	if err := m.kubeClient.WaitForResource(ctx, kube.KindPVC, m.blockConfig.Namespace, optStor.PVCName, kube.IsPVCBound, timeout); err != nil {
		return errorx.IllegalState.Wrap(err, "%s PVC did not become bound in time", optStor.Name)
	}
	m.logger.Info().Str("pvc", optStor.PVCName).Msgf("%s PVC is bound", optStor.Name)

	return nil
}

// resolveOptionalStoragePathAndSize derives the effective path and size for an optional storage,
// using basePath derivation when the individual path is not explicitly configured.
func (m *Manager) resolveOptionalStoragePathAndSize(optStor OptionalStorage) (string, string, error) {
	storagePath := optStor.GetPath(&m.blockConfig.Storage)
	storageSize := optStor.GetSize(&m.blockConfig.Storage)

	// If individual path is not set, derive from basePath
	if storagePath == "" {
		basePath := m.blockConfig.Storage.BasePath
		if basePath != "" {
			sanitizedBase, err := sanity.SanitizePath(basePath)
			if err != nil {
				return "", "", errorx.IllegalArgument.Wrap(err, "invalid base storage path")
			}
			storagePath = path.Join(sanitizedBase, optStor.DirName)
		}
	}

	// Validate the path
	if storagePath != "" {
		sanitized, err := sanity.SanitizePath(storagePath)
		if err != nil {
			return "", "", errorx.IllegalArgument.Wrap(err, "invalid %s path", optStor.Name)
		}
		storagePath = sanitized
	} else {
		return "", "", errorx.IllegalArgument.New(
			"%s storage path is empty: set storage.basePath or the specific %s path flag",
			optStor.Name, optStor.Name,
		)
	}

	return storagePath, storageSize, nil
}

// InstallChart installs the block node helm chart
func (m *Manager) InstallChart(ctx context.Context, valuesFile string) (bool, error) {
	// Check if already installed
	isInstalled, err := m.helmManager.IsInstalled(m.blockConfig.Release, m.blockConfig.Namespace)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to check if block node is installed")
	}

	if isInstalled {
		m.logger.Info().Msg("Block Node is already installed, skipping installation")
		return false, nil
	}

	// Install the chart
	_, err = m.helmManager.InstallChart(
		ctx,
		m.blockConfig.Release,
		m.blockConfig.Chart,
		m.blockConfig.ChartVersion,
		m.blockConfig.Namespace,
		helm.InstallChartOptions{
			ValueOpts: &values.Options{
				ValueFiles: []string{valuesFile},
			},
			CreateNamespace: false, // namespace already created
			Atomic:          true,
			Wait:            true,
			Timeout:         helm.DefaultTimeout,
		},
	)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to install block node chart")
	}

	return true, nil
}

// UninstallChart uninstalls the block node helm chart
func (m *Manager) UninstallChart(ctx context.Context) error {
	return m.helmManager.UninstallChart(m.blockConfig.Release, m.blockConfig.Namespace)
}

// UpgradeChart upgrades the block node helm chart
func (m *Manager) UpgradeChart(ctx context.Context, valuesFile string, reuseValues bool) error {
	// Check if installed first
	isInstalled, err := m.helmManager.IsInstalled(m.blockConfig.Release, m.blockConfig.Namespace)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to check if block node is installed")
	}

	if !isInstalled {
		return errorx.IllegalState.New("block node is not installed, cannot upgrade. Use 'install' command instead")
	}

	// Prepare value files slice
	// When reuseValues is true and no custom values file is provided (valuesFile is empty),
	// pass an empty ValueFiles slice to allow pure value reuse following Helm CLI convention.
	// This avoids merging default template values on top of existing release values.
	var valueFiles []string
	if valuesFile != "" {
		valueFiles = []string{valuesFile}
	} else if !reuseValues {
		// If not reusing values and no values file provided, this is an error condition
		// as we need either existing values or new values
		return errorx.IllegalArgument.New("no values file provided and --no-reuse-values is set")
	}

	// Upgrade the chart
	_, err = m.helmManager.UpgradeChart(
		ctx,
		m.blockConfig.Release,
		m.blockConfig.Chart,
		m.blockConfig.ChartVersion,
		m.blockConfig.Namespace,
		helm.UpgradeChartOptions{
			ValueOpts: &values.Options{
				ValueFiles: valueFiles,
			},
			ReuseValues: reuseValues,
			Atomic:      true,
			Wait:        true,
			Timeout:     helm.DefaultTimeout,
		},
	)
	if err != nil {
		m.logger.Error().
			Err(err).
			Str("chart", m.blockConfig.Chart).
			Str("version", m.blockConfig.ChartVersion).
			Str("namespace", m.blockConfig.Namespace).
			Msg("Helm upgrade failed")
		return errorx.IllegalState.Wrap(err, "failed to upgrade block node chart")
	}

	return nil
}

// DeleteStatefulSetForUpgrade deletes the block node StatefulSet using orphan cascading
// and waits for it to be fully removed from the API server. Orphan cascading removes
// the controller object but keeps pods running. This is required before any Helm upgrade
// that changes volumeClaimTemplates, since Kubernetes forbids in-place updates to those fields.
// Returns nil if the StatefulSet doesn't exist.
func (m *Manager) DeleteStatefulSetForUpgrade(ctx context.Context) error {
	stsName := m.blockConfig.Release + ResourceNameSuffix

	m.logger.Info().
		Str("statefulset", stsName).
		Msg("Deleting StatefulSet (orphan cascade) to allow volumeClaimTemplates update")

	if err := m.kubeClient.DeleteStatefulSet(ctx, m.blockConfig.Namespace, stsName); err != nil {
		m.logger.Warn().Err(err).Str("statefulset", stsName).Msg("Failed to delete StatefulSet before upgrade")
		return errorx.ExternalError.Wrap(err, "failed to delete StatefulSet before upgrade")
	}

	// Wait for the StatefulSet to be fully removed from the API server before upgrading.
	// Polling avoids the race where Helm patches a stale StatefulSet.
	m.logger.Info().Str("statefulset", stsName).Msg("Waiting for StatefulSet deletion to complete")
	waitTimeout := 60 * time.Second
	if err := m.kubeClient.WaitForResource(ctx, kube.KindStatefulSet, m.blockConfig.Namespace, stsName, kube.IsDeleted, waitTimeout); err != nil {
		m.logger.Warn().Err(err).Str("statefulset", stsName).Msg("Timeout waiting for StatefulSet deletion, proceeding with upgrade attempt")
	}

	return nil
}

// ScaleStatefulSet scales the block node statefulset to the specified number of replicas
func (m *Manager) ScaleStatefulSet(ctx context.Context, replicas int32) error {
	resourceName := m.blockConfig.Release + ResourceNameSuffix

	m.logger.Info().
		Str("statefulset", resourceName).
		Int32("replicas", replicas).
		Msg("Scaling block node statefulset")

	if err := m.kubeClient.ScaleStatefulSet(ctx, m.blockConfig.Namespace, resourceName, replicas); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to scale statefulset: %s", resourceName)
	}

	return nil
}

// WaitForPodsTerminated waits until all pods matching the block node label selector are terminated
func (m *Manager) WaitForPodsTerminated(ctx context.Context) error {
	m.logger.Info().Msg("Waiting for Block Node pods to terminate...")

	timeout := time.Duration(PodReadyTimeoutSeconds) * time.Second
	opts := kube.WaitOptions{
		LabelSelector: PodLabelSelector,
	}

	// Wait until no pods exist with the block node label
	if err := m.kubeClient.WaitForResourcesDeletion(ctx, kube.KindPod, m.blockConfig.Namespace, timeout, opts); err != nil {
		return errorx.IllegalState.Wrap(err, "pods did not terminate in time")
	}

	return nil
}

// ClearStorageDirectory removes all files and subdirectories from a storage directory
// while preserving the directory itself
func (m *Manager) ClearStorageDirectory(dirPath string) error {
	m.logger.Info().Str("path", dirPath).Msg("Clearing storage directory")

	_, exists, err := m.fsManager.PathExists(dirPath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to check path existence: %s", dirPath)
	}

	if !exists {
		m.logger.Debug().Str("path", dirPath).Msg("Directory does not exist, skipping")
		return nil
	}

	// Remove all contents of the directory
	if err := m.fsManager.RemoveContents(dirPath); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to clear directory contents: %s", dirPath)
	}

	return nil
}

// ResetStorage clears all block node storage directories
func (m *Manager) ResetStorage(ctx context.Context) error {
	archivePath, livePath, logPath, optionalPaths, err := m.GetStoragePaths()
	if err != nil {
		return err
	}

	// Clear core storage paths
	storagePaths := []string{
		archivePath,
		livePath,
		logPath,
	}

	// Append applicable optional storage paths
	for _, p := range optionalPaths {
		if p != "" {
			storagePaths = append(storagePaths, p)
		}
	}

	for _, dirPath := range storagePaths {
		if err := m.ClearStorageDirectory(dirPath); err != nil {
			return err
		}
	}

	m.logger.Info().Msg("All storage directories cleared successfully")
	return nil
}

// AnnotateService annotates the block node service with MetalLB address pool
func (m *Manager) AnnotateService(ctx context.Context) error {
	resourceName := m.blockConfig.Release + ResourceNameSuffix

	annotations := map[string]string{
		"metallb.io/address-pool": "public-address-pool",
	}

	if err := m.kubeClient.AnnotateResource(ctx, kube.KindService, m.blockConfig.Namespace, resourceName, annotations); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to annotate service: %s", resourceName)
	}

	return nil
}

// WaitForPodReady waits for the block node pod to be ready
func (m *Manager) WaitForPodReady(ctx context.Context) error {
	m.logger.Info().Msg("Waiting for Block Node pod to be ready...")

	timeout := time.Duration(PodReadyTimeoutSeconds) * time.Second
	opts := kube.WaitOptions{
		LabelSelector: PodLabelSelector,
	}

	if err := m.kubeClient.WaitForResources(ctx, kube.KindPod, m.blockConfig.Namespace, kube.IsPodReady, timeout, opts); err != nil {
		return errorx.IllegalState.Wrap(err, "pod did not become ready in time")
	}

	return nil
}

// ComputeValuesFile generates the values file for helm installation based on profile and version.
// It provides the path to the generated values file.
//
// The method renders the appropriate values template with conditional sections based on
// which optional storages the target Block Node version requires.
//
// When a custom values file is provided, this method injects/overrides the persistence
// settings to ensure create: false and existingClaim are set for each storage type.
// This is critical because weaver manages PVs/PVCs outside of Helm, so the chart must
// always reference the pre-created PVCs rather than attempting to create its own.
//
// NOTE: This method implements defense-in-depth validation. Even though the CLI layer
// validates paths using sanity.ValidateInputFile(), this method also validates to ensure
// safety regardless of the caller.
func (m *Manager) ComputeValuesFile(profile string, valuesFile string) (string, error) {
	var valuesContent []byte
	var err error

	if valuesFile == "" {
		// Determine which optional storages are needed
		applicable := GetApplicableOptionalStorages(m.blockConfig.Version)
		includeVerification := false
		includePlugins := false
		for _, optStor := range applicable {
			switch optStor.Name {
			case "verification":
				includeVerification = true
			case "plugins":
				includePlugins = true
			}
		}

		// Select the base template based on profile
		valuesTemplatePath := ValuesPath
		if profile == models.ProfileLocal {
			valuesTemplatePath = NanoValuesPath
			logx.As().Info().
				Bool("includeVerification", includeVerification).
				Bool("includePlugins", includePlugins).
				Msg("Using nano values configuration for local profile")
		} else {
			logx.As().Info().
				Bool("includeVerification", includeVerification).
				Bool("includePlugins", includePlugins).
				Msg("Using full values configuration")
		}

		// Render the Go-templated values file with conditional storage sections
		templateData := struct {
			IncludeVerification bool
			IncludePlugins      bool
		}{
			IncludeVerification: includeVerification,
			IncludePlugins:      includePlugins,
		}

		rendered, renderErr := templates.Render(valuesTemplatePath, templateData)
		if renderErr != nil {
			return "", errorx.InternalError.Wrap(renderErr, "failed to render block node values template")
		}
		valuesContent = []byte(rendered)
	} else {
		// Defense-in-depth: validate even though CLI layer already validated
		sanitizedPath, err := sanity.ValidateInputFile(valuesFile)
		if err != nil {
			return "", err
		}

		valuesContent, err = oslib.ReadFile(sanitizedPath)
		if err != nil {
			return "", errorx.InternalError.Wrap(err, "failed to read provided values file: %s", sanitizedPath)
		}

		logx.As().Info().Str("path", sanitizedPath).Msg("Using custom values file")

		// Inject/override persistence settings to ensure weaver-managed PVCs are used.
		// Since weaver creates PVs and PVCs outside of Helm, the chart must always use
		// create: false with existingClaim pointing to the pre-created PVCs.
		valuesContent, err = m.injectPersistenceOverrides(valuesContent)
		if err != nil {
			return "", errorx.InternalError.Wrap(err, "failed to inject persistence overrides into custom values file")
		}
	}

	// Write temporary copy to weaver's temp directory
	valuesFilePath := path.Join(models.Paths().TempDir, "block-node-values.yaml")
	if err = oslib.WriteFile(valuesFilePath, valuesContent, models.DefaultFilePerm); err != nil {
		return "", errorx.InternalError.Wrap(err, "failed to write block node values file")
	}

	return valuesFilePath, nil
}

// persistenceEntry represents the required persistence settings for a storage type.
type persistenceEntry struct {
	name      string
	claimName string
}

// injectPersistenceOverrides parses a user-provided values YAML and ensures that
// all applicable persistence entries have create: false and existingClaim set.
// This prevents the Helm chart from creating its own PVCs that would conflict
// with the PVs/PVCs managed by weaver, which causes the Pending PVC issue.
func (m *Manager) injectPersistenceOverrides(valuesContent []byte) ([]byte, error) {
	var vals map[string]interface{}
	if err := yaml.Unmarshal(valuesContent, &vals); err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "failed to parse values YAML")
	}

	// Core persistence entries that always need overriding
	entries := []persistenceEntry{
		{name: "live", claimName: "live-storage-pvc"},
		{name: "archive", claimName: "archive-storage-pvc"},
		{name: "logging", claimName: "logging-storage-pvc"},
	}

	// Add applicable optional storage entries
	for _, optStor := range GetApplicableOptionalStorages(m.blockConfig.Version) {
		entries = append(entries, persistenceEntry{
			name:      optStor.Name,
			claimName: optStor.PVCName,
		})
	}

	// Navigate to blockNode.persistence, creating the path if needed
	blockNode, ok := vals["blockNode"].(map[string]interface{})
	if !ok {
		blockNode = make(map[string]interface{})
		vals["blockNode"] = blockNode
	}

	persistence, ok := blockNode["persistence"].(map[string]interface{})
	if !ok {
		persistence = make(map[string]interface{})
		blockNode["persistence"] = persistence
	}

	// Override each entry
	for _, entry := range entries {
		existing, ok := persistence[entry.name].(map[string]interface{})
		if !ok {
			existing = make(map[string]interface{})
		}

		// Check if we need to override
		needsOverride := false
		if create, exists := existing["create"]; !exists || create != false {
			needsOverride = true
		}
		if claim, exists := existing["existingClaim"]; !exists || claim != entry.claimName {
			needsOverride = true
		}

		if needsOverride {
			logx.As().Warn().
				Str("storageType", entry.name).
				Str("existingClaim", entry.claimName).
				Msg("Overriding persistence settings in custom values file: setting create=false and existingClaim (weaver manages PVs/PVCs)")
		}

		existing["create"] = false
		existing["existingClaim"] = entry.claimName
		if _, exists := existing["subPath"]; !exists {
			existing["subPath"] = ""
		}

		persistence[entry.name] = existing
	}

	// Marshal back to YAML
	result, err := yaml.Marshal(vals)
	if err != nil {
		return nil, errorx.InternalError.Wrap(err, "failed to marshal values YAML after persistence override")
	}

	return result, nil
}

// GetStoragePaths returns the computed storage paths based on configuration.
// If individual paths are specified, they are used; otherwise, paths are derived from basePath.
// All paths are validated using sanity checks.
// optionalPaths contains the paths for applicable optional storages (in registry order).
func (m *Manager) GetStoragePaths() (archivePath, livePath, logPath string, optionalPaths []string, err error) {
	archivePath = m.blockConfig.Storage.ArchivePath
	livePath = m.blockConfig.Storage.LivePath
	logPath = m.blockConfig.Storage.LogPath

	applicable := GetApplicableOptionalStorages(m.blockConfig.Version)

	// Sanitize basePath before using it to construct other paths
	basePath := m.blockConfig.Storage.BasePath
	if basePath != "" {
		basePath, err = sanity.SanitizePath(basePath)
		if err != nil {
			return "", "", "", nil, errorx.IllegalArgument.Wrap(err, "invalid base storage path")
		}
	}

	// Check if any core or optional paths need to be derived from basePath
	corePathsMissing := archivePath == "" || livePath == "" || logPath == ""
	optionalPathMissing := false
	for _, optStor := range applicable {
		if optStor.GetPath(&m.blockConfig.Storage) == "" {
			optionalPathMissing = true
			break
		}
	}

	if basePath == "" && (corePathsMissing || optionalPathMissing) {
		return "", "", "", nil, errorx.IllegalArgument.New("at least one storage path is not set and base storage path is empty")
	}

	if archivePath == "" {
		archivePath = path.Join(basePath, "archive")
	}
	if livePath == "" {
		livePath = path.Join(basePath, "live")
	}
	if logPath == "" {
		logPath = path.Join(basePath, "logs")
	}

	// Derive and validate optional storage paths
	for _, optStor := range applicable {
		p := optStor.GetPath(&m.blockConfig.Storage)
		if p == "" {
			p = path.Join(basePath, optStor.DirName)
		}
		optionalPaths = append(optionalPaths, p)
	}

	// Validate all paths using sanity checks
	archivePath, err = sanity.SanitizePath(archivePath)
	if err != nil {
		return "", "", "", nil, errorx.IllegalArgument.Wrap(err, "invalid archive path")
	}

	livePath, err = sanity.SanitizePath(livePath)
	if err != nil {
		return "", "", "", nil, errorx.IllegalArgument.Wrap(err, "invalid live path")
	}

	logPath, err = sanity.SanitizePath(logPath)
	if err != nil {
		return "", "", "", nil, errorx.IllegalArgument.Wrap(err, "invalid log path")
	}

	for i, p := range optionalPaths {
		if p != "" {
			optionalPaths[i], err = sanity.SanitizePath(p)
			if err != nil {
				return "", "", "", nil, errorx.IllegalArgument.Wrap(err, "invalid %s path", applicable[i].Name)
			}
		}
	}

	return archivePath, livePath, logPath, optionalPaths, nil
}

// GetTargetVersion returns the configured target version for block node.
func (m *Manager) GetTargetVersion() string {
	return m.blockConfig.Version
}

// GetInstalledVersion returns the currently installed Block Node chart version.
// Returns empty string if not installed.
func (m *Manager) GetInstalledVersion() (string, error) {
	rel, err := m.helmManager.GetRelease(m.blockConfig.Release, m.blockConfig.Namespace)
	if err != nil {
		if errorx.IsOfType(err, helm.ErrNotFound) {
			return "", nil
		}
		return "", errorx.IllegalState.Wrap(err, "failed to get current release")
	}

	if rel.Chart != nil && rel.Chart.Metadata != nil {
		return rel.Chart.Metadata.Version, nil
	}

	return "", nil
}

// GetReleaseValues returns the user-supplied values from the currently installed release.
// Returns nil if not installed or if no user values were supplied.
func (m *Manager) GetReleaseValues() (map[string]interface{}, error) {
	rel, err := m.helmManager.GetRelease(m.blockConfig.Release, m.blockConfig.Namespace)
	if err != nil {
		if errorx.IsOfType(err, helm.ErrNotFound) {
			return nil, nil
		}
		return nil, errorx.IllegalState.Wrap(err, "failed to get current release")
	}

	// rel.Config contains the user-supplied values (not the computed/merged values)
	return rel.Config, nil
}
