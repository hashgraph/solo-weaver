// SPDX-License-Identifier: Apache-2.0

package software

import (
	"bytes"
	"embed"
	"fmt"
	"runtime"
	"sort"
	"sync"
	"text/template"

	"github.com/hashgraph/solo-weaver/pkg/semver"
	"gopkg.in/yaml.v3"
)

//go:embed infrastructure-catalog.yaml
var infrastructureCatalogFS embed.FS

// ChartType identifies how a Helm chart is distributed.
type ChartType string

const (
	ChartTypeClassic ChartType = "classic"
	ChartTypeOCI     ChartType = "oci"
)

// Semantic types + enums

type Version string

const (
	OSLinux   string = "linux"
	OSDarwin  string = "darwin"
	OSWindows string = "windows"

	ArchAMD64 string = "amd64"
	ArchARM64 string = "arm64"
)

// platformProvider provides OS and architecture information.
// It defaults to the runtime's OS and architecture.
type platformProvider struct {
	os   string
	arch string
}

// withPlatform sets a custom platform for the software artifact.
// This is primarily used for testing purposes to simulate different OS and architecture combinations.
func (si *ArtifactMetadata) withPlatform(currentOS, currentArch string) *ArtifactMetadata {
	s := *si
	s.platform = &platformProvider{os: currentOS, arch: currentArch}
	return &s
}

// getPlatform returns the platformProvider for the software artifact.
// If no platformProvider is set, it returns the runtime's OS and architecture.
func (si *ArtifactMetadata) getPlatform() platformProvider {
	if si.platform == nil {
		return platformProvider{runtime.GOOS, runtime.GOARCH}
	}
	return *si.platform
}

// InfrastructureCatalog is the root structure of the embedded
// infrastructure-catalog.yaml file shipped inside the solo-provisioner
// binary. It groups infrastructure components by where they run: `host:`
// for binaries installed on the host, `cluster:` for Helm charts
// installed into Kubernetes. The catalog's `default:` field is the
// single source of truth for which version of each component gets
// installed at runtime.
//
// A separate file named `manifests/infrastructure-versions.yaml`
// (shipped inside the CN release package, minimal `name`+`version`
// schema) acts as a declarative audit list that the provisioner
// cross-checks against this catalog at apply time — it does not direct
// version selection. The intentional filename split (`-catalog` vs
// `-versions`) avoids any suggestion that the two are interchangeable.
// A hidden emergency CLI flag (`--override-component <name>=<version>`,
// guarded by `--confirm-untested-combination`) will additionally allow
// overriding a single component's version, but only among versions the
// catalog already declares. See docs/dev/chart-checksums.md
// ("Future overrides") for details.
type InfrastructureCatalog struct {
	Host    []ArtifactMetadata `yaml:"host"`
	Cluster []ChartMetadata    `yaml:"cluster"`
}

// ChartMetadata describes a Helm chart entry under `cluster:`. A chart has
// a single artifact per version; integrity is verified against the SHA256
// of the .tgz (classic) or the OCI manifest digest (oci), recorded as a
// Checksum value. Namespace and Release describe the installation topology
// — the Kubernetes namespace the chart deploys into and the Helm release
// name used to track it.
type ChartMetadata struct {
	Name      string               `yaml:"name"`
	Type      ChartType            `yaml:"type"`
	Repo      string               `yaml:"repo,omitempty"`
	Chart     string               `yaml:"chart"`
	Namespace string               `yaml:"namespace"`
	Release   string               `yaml:"release"`
	Default   Version              `yaml:"default"`
	Versions  map[Version]Checksum `yaml:"versions"`
}

// ArtifactMetadata represents a single software artifact  configuration
// with its versions which including archives, binaries and configuration files
type ArtifactMetadata struct {
	Name     string                     `yaml:"name"`
	Default  Version                    `yaml:"default"`
	Versions map[Version]VersionDetails `yaml:"versions"`
	platform *platformProvider
}

// VersionDetails represents the structure for a specific versionToBeInstalled
// including the archive, binary and related configuration files
type VersionDetails struct {
	Archives []ArchiveDetail `yaml:"archives,omitempty"`
	Binaries []BinaryDetail  `yaml:"binaries"`
	Configs  []ConfigDetail  `yaml:"configs,omitempty"`
}

// GetArchives retrieves all archives, sorted by name for consistent order
func (v VersionDetails) GetArchives() []ArchiveDetail {
	archives := make([]ArchiveDetail, len(v.Archives))
	copy(archives, v.Archives)

	// Sort by name for consistent order
	sort.Slice(archives, func(i, j int) bool {
		return archives[i].Name < archives[j].Name
	})

	return archives
}

// GetBinaries retrieves all binaries, sorted by name for consistent order
func (v VersionDetails) GetBinaries() []BinaryDetail {
	binaries := make([]BinaryDetail, len(v.Binaries))
	copy(binaries, v.Binaries)

	// Sort by name for consistent order
	sort.Slice(binaries, func(i, j int) bool {
		return binaries[i].Name < binaries[j].Name
	})

	return binaries
}

// BinariesByURL returns only the binaries that are downloaded directly via URL
func (v VersionDetails) BinariesByURL() []BinaryDetail {
	downloadable := make([]BinaryDetail, 0)
	for _, binary := range v.GetBinaries() {
		if binary.URL != "" {
			downloadable = append(downloadable, binary)
		}
	}
	return downloadable
}

// BinariesByArchive returns only the binaries that are extracted from archives
func (v VersionDetails) BinariesByArchive() []BinaryDetail {
	extracted := make([]BinaryDetail, 0)
	for _, binary := range v.GetBinaries() {
		if binary.Archive != "" {
			extracted = append(extracted, binary)
		}
	}
	return extracted
}

// GetConfigs retrieves all configuration files, sorted by name for consistent order
func (v VersionDetails) GetConfigs() []ConfigDetail {
	configs := make([]ConfigDetail, len(v.Configs))
	copy(configs, v.Configs)

	// Sort by name for consistent order
	sort.Slice(configs, func(i, j int) bool {
		return configs[i].Name < configs[j].Name
	})

	return configs
}

// ConfigsByURL returns only the configuration files that are downloaded directly via URL
func (v VersionDetails) ConfigsByURL() []ConfigDetail {
	downloadable := make([]ConfigDetail, 0)
	for _, config := range v.GetConfigs() {
		if config.URL != "" && config.Archive == "" {
			downloadable = append(downloadable, config)
		}
	}
	return downloadable
}

// ConfigsByArchive returns only the configuration files that are extracted from archives
func (v VersionDetails) ConfigsByArchive() []ConfigDetail {
	extracted := make([]ConfigDetail, 0)
	for _, config := range v.GetConfigs() {
		if config.Archive != "" {
			extracted = append(extracted, config)
		}
	}
	return extracted
}

// GetArchiveByName retrieves a specific archive file by name
func (v VersionDetails) GetArchiveByName(archiveName string) (*ArchiveDetail, error) {
	for i := range v.Archives {
		if v.Archives[i].Name == archiveName {
			return &v.Archives[i], nil
		}
	}
	return nil, fmt.Errorf("archive file '%s' not found", archiveName)
}

// GetBinaryByName retrieves a specific binary file by name
func (v VersionDetails) GetBinaryByName(binaryName string) (*BinaryDetail, error) {
	for i := range v.Binaries {
		if v.Binaries[i].Name == binaryName {
			return &v.Binaries[i], nil
		}
	}
	return nil, fmt.Errorf("binary file '%s' not found", binaryName)
}

// GetConfigByName retrieves a specific configuration file by name
func (v VersionDetails) GetConfigByName(configName string) (*ConfigDetail, error) {
	for i := range v.Configs {
		if v.Configs[i].Name == configName {
			return &v.Configs[i], nil
		}
	}
	return nil, fmt.Errorf("configuration file '%s' not found", configName)
}

type ArchiveDetail struct {
	Name             string `yaml:"name"`
	URL              string `yaml:"url"`
	PlatformChecksum `yaml:",inline"`
}

type BinaryDetail struct {
	Name             string `yaml:"name"`
	URL              string `yaml:"url,omitempty"`
	Archive          string `yaml:"archive,omitempty"`
	PlatformChecksum `yaml:",inline"`
}

// ConfigDetail represents a configuration file with its software
type ConfigDetail struct {
	Name      string `yaml:"name"`
	URL       string `yaml:"url"`
	Archive   string `yaml:"archive,omitempty"`
	Algorithm string `yaml:"algorithm"`
	Value     string `yaml:"checksum"`
}

// PlatformChecksum maps OS and ARCH to their respective checksums
// Format: map[OS]map[ARCH]Checksum
// e.g. in yaml format:
//
//	linux:
//	  amd64:
//	    algorithm: sha256
//	    checksum: abcdef...
type PlatformChecksum map[string]map[string]Checksum

// Checksum contains platform-specific checksum information
type Checksum struct {
	Algorithm string `yaml:"algorithm"`
	Value     string `yaml:"checksum"`
}

// TemplateData contains the variables used in template substitution
type TemplateData struct {
	VERSION string
	OS      string
	ARCH    string
}

var (
	cachedCatalog    *InfrastructureCatalog
	cachedCatalogErr error
	loadCatalogOnce  sync.Once
)

// ResetCatalogCacheForTest clears the cached catalog so the next
// LoadInfrastructureCatalog call re-reads, re-parses, and re-validates the
// embedded YAML. Intended for tests that exercise load-failure paths or
// swap in alternative embedded data; not safe to call concurrently with
// LoadInfrastructureCatalog. The name is deliberately verbose so it stands
// out at call sites — production code must never invoke this.
func ResetCatalogCacheForTest() {
	cachedCatalog = nil
	cachedCatalogErr = nil
	loadCatalogOnce = sync.Once{}
}

// LoadInfrastructureCatalog loads, parses, and validates the embedded
// infrastructure-catalog.yaml configuration. The result is cached for the
// lifetime of the process — the catalog is embedded in the binary and
// immutable, so callers must not mutate the returned value.
func LoadInfrastructureCatalog() (*InfrastructureCatalog, error) {
	loadCatalogOnce.Do(func() {
		data, err := infrastructureCatalogFS.ReadFile("infrastructure-catalog.yaml")
		if err != nil {
			cachedCatalogErr = NewConfigLoadError(err)
			return
		}

		var catalog InfrastructureCatalog
		if err := yaml.Unmarshal(data, &catalog); err != nil {
			cachedCatalogErr = NewConfigLoadError(err)
			return
		}

		if err := catalog.validate(); err != nil {
			cachedCatalogErr = NewConfigLoadError(err)
			return
		}

		cachedCatalog = &catalog
	})
	return cachedCatalog, cachedCatalogErr
}

// GetHostArtifact finds a host artifact entry by name.
func (c *InfrastructureCatalog) GetHostArtifact(name string) (*ArtifactMetadata, error) {
	for i, item := range c.Host {
		if item.Name == name {
			return &c.Host[i], nil
		}
	}
	return nil, NewSoftwareNotFoundError(name)
}

// GetClusterComponent finds a cluster Helm chart entry by name.
func (c *InfrastructureCatalog) GetClusterComponent(name string) (*ChartMetadata, error) {
	for i, item := range c.Cluster {
		if item.Name == name {
			return &c.Cluster[i], nil
		}
	}
	return nil, NewSoftwareNotFoundError(name)
}

// MustGetClusterComponent returns the named cluster chart from the embedded
// catalog. It panics if the catalog cannot be loaded or the chart is not
// present — both conditions are impossible at runtime because the catalog
// is embedded into the binary and validated at load time. Callers that
// reference catalog entries by literal name (steps, RSL defaults, manifest
// rendering) use this to avoid threading uninteresting errors through every
// call site.
func MustGetClusterComponent(name string) *ChartMetadata {
	catalog, err := LoadInfrastructureCatalog()
	if err != nil {
		panic(fmt.Sprintf("infrastructure catalog: %v", err))
	}
	chart, err := catalog.GetClusterComponent(name)
	if err != nil {
		panic(fmt.Sprintf("infrastructure catalog: %v", err))
	}
	return chart
}

// HostNames returns the names of all host artifacts in the catalog.
func (c *InfrastructureCatalog) HostNames() []string {
	names := make([]string, 0, len(c.Host))
	for _, item := range c.Host {
		names = append(names, item.Name)
	}
	return names
}

// ClusterNames returns the names of all cluster components in the catalog.
func (c *InfrastructureCatalog) ClusterNames() []string {
	names := make([]string, 0, len(c.Cluster))
	for _, item := range c.Cluster {
		names = append(names, item.Name)
	}
	return names
}

// validate enforces the catalog schema invariants documented in
// infrastructure-catalog.yaml: every entry must declare an explicit default
// that resolves to a known version, cluster entries must declare a valid
// type, classic entries must carry a repo (and oci entries must not), and
// every chart version must have a non-empty algorithm and checksum.
func (c *InfrastructureCatalog) validate() error {
	for i := range c.Host {
		host := &c.Host[i]
		if _, err := host.GetDefaultVersion(); err != nil {
			return fmt.Errorf("host[%s]: %w", host.Name, err)
		}
	}
	for i := range c.Cluster {
		chart := &c.Cluster[i]
		if err := chart.validate(); err != nil {
			return fmt.Errorf("cluster[%s]: %w", chart.Name, err)
		}
	}
	return nil
}

// validate enforces ChartMetadata invariants. It is called by
// InfrastructureCatalog.validate() at load time so callers never observe a
// malformed chart entry.
//
// Checks are ordered from most fundamental to most derived: the type/repo
// shape comes first, then the chart reference, then per-version integrity
// records, and finally the default resolution. This keeps validation
// deterministic and ensures malformed structural fields are rejected before
// derived semantic checks such as resolving the default version.
func (cm *ChartMetadata) validate() error {
	switch cm.Type {
	case ChartTypeClassic:
		if cm.Repo == "" {
			return fmt.Errorf("classic chart must declare a repo")
		}
	case ChartTypeOCI:
		if cm.Repo != "" {
			return fmt.Errorf("oci chart must not declare a repo (repo is encoded in the chart reference)")
		}
	case "":
		return fmt.Errorf("missing type (must be %q or %q)", ChartTypeClassic, ChartTypeOCI)
	default:
		return fmt.Errorf("unknown type %q (must be %q or %q)", cm.Type, ChartTypeClassic, ChartTypeOCI)
	}
	if cm.Chart == "" {
		return fmt.Errorf("missing chart reference")
	}
	if cm.Namespace == "" {
		return fmt.Errorf("missing namespace")
	}
	if cm.Release == "" {
		return fmt.Errorf("missing release")
	}
	if len(cm.Versions) == 0 {
		return fmt.Errorf("no versions declared")
	}
	// Iterate over versions in a stable, alphabetical order so that when
	// more than one version is malformed the error message always names
	// the same one (Go map iteration order is randomized). Mirrors the
	// convention used by VersionDetails.GetArchives/Binaries/Configs.
	versions := make([]Version, 0, len(cm.Versions))
	for v := range cm.Versions {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	for _, version := range versions {
		details := cm.Versions[version]
		if details.Algorithm == "" {
			return fmt.Errorf("version %s: empty algorithm", version)
		}
		if details.Value == "" {
			return fmt.Errorf("version %s: empty checksum", version)
		}
	}
	if _, err := cm.GetDefaultVersion(); err != nil {
		return err
	}
	return nil
}

// GetDefaultVersion returns the explicit default version declared for this
// chart. The default is read from the chart's `default:` field; it must point
// to a version present in the `versions:` map. Returns an error when
// `default:` is unset or names an unknown version.
func (cm *ChartMetadata) GetDefaultVersion() (string, error) {
	if cm.Default == "" {
		return "", fmt.Errorf("chart %s has no default version declared", cm.Name)
	}
	if _, ok := cm.Versions[cm.Default]; !ok {
		return "", NewVersionNotFoundError(cm.Name, string(cm.Default))
	}
	return string(cm.Default), nil
}

// executeTemplate executes a template string with the given data
func executeTemplate(templateStr string, data TemplateData) (string, error) {
	tmpl, err := template.New("software").Parse(templateStr)
	if err != nil {
		return "", NewTemplateError(err, "")
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", NewTemplateError(err, "")
	}

	return buf.String(), nil
}

// GetDefaultVersion returns the explicit default version declared for this
// software item. The default is read from the artifact's `default:` field;
// it must point to a version present in the `versions:` map. Returns an
// error when `default:` is unset or names an unknown version.
func (si *ArtifactMetadata) GetDefaultVersion() (string, error) {
	if si.Default == "" {
		return "", fmt.Errorf("artifact %s has no default version declared", si.Name)
	}
	if _, ok := si.Versions[si.Default]; !ok {
		return "", NewVersionNotFoundError(si.Name, string(si.Default))
	}
	return string(si.Default), nil
}

func isValidVersion(version string) bool {
	_, err := semver.NewSemver(version)
	return err == nil
}
