// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"fmt"
	oslib "os"
	"path"
	"strconv"
	"strings"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/chartutil"
)

// ComputeValuesFile generates the values file for helm installation based on profile and version.
// It writes the result to a temp file and returns the path.
//
// The profile's embedded base template is always rendered first; when an operator --values
// file is provided, it is deep-merged on top of the base (operator keys win on conflict)
// using the same semantics as `helm install -f`. This preserves base defaults such as
// service.type: LoadBalancer, blockNode.config, initContainers, and persistence wiring
// that an operator file would otherwise drop by replacing the base entirely.
//
// Persistence overrides are then applied unconditionally so that weaver-managed PVCs are
// referenced (create: false + existingClaim) regardless of what the operator file specified.
// Retention thresholds and plugin/service-annotation injections follow.
//
// NOTE: Defense-in-depth path validation is applied even though the CLI layer also validates.
func (m *Manager) ComputeValuesFile(profile string, valuesFile string) (string, error) {
	valuesContent, err := m.renderDefaultValues(profile)
	if err != nil {
		return "", err
	}

	if valuesFile != "" {
		operatorContent, err := m.readCustomValues(valuesFile)
		if err != nil {
			return "", err
		}
		// Return mergeValues' typed error directly so an IllegalFormat from a malformed
		// operator YAML stays IllegalFormat — wrapping it as InternalError would
		// misattribute the failure to weaver instead of the operator's --values file.
		valuesContent, err = mergeValues(valuesContent, operatorContent)
		if err != nil {
			return "", err
		}
	}

	// Force weaver-managed PVC references regardless of what the operator file said.
	valuesContent, err = m.injectPersistenceOverrides(valuesContent)
	if err != nil {
		return "", errorx.InternalError.Wrap(err, "failed to inject persistence overrides into values file")
	}

	// Merge effective retention thresholds into blockNode.config when non-empty.
	valuesContent, err = m.injectRetentionConfig(valuesContent)
	if err != nil {
		return "", errorx.InternalError.Wrap(err, "failed to inject retention config into values file")
	}

	// Inject resolved plugin list into plugins.names when set.
	valuesContent, err = m.injectPluginsConfig(valuesContent)
	if err != nil {
		return "", errorx.InternalError.Wrap(err, "failed to inject plugins config into values file")
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
// The template includes conditional sections for optional storages (verification, plugins,
// application-state) gated on the chart's target version via the registry.
func (m *Manager) renderDefaultValues(profile string) ([]byte, error) {
	applicable := GetApplicableOptionalStorages(m.blockNodeInputs.ChartVersion)
	includeVerification := false
	includePlugins := false
	includeApplicationState := false
	for _, optStor := range applicable {
		switch optStor.Name {
		case "verification":
			includeVerification = true
		case "plugins":
			// #913: only mount the plugins volume when the effective plugins.names
			// is non-empty. For a plugins-baked image the chart reads the image's
			// baked plugins directly, so weaver must not render the volume/mount.
			includePlugins = m.managesPluginsStorage()
		case "application-state":
			includeApplicationState = true
		}
	}

	valuesTemplatePath := ValuesPath
	if profile == models.ProfileLocal {
		valuesTemplatePath = NanoValuesPath
		logx.As().Info().
			Bool("includeVerification", includeVerification).
			Bool("includePlugins", includePlugins).
			Bool("includeApplicationState", includeApplicationState).
			Msg("Using nano values configuration for local profile")
	} else {
		logx.As().Info().
			Bool("includeVerification", includeVerification).
			Bool("includePlugins", includePlugins).
			Bool("includeApplicationState", includeApplicationState).
			Msg("Using full values configuration")
	}

	rendered, err := templates.Render(valuesTemplatePath, struct {
		IncludeVerification     bool
		IncludePlugins          bool
		IncludeApplicationState bool
	}{
		IncludeVerification:     includeVerification,
		IncludePlugins:          includePlugins,
		IncludeApplicationState: includeApplicationState,
	})
	if err != nil {
		return nil, errorx.InternalError.Wrap(err, "failed to render block node values template")
	}

	return []byte(rendered), nil
}

// readCustomValues reads and validates a user-supplied values file and returns its raw
// bytes. The caller is responsible for merging it on top of the profile base and applying
// any post-merge invariants (persistence overrides, etc.). Defense-in-depth validation
// is applied even though the CLI layer also validates the path.
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

	return content, nil
}

// mergeValues deep-merges an operator-supplied values document on top of the profile
// base using helm's own coalescing rules (the same logic that `helm install -f` applies):
// nested maps are merged recursively, while scalars and sequences from the operator
// replace whatever the base had. Operator keys win on conflict; base defaults survive
// where the operator stays silent.
func mergeValues(base, operator []byte) ([]byte, error) {
	baseMap := map[string]interface{}{}
	if err := yaml.Unmarshal(base, &baseMap); err != nil {
		// The base is rendered from an embedded template, never operator input;
		// a parse failure here is a developer bug, not malformed user input.
		return nil, errorx.InternalError.Wrap(err, "failed to parse embedded base values YAML")
	}

	operatorMap := map[string]interface{}{}
	if err := yaml.Unmarshal(operator, &operatorMap); err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "failed to parse custom values YAML")
	}

	merged := chartutil.CoalesceTables(operatorMap, baseMap)

	result, err := yaml.Marshal(merged)
	if err != nil {
		return nil, errorx.InternalError.Wrap(err, "failed to marshal merged values YAML")
	}
	return result, nil
}

// ValuesFileDefinesPlugins reports whether the operator-supplied values file manages
// plugins.names at all — i.e. the key is present under plugins, regardless of its value.
// This includes an explicit empty string, an empty sequence, or null: Helm's
// chartutil.CoalesceTables treats an operator-set null as an instruction to delete the
// chart's default plugins.names entirely (see coalesceTablesFullKey's "nullifying a
// chart default" branch), which is just as much a deliberate operator choice as setting
// a concrete list. Callers (the prompt smart-default and the upgrade re-resolve guard)
// must not clobber any of these with a preset-derived list.
// It is intentionally error-tolerant: an empty path, an unreadable file, or a parse
// failure all return false rather than an error — the interactive prompt uses this only
// to decide a smart default, and the deploy-time path (ComputeValuesFile) surfaces any
// real read/parse error later.
func ValuesFileDefinesPlugins(valuesFile string) bool {
	_, defined := pluginsNamesFromValuesFile(valuesFile)
	return defined
}

// pluginsNamesFromValuesFile reads plugins.names from an operator values file,
// returning its raw value and whether the key is defined at all. It is the
// shared parse behind ValuesFileDefinesPlugins and EffectivePluginsNamesEmpty.
// It is intentionally error-tolerant: an empty path, an unreadable file, a parse
// failure, or a missing plugins/names key all return (nil, false) rather than an
// error — callers use it only for smart-defaults and provisioning gates, and the
// deploy-time path surfaces any real read/parse error later.
func pluginsNamesFromValuesFile(valuesFile string) (value interface{}, defined bool) {
	if valuesFile == "" {
		return nil, false
	}
	sanitizedPath, err := sanity.ValidateInputFile(valuesFile)
	if err != nil {
		return nil, false
	}
	content, err := oslib.ReadFile(sanitizedPath)
	if err != nil {
		return nil, false
	}

	var vals map[string]interface{}
	if err := yaml.Unmarshal(content, &vals); err != nil {
		return nil, false
	}
	plugins, ok := vals["plugins"].(map[string]interface{})
	if !ok {
		return nil, false
	}
	v, ok := plugins["names"]
	return v, ok
}

// pluginsNamesValueEmpty reports whether a plugins.names value read from a values
// file is empty — nil (a YAML `names:` with no value), a blank/whitespace string,
// or an empty sequence. Any other shape (a non-blank string, a non-empty list) is
// treated as non-empty.
func pluginsNamesValueEmpty(v interface{}) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	case []interface{}:
		return len(t) == 0
	default:
		return false
	}
}

// EffectivePluginsNamesEmpty reports whether the plugins.names that will reach the
// block-node chart is empty — the "plugins-baked image" signal for issue #913.
// Precedence mirrors how weaver builds the final values:
//  1. A weaver-resolved PluginList (from --plugins/--plugin-preset) is injected
//     into plugins.names, so a non-empty list means non-empty.
//  2. Otherwise an operator --values file that defines plugins.names decides: an
//     explicit empty/null empties it (baked image), a concrete value keeps it.
//  3. Otherwise weaver's base-template default (a non-empty list) reaches the chart.
//
// When true, weaver must skip provisioning the managed plugins PVC and injecting
// existingClaim, and must emit an explicit empty plugins.names, so the chart
// renders no plugins volume and the container reads the image's baked plugins.
func EffectivePluginsNamesEmpty(pluginList, valuesFile string) bool {
	if strings.TrimSpace(pluginList) != "" {
		return false
	}
	if names, defined := pluginsNamesFromValuesFile(valuesFile); defined {
		return pluginsNamesValueEmpty(names)
	}
	return false
}

// managesPluginsStorage reports whether weaver should provision and wire the
// managed plugins PVC for this operation. It is the inverse of the baked-image
// signal (EffectivePluginsNamesEmpty): when the effective plugins.names is empty
// the chart needs no plugins volume (issue #913), so weaver provisions and
// injects nothing.
func (m *Manager) managesPluginsStorage() bool {
	return !EffectivePluginsNamesEmpty(m.blockNodeInputs.PluginList, m.blockNodeInputs.ValuesFile)
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

	// Add optional storage entries that apply at this chart version. Key each by
	// the chart's persistence key, not the kebab-case Name — application-state's
	// chart key is camelCase "applicationState", so keying by Name would write
	// blockNode.persistence.application-state, which the chart never reads,
	// leaving its StatefulSet to fall back to a volumeClaimTemplate (PVC stuck
	// Pending). PersistenceKey defaults to Name when unset (verification, plugins).
	for _, optStor := range GetApplicableOptionalStorages(m.blockNodeInputs.ChartVersion) {
		// #913: when the effective plugins.names is empty (plugins-baked image)
		// the chart renders no plugins volume, so weaver must not force an
		// existingClaim — doing so would mount an empty PVC read-only over the
		// image's baked plugins directory.
		if optStor.Name == "plugins" && !m.managesPluginsStorage() {
			continue
		}
		key := optStor.PersistenceKey
		if key == "" {
			key = optStor.Name
		}
		entries = append(entries, persistenceEntry{
			name:      key,
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

// injectPluginsConfig injects the resolved plugin list into plugins.names in the Helm values
// when PluginList is non-empty. If PluginList is empty the values content is returned unchanged,
// leaving the chart's built-in default plugin list intact.
func (m *Manager) injectPluginsConfig(valuesContent []byte) ([]byte, error) {
	pluginList := m.blockNodeInputs.PluginList

	// #913: for a plugins-baked image the effective plugins.names is empty, and we
	// must set it explicitly to "" so Helm does not re-apply the chart's own
	// non-empty plugins.names default (which would render a plugins volume/mount +
	// the Maven resolve init). bakedEmpty implies pluginList == "".
	bakedEmpty := !m.managesPluginsStorage()

	// Nothing to inject unless we have a resolved list to set, or must force an
	// explicit empty for a baked image. Leaving it untouched keeps the base
	// template's / chart's default plugin list intact.
	if pluginList == "" && !bakedEmpty {
		return valuesContent, nil
	}

	var vals map[string]interface{}
	if err := yaml.Unmarshal(valuesContent, &vals); err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "failed to parse values YAML for plugins config injection")
	}

	// Navigate to plugins, creating the path if needed.
	plugins, ok := vals["plugins"].(map[string]interface{})
	if !ok {
		plugins = make(map[string]interface{})
		vals["plugins"] = plugins
	}

	if bakedEmpty {
		logx.As().Info().
			Msg("Plugins-baked image (effective plugins.names empty): forcing plugins.names=\"\" and skipping the managed plugins PVC")
		plugins["names"] = ""
	} else {
		logx.As().Info().
			Str("preset", m.blockNodeInputs.PluginPreset).
			Str("plugins_names", pluginList).
			Msg("Applying plugin list to block node config")
		plugins["names"] = pluginList
	}

	result, err := yaml.Marshal(vals)
	if err != nil {
		return nil, errorx.InternalError.Wrap(err, "failed to marshal values YAML after plugins config injection")
	}

	return result, nil
}

// injectServiceAnnotations ensures the merged values express a LoadBalancer service when
// LoadBalancerEnabled is true. It (a) sets service.type to LoadBalancer if absent — defense
// in depth so the type can never go missing regardless of which values path is taken — and
// (b) merges the MetalLB address-pool annotation into service.annotations.
//
// When the operator's values file already contains either field, the existing value is
// left untouched. An explicit non-LoadBalancer service.type triggers a warning so the
// mismatch is visible in logs without weaver clobbering an explicit operator choice.
// When LoadBalancerEnabled is false the values are returned unchanged.
//
// Exception (issue #900): when the operator enables the chart's own loadBalancer block
// (loadBalancer.enabled: true — the "split topology": a ClusterIP main Service plus a
// separate "-external" LoadBalancer Service), the chart renders that external Service and
// applies its annotations from .Values.loadBalancer.annotations. In that case weaver defers
// entirely to the chart: it does not warn about a ClusterIP service.type and does not inject
// a MetalLB annotation onto the main service (which would be inert there, since MetalLB
// ignores non-LoadBalancer services). The reachability probe already locates the chart's
// "-external" Service, so no service.* injection is needed.
func (m *Manager) injectServiceAnnotations(valuesContent []byte) ([]byte, error) {
	if !m.blockNodeInputs.LoadBalancerEnabled {
		return valuesContent, nil
	}

	var vals map[string]interface{}
	if err := yaml.Unmarshal(valuesContent, &vals); err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "failed to parse values YAML for service annotation injection")
	}

	// Issue #900: if the operator drives the external endpoint through the chart's own
	// loadBalancer block, the chart owns the "-external" Service and its annotations. Weaver
	// must not warn about a ClusterIP main service.type or inject an inert MetalLB annotation
	// onto it — defer entirely to the chart.
	if chartOwnsLoadBalancer(vals) {
		logx.As().Debug().Msg("values enable the chart's loadBalancer block; deferring the external Service and its MetalLB annotation to the chart (skipping service.* injection)")
		return valuesContent, nil
	}

	service, ok := vals["service"].(map[string]interface{})
	if !ok {
		service = make(map[string]interface{})
		vals["service"] = service
	}

	const (
		typeKey       = "type"
		typeLB        = "LoadBalancer"
		annotationKey = "metallb.io/address-pool"
		defaultPool   = "public-address-pool"
	)

	mutated := false

	switch t := service[typeKey].(type) {
	case nil:
		service[typeKey] = typeLB
		logx.As().Info().Msg("Injecting service.type: LoadBalancer (LoadBalancerEnabled=true and no operator override)")
		mutated = true
	case string:
		if t != typeLB {
			logx.As().Warn().
				Str("service.type", t).
				Msg("LoadBalancerEnabled=true but operator values set service.type to a non-LoadBalancer value; leaving as-is — verify-block-node-reachable will fail")
		}
	default:
		logx.As().Warn().
			Str("service.type", fmt.Sprintf("%v", t)).
			Msg("LoadBalancerEnabled=true but operator values set service.type to a non-string value; leaving as-is")
	}

	annotations, ok := service["annotations"].(map[string]interface{})
	if !ok {
		annotations = make(map[string]interface{})
	}
	if existing, alreadySet := annotations[annotationKey]; alreadySet {
		logx.As().Debug().
			Str(annotationKey, fmt.Sprintf("%v", existing)).
			Msg("metallb.io/address-pool already set in values file; skipping injection")
	} else {
		annotations[annotationKey] = defaultPool
		service["annotations"] = annotations
		logx.As().Info().Msg("Injecting MetalLB address-pool annotation into service.annotations")
		mutated = true
	}

	if !mutated {
		return valuesContent, nil
	}

	result, err := yaml.Marshal(vals)
	if err != nil {
		return nil, errorx.InternalError.Wrap(err, "failed to marshal values YAML after service annotation injection")
	}

	return result, nil
}

// chartOwnsLoadBalancer reports whether the merged values enable the block-node chart's own
// loadBalancer block (loadBalancer.enabled: true). When enabled, the chart renders a dedicated
// "-external" LoadBalancer Service and applies its annotations from .Values.loadBalancer.annotations,
// so weaver must not inject its own service.type/annotations for the LoadBalancer. See issue #900.
//
// The value is read for truthiness rather than a strict bool type: an operator may quote it
// (loadBalancer.enabled: "true"), which Helm still treats as enabled, so a bool-only check would
// misread that as disabled and re-introduce the misleading warning + inert injection this gate
// suppresses. A native bool is honored directly; a string is parsed with strconv.ParseBool
// (accepting "true"/"1"/"t" and "false"/"0"/"f", case-insensitive). Any other shape — absent
// block, missing key, unparseable value — is treated as not-enabled, preserving the default
// single-LB path and the accurate ClusterIP-with-no-LB warning.
func chartOwnsLoadBalancer(vals map[string]interface{}) bool {
	lb, ok := vals["loadBalancer"].(map[string]interface{})
	if !ok {
		return false
	}
	switch enabled := lb["enabled"].(type) {
	case bool:
		return enabled
	case string:
		parsed, err := strconv.ParseBool(enabled)
		return err == nil && parsed
	default:
		return false
	}
}
