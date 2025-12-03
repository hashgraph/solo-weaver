// SPDX-License-Identifier: Apache-2.0

package software

import (
	"bytes"
	"embed"
	"fmt"
	"runtime"
	"sort"
	"text/template"

	"golang.hedera.com/solo-weaver/pkg/semver"
	"gopkg.in/yaml.v3"
)

//go:embed artifact.yaml
var artifactConfigFS embed.FS

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

// ArtifactCollection represents the root configuration structure
type ArtifactCollection struct {
	Artifact []ArtifactMetadata `yaml:"artifact"`
}

// ArtifactMetadata represents a single software artifact  configuration
// with its versions which including archives, binaries and configuration files
type ArtifactMetadata struct {
	Name     string                     `yaml:"name"`
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

// LoadArtifactConfig loads and parses the artifact.yaml configuration
func LoadArtifactConfig() (*ArtifactCollection, error) {
	data, err := artifactConfigFS.ReadFile("artifact.yaml")
	if err != nil {
		return nil, NewConfigLoadError(err)
	}

	var config ArtifactCollection
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, NewConfigLoadError(err)
	}

	return &config, nil
}

// GetArtifactByName finds a artifact item by name
func (sc *ArtifactCollection) GetArtifactByName(name string) (*ArtifactMetadata, error) {
	for i, item := range sc.Artifact {
		if item.Name == name {
			return &sc.Artifact[i], nil
		}
	}
	return nil, NewSoftwareNotFoundError(name)
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

// GetLatestVersion returns the latest versionToBeInstalled available for this software item
// Only supports semantic versions - returns error for non-semantic versionToBeInstalled formats
func (si *ArtifactMetadata) GetLatestVersion() (string, error) {
	if len(si.Versions) == 0 {
		return "", NewVersionNotFoundError(si.Name, "any")
	}

	// If there's only one versionToBeInstalled, return it if it's valid semantic versionToBeInstalled
	if len(si.Versions) == 1 {
		for version := range si.Versions {
			versionStr := string(version)
			if !isValidVersion(versionStr) {
				return "", fmt.Errorf("versionToBeInstalled %s for software %s is not in semantic versionToBeInstalled format", versionStr, si.Name)
			}
			return versionStr, nil
		}
	}

	// Collect and validate all versionToBeInstalled strings
	versions := make([]string, 0, len(si.Versions))

	for version := range si.Versions {
		versionStr := string(version)
		if !isValidVersion(versionStr) {
			return "", fmt.Errorf("versionToBeInstalled %s for software %s is not in semantic versionToBeInstalled format", versionStr, si.Name)
		}
		versions = append(versions, versionStr)
	}

	// Sort versions using semantic versionToBeInstalled comparison
	sort.Slice(versions, func(i, j int) bool {
		// Use the semantic versionToBeInstalled comparator for sorting
		result, err := compareVersions(versions[i], versions[j])
		if err != nil {
			// This shouldn't happen since we validated versions above, but handle gracefully
			return false
		}
		return result
	})

	// Return the first (latest) versionToBeInstalled
	return versions[0], nil
}

func compareVersions(version1, version2 string) (bool, error) {
	v1, err1 := semver.NewSemver(version1)
	v2, err2 := semver.NewSemver(version2)

	if err1 != nil {
		return false, fmt.Errorf("invalid semantic versionToBeInstalled format: %s", version1)
	}
	if err2 != nil {
		return false, fmt.Errorf("invalid semantic versionToBeInstalled format: %s", version2)
	}

	return v1.GreaterThan(v2), nil
}

func isValidVersion(version string) bool {
	_, err := semver.NewSemver(version)
	return err == nil
}
