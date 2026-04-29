// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"
	oslib "os"
	"path"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"github.com/rs/zerolog"
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

// Manager handles block node setup and management operations.
// Methods are grouped by concern across sibling files:
//   - storage.go  — directory setup, PV/PVC lifecycle, path resolution
//   - chart.go    — Helm install/upgrade/uninstall, StatefulSet and pod lifecycle
//   - values.go   — Helm values file computation and YAML injection helpers
type Manager struct {
	fsManager       fsx.Manager
	helmManager     helm.Manager
	kubeClient      *kube.Client
	logger          *zerolog.Logger
	blockNodeInputs models.BlockNodeInputs
}

// NewManager creates a new block node manager
func NewManager(blockConfig models.BlockNodeInputs) (*Manager, error) {
	l := logx.As()

	fsManager, err := fsx.NewManager()
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create file system manager")
	}

	helmManager, err := helm.NewManager(helm.WithLogger(*l))
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create helm manager")
	}

	kubeClient, err := kube.NewClient()
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create kubernetes client")
	}

	return &Manager{
		fsManager:       fsManager,
		helmManager:     helmManager,
		kubeClient:      kubeClient,
		logger:          l,
		blockNodeInputs: blockConfig,
	}, nil
}

// CreateNamespace creates the block-node namespace if it doesn't exist.
// ApplyManifest is idempotent so this is safe to call on every install.
func (m *Manager) CreateNamespace(ctx context.Context, tempDir string) error {
	data := struct {
		Namespace string
	}{
		Namespace: m.blockNodeInputs.Namespace,
	}

	namespaceContent, err := templates.Render(NamespacePath, data)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to render namespace template")
	}

	manifestFilePath := path.Join(tempDir, "block-node-namespace.yaml")
	if err := oslib.WriteFile(manifestFilePath, []byte(namespaceContent), models.DefaultFilePerm); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to write namespace manifest to temp file")
	}

	if err := m.kubeClient.ApplyManifest(ctx, manifestFilePath); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to apply namespace manifest")
	}

	m.logger.Info().Msgf("Applied namespace manifest for: %s", m.blockNodeInputs.Namespace)
	return nil
}

// DeleteNamespace deletes the block-node namespace
func (m *Manager) DeleteNamespace(ctx context.Context, tempDir string) error {
	manifestFilePath := path.Join(tempDir, "block-node-namespace.yaml")
	return m.kubeClient.DeleteManifest(ctx, manifestFilePath)
}
