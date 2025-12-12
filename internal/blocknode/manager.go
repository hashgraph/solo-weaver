// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"
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
	ServiceName       = "block-node-block-node-server"
	PodLabelSelector  = "app.kubernetes.io/name=block-node-server"
	MetalLBAnnotation = "metallb.io/address-pool=public-address-pool"

	// Template paths
	NamespacePath     = "files/block-node/namespace.yaml"
	StorageConfigPath = "files/block-node/storage-config.yaml"
	ValuesPath        = "files/block-node/full-values.yaml"
	NanoValuesPath    = "files/block-node/nano-values.yaml"

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
	storagePaths := []string{
		m.blockConfig.Storage.BasePath,
		path.Join(m.blockConfig.Storage.BasePath, "archive"),
		path.Join(m.blockConfig.Storage.BasePath, "live"),
		path.Join(m.blockConfig.Storage.BasePath, "logs"),
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
	// Read the namespace template
	namespaceManifest, err := templates.Read(NamespacePath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to read namespace template")
	}

	// Write to temp file
	manifestFilePath := path.Join(tempDir, "block-node-namespace.yaml")
	if err := os.WriteFile(manifestFilePath, []byte(namespaceManifest), core.DefaultFilePerm); err != nil {
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
	// Read the storage config template
	storageConfig, err := templates.Read(StorageConfigPath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to read storage config template")
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

// AnnotateService annotates the block node service with MetalLB address pool
func (m *Manager) AnnotateService(ctx context.Context) error {
	svc, err := m.clientset.CoreV1().Services(m.blockConfig.Namespace).Get(ctx, ServiceName, metav1.GetOptions{})
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to get service: %s", ServiceName)
	}

	if svc.Annotations == nil {
		svc.Annotations = make(map[string]string)
	}

	svc.Annotations["metallb.io/address-pool"] = "public-address-pool"

	_, err = m.clientset.CoreV1().Services(m.blockConfig.Namespace).Update(ctx, svc, metav1.UpdateOptions{})
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to annotate service: %s", ServiceName)
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
