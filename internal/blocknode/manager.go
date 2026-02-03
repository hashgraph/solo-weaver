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

	// Template paths for v0.26.2+ (includes verification storage)
	ValuesPathV0262     = "files/block-node/full-values-v0.26.2.yaml"
	NanoValuesPathV0262 = "files/block-node/nano-values-v0.26.2.yaml"

	// Timeouts
	PodReadyTimeoutSeconds = 300
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

	// Core storage paths are always required
	storagePaths := []string{
		archivePath,
		livePath,
		logPath,
	}

	// Verification storage is only needed for Block Node versions >= 0.26.2
	if m.requiresVerificationStorage() && verificationPath != "" {
		storagePaths = append(storagePaths, verificationPath)
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
	pvcNames := []string{"live-storage-pvc", "archive-storage-pvc", "logging-storage-pvc"}

	// Verification storage PVC is only needed for Block Node versions >= 0.26.2
	if m.requiresVerificationStorage() {
		pvcNames = append(pvcNames, "verification-storage-pvc")
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

// ComputeValuesFile generates the values file for helm installation based on profile and version.
// It provides the path to the generated values file.
//
// The method selects the appropriate values template based on the target Block Node version:
// - For versions >= 0.26.2: Uses values templates with verification storage configuration
// - For versions < 0.26.2: Uses values templates without verification storage
//
// NOTE: This method implements defense-in-depth validation. Even though the CLI layer
// validates paths using sanity.ValidateInputFile(), this method also validates to ensure
// safety regardless of the caller. This protects against future code changes where this
// method might be called from other places without proper validation.
func (m *Manager) ComputeValuesFile(profile string, valuesFile string) (string, error) {
	var valuesContent []byte
	var err error

	if valuesFile == "" {
		// Determine if we need v0.26.2+ values (with verification storage)
		needsVerificationStorage := m.requiresVerificationStorage()

		// Use embedded template based on profile and version
		valuesTemplatePath := ValuesPath
		if profile == core.ProfileLocal {
			if needsVerificationStorage {
				valuesTemplatePath = NanoValuesPathV0262
				logx.As().Info().Msg("Using nano values configuration with verification storage for local profile")
			} else {
				valuesTemplatePath = NanoValuesPath
				logx.As().Info().Msg("Using nano values configuration for local profile")
			}
		} else {
			if needsVerificationStorage {
				valuesTemplatePath = ValuesPathV0262
				logx.As().Info().Msg("Using full values configuration with verification storage")
			}
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
// verificationPath is only required/derived/validated when the target version requires verification storage (>= 0.26.2).
func (m *Manager) GetStoragePaths() (archivePath, livePath, logPath, verificationPath string, err error) {
	archivePath = m.blockConfig.Storage.ArchivePath
	livePath = m.blockConfig.Storage.LivePath
	logPath = m.blockConfig.Storage.LogPath
	verificationPath = m.blockConfig.Storage.VerificationPath

	// Determine if verification storage is needed based on target version
	needsVerificationStorage := m.requiresVerificationStorage()

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
	// Note: verificationPath is only required for versions >= 0.26.2.
	corePathsMissing := archivePath == "" || livePath == "" || logPath == ""
	verificationPathMissing := needsVerificationStorage && verificationPath == ""
	if basePath == "" && (corePathsMissing || verificationPathMissing) {
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
	if needsVerificationStorage && verificationPath == "" {
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

	// Always sanitize verificationPath when non-empty to prevent bypassing security checks.
	// This handles the case where a user sets verificationPath while targeting < 0.26.2.
	if verificationPath != "" {
		verificationPath, err = sanity.SanitizePath(verificationPath)
		if err != nil {
			return "", "", "", "", errorx.IllegalArgument.Wrap(err, "invalid verification path")
		}
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

// requiresVerificationStorage checks if the target version requires verification storage.
// Returns true if the target version is >= 0.26.2, false otherwise.
func (m *Manager) requiresVerificationStorage() bool {
	targetVersion := m.blockConfig.Version

	target, err := semver.NewSemver(targetVersion)
	if err != nil {
		// If we can't parse the version, assume it doesn't need verification storage
		// to maintain backward compatibility
		m.logger.Warn().
			Err(err).
			Str("version", targetVersion).
			Msg("Could not parse target version, assuming no verification storage needed")
		return false
	}

	minVersion, err := semver.NewSemver(VerificationStorageMinVersion)
	if err != nil {
		m.logger.Panic().
			Err(err).
			Str("version", VerificationStorageMinVersion).
			Msg("Invalid VerificationStorageMinVersion constant; this is a programming error")
		return false
	}

	// Requires verification storage if target >= 0.26.2
	return !target.LessThan(minVersion)
}
