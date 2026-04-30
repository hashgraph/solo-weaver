// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"
	oslib "os"
	"path"
	"time"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
)

// SetupStorage creates the required directories for block node storage
func (m *Manager) SetupStorage(ctx context.Context) error {
	archivePath, livePath, logPath, optionalPaths, err := m.GetStoragePaths()
	if err != nil {
		return err
	}

	storagePaths := []string{archivePath, livePath, logPath}
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

// CreatePersistentVolumes creates PVs and PVCs from the storage config
func (m *Manager) CreatePersistentVolumes(ctx context.Context, tempDir string) error {
	archivePath, livePath, logPath, optionalPaths, err := m.GetStoragePaths()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to get storage paths")
	}

	applicable := GetApplicableOptionalStorages(m.blockNodeInputs.ChartVersion)
	includeVerification := false
	includePlugins := false
	verificationPath := ""
	pluginsPath := ""
	verificationSize := ""
	pluginsSize := ""

	for i, optStor := range applicable {
		p := ""
		if i < len(optionalPaths) {
			p = optionalPaths[i]
		}
		s := optStor.GetSize(&m.blockNodeInputs.Storage)
		switch optStor.Name {
		case "verification":
			includeVerification = true
			verificationPath = p
			verificationSize = s
		case "plugins":
			includePlugins = true
			pluginsPath = p
			pluginsSize = s
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
		Namespace:           m.blockNodeInputs.Namespace,
		LivePath:            livePath,
		ArchivePath:         archivePath,
		LogPath:             logPath,
		VerificationPath:    verificationPath,
		PluginsPath:         pluginsPath,
		LiveSize:            m.blockNodeInputs.Storage.LiveSize,
		ArchiveSize:         m.blockNodeInputs.Storage.ArchiveSize,
		LogSize:             m.blockNodeInputs.Storage.LogSize,
		VerificationSize:    verificationSize,
		PluginsSize:         pluginsSize,
		IncludeVerification: includeVerification,
		IncludePlugins:      includePlugins,
	}

	m.logger.Debug().Str("template", StorageConfigPath).Msg("Rendering storage config template")
	storageConfig, err := templates.Render(StorageConfigPath, data)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to render storage config template")
	}

	configFilePath := path.Join(tempDir, "block-node-storage-config.yaml")
	m.logger.Debug().Str("configFile", configFilePath).Msg("Writing storage config to temp file")
	if err := oslib.WriteFile(configFilePath, []byte(storageConfig), models.DefaultFilePerm); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to write storage config to temp file")
	}

	m.logger.Debug().Str("configFile", configFilePath).Msg("Applying storage manifest")
	if err := m.kubeClient.ApplyManifest(ctx, configFilePath); err != nil {
		m.logger.Error().Err(err).Str("configFile", configFilePath).Msg("Failed to apply storage manifest")
		return errorx.IllegalState.Wrap(err, "failed to apply storage configuration")
	}
	m.logger.Debug().Msg("Storage manifest applied successfully")

	pvcNames := []string{"live-storage-pvc", "archive-storage-pvc", "logging-storage-pvc"}
	for _, os := range applicable {
		pvcNames = append(pvcNames, os.PVCName)
	}

	timeout := 2 * time.Minute
	for _, pvcName := range pvcNames {
		m.logger.Info().Str("pvc", pvcName).Msg("Waiting for PVC to be bound...")
		if err := m.kubeClient.WaitForResource(ctx, kube.KindPVC, m.blockNodeInputs.Namespace, pvcName, kube.IsPVCBound, timeout); err != nil {
			return errorx.IllegalState.Wrap(err, "PVC %s did not become bound in time", pvcName)
		}
		m.logger.Info().Str("pvc", pvcName).Msg("PVC is bound")
	}

	return nil
}

// DeleteAllPersistentVolumes deletes all known block-node PVs and PVCs by name.
// It does not depend on a previously-written manifest file, making it safe to call
// even when the tempDir has been cleared or was never written.
// Missing resources are silently ignored.
func (m *Manager) DeleteAllPersistentVolumes(ctx context.Context) error {
	ns := m.blockNodeInputs.Namespace

	corePVCNames := []string{"live-storage-pvc", "archive-storage-pvc", "logging-storage-pvc"}
	corePVNames := []string{"live-storage-pv", "archive-storage-pv", "logging-storage-pv"}

	for _, optStor := range GetApplicableOptionalStorages(m.blockNodeInputs.ChartVersion) {
		corePVCNames = append(corePVCNames, optStor.PVCName)
		corePVNames = append(corePVNames, optStor.PVName)
	}

	for _, pvcName := range corePVCNames {
		m.logger.Info().Str("pvc", pvcName).Msg("Deleting PVC")
		if err := m.kubeClient.DeletePVC(ctx, ns, pvcName); err != nil {
			return errorx.IllegalState.Wrap(err, "failed to delete PVC %s", pvcName)
		}
	}

	for _, pvName := range corePVNames {
		m.logger.Info().Str("pv", pvName).Msg("Deleting PV")
		if err := m.kubeClient.DeletePV(ctx, pvName); err != nil {
			return errorx.IllegalState.Wrap(err, "failed to delete PV %s", pvName)
		}
	}

	return nil
}

// CreateOptionalStorage creates a single optional PV/PVC using the unified template.
// Used during migration when other PVs already exist.
func (m *Manager) CreateOptionalStorage(ctx context.Context, tempDir string, optStor OptionalStorage) error {
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
		Namespace: m.blockNodeInputs.Namespace,
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

	storageConfig, err := templates.Render(OptionalStoragePath, data)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to render %s storage template", optStor.Name)
	}

	configFilePath := path.Join(tempDir, "block-node-"+optStor.Name+"-storage.yaml")
	if err := oslib.WriteFile(configFilePath, []byte(storageConfig), models.DefaultFilePerm); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to write %s storage config", optStor.Name)
	}

	if err := m.kubeClient.ApplyManifest(ctx, configFilePath); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to apply %s storage", optStor.Name)
	}

	m.logger.Info().Str("pvc", optStor.PVCName).Msgf("Waiting for %s PVC to be bound...", optStor.Name)
	timeout := 2 * time.Minute
	if err := m.kubeClient.WaitForResource(ctx, kube.KindPVC, m.blockNodeInputs.Namespace, optStor.PVCName, kube.IsPVCBound, timeout); err != nil {
		return errorx.IllegalState.Wrap(err, "%s PVC did not become bound in time", optStor.Name)
	}
	m.logger.Info().Str("pvc", optStor.PVCName).Msgf("%s PVC is bound", optStor.Name)

	return nil
}

// resolveOptionalStoragePathAndSize derives the effective path and size for an optional storage,
// using basePath derivation when the individual path is not explicitly configured.
func (m *Manager) resolveOptionalStoragePathAndSize(optStor OptionalStorage) (string, string, error) {
	storagePath := optStor.GetPath(&m.blockNodeInputs.Storage)
	storageSize := optStor.GetSize(&m.blockNodeInputs.Storage)

	if storagePath == "" {
		basePath := m.blockNodeInputs.Storage.BasePath
		if basePath != "" {
			sanitizedBase, err := sanity.SanitizePath(basePath)
			if err != nil {
				return "", "", errorx.IllegalArgument.Wrap(err, "invalid base storage path")
			}
			storagePath = path.Join(sanitizedBase, optStor.DirName)
		}
	}

	if storagePath != "" {
		sanitized, err := sanity.SanitizePath(storagePath)
		if err != nil {
			return "", "", errorx.IllegalArgument.Wrap(err, "invalid %s path", optStor.Name)
		}
		storagePath = sanitized
	} else {
		return "", "", errorx.IllegalArgument.New(
			"%s storage path is empty: set --base-path (or storage.basePath in config) or --%s-path",
			optStor.Name, optStor.Name,
		)
	}

	return storagePath, storageSize, nil
}

// ClearStorageDirectory removes all files and subdirectories from a storage directory
// while preserving the directory itself.
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

	if err := m.fsManager.RemoveContents(dirPath); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to clear directory contents: %s", dirPath)
	}

	return nil
}

// ResetStorage clears all block node storage directories.
func (m *Manager) ResetStorage(ctx context.Context) error {
	archivePath, livePath, logPath, optionalPaths, err := m.GetStoragePaths()
	if err != nil {
		return err
	}

	storagePaths := []string{archivePath, livePath, logPath}
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

// GetStoragePaths returns the computed storage paths based on configuration.
// If individual paths are specified, they are used; otherwise, paths are derived from basePath.
// All paths are validated using sanity checks.
// optionalPaths contains the paths for applicable optional storages (in registry order).
func (m *Manager) GetStoragePaths() (archivePath, livePath, logPath string, optionalPaths []string, err error) {
	archivePath = m.blockNodeInputs.Storage.ArchivePath
	livePath = m.blockNodeInputs.Storage.LivePath
	logPath = m.blockNodeInputs.Storage.LogPath

	applicable := GetApplicableOptionalStorages(m.blockNodeInputs.ChartVersion)

	basePath := m.blockNodeInputs.Storage.BasePath
	if basePath != "" {
		basePath, err = sanity.SanitizePath(basePath)
		if err != nil {
			return "", "", "", nil, errorx.IllegalArgument.Wrap(err, "invalid base storage path")
		}
	}

	corePathsMissing := archivePath == "" || livePath == "" || logPath == ""
	optionalPathMissing := false
	for _, optStor := range applicable {
		if optStor.GetPath(&m.blockNodeInputs.Storage) == "" {
			optionalPathMissing = true
			break
		}
	}

	if basePath == "" && (corePathsMissing || optionalPathMissing) {
		return "", "", "", nil, errorx.IllegalArgument.New("at least one storage path is not set and base path is empty; set --base-path flag or storage.basePath in config")
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

	for _, optStor := range applicable {
		p := optStor.GetPath(&m.blockNodeInputs.Storage)
		if p == "" {
			p = path.Join(basePath, optStor.DirName)
		}
		optionalPaths = append(optionalPaths, p)
	}

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
