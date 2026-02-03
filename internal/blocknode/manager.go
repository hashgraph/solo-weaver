// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/hashgraph/solo-weaver/pkg/semver"
	"github.com/joomcode/errorx"
	"github.com/rs/zerolog"
	"helm.sh/helm/v3/pkg/cli/values"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	// Kubernetes resources
	ServiceNameSuffix = "-block-node-server"
	PodLabelSelector  = "app.kubernetes.io/name=block-node-server"
	MetalLBAnnotation = "metallb.io/address-pool=public-address-pool"

	// Template paths
	NamespacePath     = "files/block-node/namespace.yaml"
	StorageConfigPath = "files/block-node/storage-config.yaml"
	ValuesPath        = "files/block-node/full-values.yaml"
	NanoValuesPath    = "files/block-node/nano-values.yaml"

	// Timeouts
	PodReadyTimeoutSeconds = 300

	// VerificationStorageMinVersion is the minimum Block Node version that requires verification storage.
	// Block Node v0.26.2 introduced a new PersistentVolume for verification data.
	// Upgrading from versions < 0.26.2 to >= 0.26.2 requires uninstall + reinstall.
	VerificationStorageMinVersion = "0.26.2"
)

// Manager handles block node setup and management operations
type Manager struct {
	fsManager   fsx.Manager
	helmManager helm.Manager
	kubeClient  *kube.Client
	clientset   *kubernetes.Clientset // Still needed for pod listing and service updates
	logger      *zerolog.Logger
	blockConfig *config.BlockNodeConfig
}

// NewManager creates a new block node manager
func NewManager(blockConfig config.BlockNodeConfig) (*Manager, error) {
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

	// Kubernetes clientset for namespace operations
	config, err := getKubeConfig()
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to get kubeconfig")
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create kubernetes clientset")
	}

	return &Manager{
		fsManager:   fsManager,
		helmManager: helmManager,
		kubeClient:  kubeClient,
		clientset:   clientset,
		logger:      l,
		blockConfig: &blockConfig,
	}, nil
}

// getKubeConfig returns the kubernetes rest config
func getKubeConfig() (*rest.Config, error) {
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		return rest.InClusterConfig()
	}

	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = os.Getenv("HOME") + "/.kube/config"
	}
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}

// SetupStorage creates the required directories for block node storage
func (m *Manager) SetupStorage(ctx context.Context) error {
	// Get storage paths (already validated by GetStoragePaths)
	archivePath, livePath, logPath, verificationPath, err := m.GetStoragePaths()
	if err != nil {
		return err
	}

	storagePaths := []string{
		archivePath,
		livePath,
		logPath,
		verificationPath,
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

		if err := m.fsManager.WritePermissions(dirPath, core.DefaultDirOrExecPerm, true); err != nil {
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
	if err := os.WriteFile(manifestFilePath, []byte(namespaceContent), core.DefaultFilePerm); err != nil {
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
	archivePath, livePath, logPath, verificationPath, err := m.GetStoragePaths()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to get storage paths")
	}

	// Prepare template data
	data := struct {
		Namespace        string
		LivePath         string
		ArchivePath      string
		LogPath          string
		VerificationPath string
		LiveSize         string
		ArchiveSize      string
		LogSize          string
		VerificationSize string
	}{
		Namespace:        m.blockConfig.Namespace,
		LivePath:         livePath,
		ArchivePath:      archivePath,
		LogPath:          logPath,
		VerificationPath: verificationPath,
		LiveSize:         m.blockConfig.Storage.LiveSize,
		ArchiveSize:      m.blockConfig.Storage.ArchiveSize,
		LogSize:          m.blockConfig.Storage.LogSize,
		VerificationSize: m.blockConfig.Storage.VerificationSize,
	}

	// Render the storage config template
	storageConfig, err := templates.Render(StorageConfigPath, data)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to render storage config template")
	}

	// Write to temp file
	configFilePath := path.Join(tempDir, "block-node-storage-config.yaml")
	if err := os.WriteFile(configFilePath, []byte(storageConfig), core.DefaultFilePerm); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to write storage config to temp file")
	}

	// Apply the configuration
	if err := m.kubeClient.ApplyManifest(ctx, configFilePath); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to apply storage configuration")
	}

	// Wait for all PVCs to be bound
	pvcNames := []string{"live-storage-pvc", "archive-storage-pvc", "logging-storage-pvc", "verification-storage-pvc"}
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
		m.blockConfig.Version,
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
		m.blockConfig.Version,
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
		return errorx.IllegalState.Wrap(err, "failed to upgrade block node chart")
	}

	return nil
}

// AnnotateService annotates the block node service with MetalLB address pool
func (m *Manager) AnnotateService(ctx context.Context) error {
	svcName := fmt.Sprintf("%s%s", m.blockConfig.Release, ServiceNameSuffix)

	svc, err := m.clientset.CoreV1().Services(m.blockConfig.Namespace).Get(ctx, svcName, metav1.GetOptions{})
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to get service: %s", svcName)
	}

	if svc.Annotations == nil {
		svc.Annotations = make(map[string]string)
	}

	svc.Annotations["metallb.io/address-pool"] = "public-address-pool"

	_, err = m.clientset.CoreV1().Services(m.blockConfig.Namespace).Update(ctx, svc, metav1.UpdateOptions{})
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to annotate service: %s", svcName)
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

// ComputeValuesFile generates the values file for helm installation based on profile.
// It provides the path to the generated values file.
//
// NOTE: This method implements defense-in-depth validation. Even though the CLI layer
// validates paths using sanity.ValidateInputFile(), this method also validates to ensure
// safety regardless of the caller. This protects against future code changes where this
// method might be called from other places without proper validation.
func (m *Manager) ComputeValuesFile(profile string, valuesFile string) (string, error) {
	var valuesContent []byte
	var err error

	if valuesFile == "" {
		// Use embedded template based on profile
		valuesTemplatePath := ValuesPath
		if profile == core.ProfileLocal {
			valuesTemplatePath = NanoValuesPath
			logx.As().Info().Msg("Using nano values configuration for local profile")
		}

		valuesContent, err = templates.Read(valuesTemplatePath)
		if err != nil {
			return "", errorx.InternalError.Wrap(err, "failed to read block node values template")
		}
	} else {
		// Defense-in-depth: validate even though CLI layer already validated
		// This ensures safety if this method is called from other contexts
		sanitizedPath, err := sanity.ValidateInputFile(valuesFile)
		if err != nil {
			return "", err
		}

		valuesContent, err = os.ReadFile(sanitizedPath)
		if err != nil {
			return "", errorx.InternalError.Wrap(err, "failed to read provided values file: %s", sanitizedPath)
		}

		logx.As().Info().Str("path", sanitizedPath).Msg("Using custom values file")
	}

	// Write temporary copy to weaver's temp directory
	valuesFilePath := path.Join(core.Paths().TempDir, "block-node-values.yaml")
	if err = os.WriteFile(valuesFilePath, valuesContent, core.DefaultFilePerm); err != nil {
		return "", errorx.InternalError.Wrap(err, "failed to write block node values file")
	}

	return valuesFilePath, nil
}

// GetStoragePaths returns the computed storage paths based on configuration.
// If individual paths are specified, they are used; otherwise, paths are derived from basePath.
// All paths are validated using sanity checks.
func (m *Manager) GetStoragePaths() (archivePath, livePath, logPath, verificationPath string, err error) {
	archivePath = m.blockConfig.Storage.ArchivePath
	livePath = m.blockConfig.Storage.LivePath
	logPath = m.blockConfig.Storage.LogPath
	verificationPath = m.blockConfig.Storage.VerificationPath

	// Sanitize basePath before using it to construct other paths
	basePath := m.blockConfig.Storage.BasePath
	if basePath != "" {
		basePath, err = sanity.SanitizePath(basePath)
		if err != nil {
			return "", "", "", "", errorx.IllegalArgument.Wrap(err, "invalid base storage path")
		}
	}

	// If individual paths are not specified, use basePath as the parent.
	// Do not allow deriving defaults from an empty basePath, as that would
	// create relative paths and cause storage to depend on the current
	// working directory.
	if basePath == "" && (archivePath == "" || livePath == "" || logPath == "" || verificationPath == "") {
		return "", "", "", "", errorx.IllegalArgument.New("at least one storage path is not set and base storage path is empty")
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
	if verificationPath == "" {
		verificationPath = path.Join(basePath, "verification")
	}

	// Validate all paths using sanity checks
	archivePath, err = sanity.SanitizePath(archivePath)
	if err != nil {
		return "", "", "", "", errorx.IllegalArgument.Wrap(err, "invalid archive path")
	}

	livePath, err = sanity.SanitizePath(livePath)
	if err != nil {
		return "", "", "", "", errorx.IllegalArgument.Wrap(err, "invalid live path")
	}

	logPath, err = sanity.SanitizePath(logPath)
	if err != nil {
		return "", "", "", "", errorx.IllegalArgument.Wrap(err, "invalid log path")
	}

	verificationPath, err = sanity.SanitizePath(verificationPath)
	if err != nil {
		return "", "", "", "", errorx.IllegalArgument.Wrap(err, "invalid verification path")
	}

	return archivePath, livePath, logPath, verificationPath, nil
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

// RequiresReinstall checks if upgrading from the currently installed version to the target version
// requires a full reinstall (uninstall + install) due to breaking Helm chart changes.
//
// Known breaking changes:
// - v0.26.2: Added verification storage PV/PVC (StatefulSet volumeMounts changed)
func (m *Manager) RequiresReinstall() (bool, string, error) {
	installedVersion, err := m.GetInstalledVersion()
	if err != nil {
		return false, "", err
	}

	// If not installed, no reinstall needed
	if installedVersion == "" {
		m.logger.Info().Msg("Block Node not installed, no reinstall needed")
		return false, "", nil
	}

	targetVersion := m.blockConfig.Version

	m.logger.Info().
		Str("installedVersion", installedVersion).
		Str("targetVersion", targetVersion).
		Str("verificationMinVersion", VerificationStorageMinVersion).
		Msg("Checking if migration is required for upgrade")

	// Parse versions for comparison
	installed, err := semver.NewSemver(installedVersion)
	if err != nil {
		m.logger.Warn().Err(err).Str("version", installedVersion).Msg("Failed to parse installed version, assuming no migration needed")
		return false, "", nil
	}

	target, err := semver.NewSemver(targetVersion)
	if err != nil {
		m.logger.Warn().Err(err).Str("version", targetVersion).Msg("Failed to parse target version, assuming no migration needed")
		return false, "", nil
	}

	verificationMinVersion, _ := semver.NewSemver(VerificationStorageMinVersion)

	// Check if upgrading across the verification storage boundary (< 0.26.2 to >= 0.26.2)
	installedLessThanMin := installed.LessThan(verificationMinVersion)
	targetNotLessThanMin := !target.LessThan(verificationMinVersion)

	m.logger.Info().
		Bool("installedLessThanMin", installedLessThanMin).
		Bool("targetNotLessThanMin", targetNotLessThanMin).
		Msg("Version comparison results")

	if installedLessThanMin && targetNotLessThanMin {
		reason := fmt.Sprintf("Block Node v%s introduced new verification storage PV/PVC. "+
			"Upgrading from v%s to v%s requires uninstall + reinstall due to StatefulSet changes.",
			VerificationStorageMinVersion, installedVersion, targetVersion)
		m.logger.Info().Str("reason", reason).Msg("Migration reinstall required")
		return true, reason, nil
	}

	m.logger.Info().Msg("No migration required, proceeding with normal upgrade")
	return false, "", nil
}

// PerformMigrationReinstall handles the uninstall + reinstall flow for breaking chart changes.
// It preserves the existing values and data while recreating the Helm release with new storage.
func (m *Manager) PerformMigrationReinstall(ctx context.Context, profile string, valuesFile string) error {
	m.logger.Info().Msg("Performing migration reinstall for breaking chart changes")

	// Step 1: Setup the new verification storage directory
	_, _, _, verificationPath, err := m.GetStoragePaths()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to get storage paths for migration")
	}

	if verificationPath != "" {
		if err := m.fsManager.CreateDirectory(verificationPath, true); err != nil {
			return errorx.IllegalState.Wrap(err, "failed to create verification storage directory")
		}
		if err := m.fsManager.WritePermissions(verificationPath, core.DefaultDirOrExecPerm, true); err != nil {
			return errorx.IllegalState.Wrap(err, "failed to set permissions on verification storage")
		}
		m.logger.Info().Str("path", verificationPath).Msg("Created verification storage directory")
	}

	// Step 2: Uninstall the current release
	m.logger.Info().Msg("Uninstalling current Block Node release for migration")
	if err := m.UninstallChart(ctx); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to uninstall chart for migration")
	}

	// Step 3: Recreate PVs/PVCs with new verification storage
	m.logger.Info().Msg("Creating updated PersistentVolumes with verification storage")
	if err := m.CreatePersistentVolumes(ctx, core.Paths().TempDir); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create PVs for migration")
	}

	// Step 4: Reinstall with the new chart version
	m.logger.Info().Msg("Reinstalling Block Node with new chart version")
	valuesFilePath, err := m.ComputeValuesFile(profile, valuesFile)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to compute values file for migration")
	}

	_, err = m.InstallChart(ctx, valuesFilePath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to reinstall chart after migration")
	}

	m.logger.Info().Msg("Migration reinstall completed successfully")
	return nil
}
