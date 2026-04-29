// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"fmt"
	oslib "os"
	"path"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
)

// ComputeValuesFile generates the values file for helm installation based on profile and version.
// It writes the result to a temp file and returns the path.
//
// When no custom values file is provided, the appropriate built-in template is rendered.
// When a custom values file is provided, persistence settings are injected to ensure
// weaver-managed PVCs are referenced (create: false + existingClaim) rather than letting
// the chart create its own.
//
// In both cases, effective retention thresholds are merged into blockNode.config.
//
// NOTE: Defense-in-depth path validation is applied even though the CLI layer also validates.
func (m *Manager) ComputeValuesFile(profile string, valuesFile string) (string, error) {
	var (
		valuesContent []byte
		err           error
	)

	if valuesFile == "" {
		valuesContent, err = m.renderDefaultValues(profile)
	} else {
		valuesContent, err = m.readCustomValues(valuesFile)
	}
	if err != nil {
		return "", err
	}

	// Merge effective retention thresholds into blockNode.config when non-empty.
	valuesContent, err = m.injectRetentionConfig(valuesContent)
	if err != nil {
		return "", errorx.InternalError.Wrap(err, "failed to inject retention config into values file")
	}

	// Inject MetalLB service annotation into Helm values when LoadBalancerEnabled is set so that
	// the annotation is managed natively by Helm across install and upgrade.
	valuesContent, err = m.injectServiceAnnotations(valuesContent)
	if err != nil {
		return "", errorx.InternalError.Wrap(err, "failed to inject service annotations into values file")
	}

	// Write temporary copy to weaver's temp directory.
	valuesFilePath := path.Join(models.Paths().TempDir, "block-node-values.yaml")
	if err = oslib.WriteFile(valuesFilePath, valuesContent, models.DefaultFilePerm); err != nil {
		return "", errorx.InternalError.Wrap(err, "failed to write block node values file")
	}

	return valuesFilePath, nil
}

// renderDefaultValues selects and renders the built-in Helm values template for the given profile.
// The template includes conditional sections for optional storages (verification, plugins) based
// on the target chart version.
func (m *Manager) renderDefaultValues(profile string) ([]byte, error) {
	applicable := GetApplicableOptionalStorages(m.blockNodeInputs.ChartVersion)
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

	rendered, err := templates.Render(valuesTemplatePath, struct {
		IncludeVerification bool
		IncludePlugins      bool
	}{
		IncludeVerification: includeVerification,
		IncludePlugins:      includePlugins,
	})
	if err != nil {
		return nil, errorx.InternalError.Wrap(err, "failed to render block node values template")
	}

	return []byte(rendered), nil
}

// readCustomValues reads and validates a user-supplied values file, then injects persistence
// overrides to ensure weaver-managed PVCs are always referenced (create: false + existingClaim).
// Defense-in-depth validation is applied even though the CLI layer also validates the path.
func (m *Manager) readCustomValues(valuesFile string) ([]byte, error) {
	sanitizedPath, err := sanity.ValidateInputFile(valuesFile)
	if err != nil {
		return nil, err
	}

	content, err := oslib.ReadFile(sanitizedPath)
	if err != nil {
		return nil, errorx.InternalError.Wrap(err, "failed to read provided values file: %s", sanitizedPath)
	}

	logx.As().Info().Str("path", sanitizedPath).Msg("Using custom values file")

	// Inject/override persistence settings to ensure weaver-managed PVCs are used.
	// Since weaver creates PVs and PVCs outside of Helm, the chart must always use
	// create: false with existingClaim pointing to the pre-created PVCs.
	content, err = m.injectPersistenceOverrides(content)
	if err != nil {
		return nil, errorx.InternalError.Wrap(err, "failed to inject persistence overrides into custom values file")
	}

	return content, nil
}

// persistenceEntry represents the required persistence settings for a storage type.
type persistenceEntry struct {
	name      string
	claimName string
}

// injectPersistenceOverrides parses a user-provided values YAML and ensures that
// all applicable persistence entries have create: false and existingClaim set.
// This prevents the Helm chart from creating its own PVCs that would conflict
// with the PVs/PVCs managed by weaver.
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
	for _, optStor := range GetApplicableOptionalStorages(m.blockNodeInputs.ChartVersion) {
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

	for _, entry := range entries {
		existing, ok := persistence[entry.name].(map[string]interface{})
		if !ok {
			existing = make(map[string]interface{})
		}

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

	result, err := yaml.Marshal(vals)
	if err != nil {
		return nil, errorx.InternalError.Wrap(err, "failed to marshal values YAML after persistence override")
	}

	return result, nil
}

// injectRetentionConfig injects FILES_HISTORIC_BLOCK_RETENTION_THRESHOLD and
// FILES_RECENT_BLOCK_RETENTION_THRESHOLD into the blockNode.config map when the
// effective retention values are non-empty (resolved from CLI flags, config file,
// persisted state, or defaults). If both values are empty, the values content is
// returned unchanged.
func (m *Manager) injectRetentionConfig(valuesContent []byte) ([]byte, error) {
	historic := m.blockNodeInputs.HistoricRetention
	recent := m.blockNodeInputs.RecentRetention

	// Nothing to inject — return as-is.
	if historic == "" && recent == "" {
		return valuesContent, nil
	}

	var vals map[string]interface{}
	if err := yaml.Unmarshal(valuesContent, &vals); err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "failed to parse values YAML for retention config injection")
	}

	// Navigate to blockNode.config, creating the path if needed.
	blockNode, ok := vals["blockNode"].(map[string]interface{})
	if !ok {
		blockNode = make(map[string]interface{})
		vals["blockNode"] = blockNode
	}

	blockNodeConfig, ok := blockNode["config"].(map[string]interface{})
	if !ok {
		blockNodeConfig = make(map[string]interface{})
		blockNode["config"] = blockNodeConfig
	}

	if historic != "" {
		logx.As().Info().
			Str("FILES_HISTORIC_BLOCK_RETENTION_THRESHOLD", historic).
			Msg("Applying historic block retention threshold to block node config")
		blockNodeConfig["FILES_HISTORIC_BLOCK_RETENTION_THRESHOLD"] = historic
	}

	if recent != "" {
		logx.As().Info().
			Str("FILES_RECENT_BLOCK_RETENTION_THRESHOLD", recent).
			Msg("Applying recent block retention threshold to block node config")
		blockNodeConfig["FILES_RECENT_BLOCK_RETENTION_THRESHOLD"] = recent
	}

	result, err := yaml.Marshal(vals)
	if err != nil {
		return nil, errorx.InternalError.Wrap(err, "failed to marshal values YAML after retention config injection")
	}

	return result, nil
}

// injectServiceAnnotations merges the MetalLB address-pool annotation into service.annotations
// in the Helm values when LoadBalancerEnabled is true. This ensures the annotation is managed
// natively by Helm across install and upgrade without requiring a post-deploy kubectl patch.
//
// When the operator's values file already contains metallb.io/address-pool (e.g. pointing at a
// custom pool), that value is left untouched — weaver never clobbers an explicitly set annotation.
// When LoadBalancerEnabled is false the values are returned unchanged.
func (m *Manager) injectServiceAnnotations(valuesContent []byte) ([]byte, error) {
	if !m.blockNodeInputs.LoadBalancerEnabled {
		return valuesContent, nil
	}

	var vals map[string]interface{}
	if err := yaml.Unmarshal(valuesContent, &vals); err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "failed to parse values YAML for service annotation injection")
	}

	// Navigate to service.annotations, creating the path if needed.
	service, ok := vals["service"].(map[string]interface{})
	if !ok {
		service = make(map[string]interface{})
		vals["service"] = service
	}

	annotations, ok := service["annotations"].(map[string]interface{})
	if !ok {
		annotations = make(map[string]interface{})
	}

	const annotationKey = "metallb.io/address-pool"
	if existing, alreadySet := annotations[annotationKey]; alreadySet {
		// Operator has explicitly set this annotation — leave it untouched.
		logx.As().Debug().
			Str(annotationKey, fmt.Sprintf("%v", existing)).
			Msg("metallb.io/address-pool already set in values file; skipping injection")
		return valuesContent, nil
	}

	annotations[annotationKey] = "public-address-pool"
	service["annotations"] = annotations

	logx.As().Info().Msg("Injecting MetalLB address-pool annotation into service.annotations")

	result, err := yaml.Marshal(vals)
	if err != nil {
		return nil, errorx.InternalError.Wrap(err, "failed to marshal values YAML after service annotation injection")
	}

	return result, nil
}
