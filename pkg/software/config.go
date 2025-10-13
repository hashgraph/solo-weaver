package software

import (
	"bytes"
	"embed"
	"fmt"
	"runtime"
	"sort"
	"text/template"

	"golang.hedera.com/solo-provisioner/pkg/semver"
	"gopkg.in/yaml.v3"
)

//go:embed software.yaml
var softwareConfigFS embed.FS

// Semantic types + enums

type Version string
type OS string
type Arch string

const (
	OSLinux   OS = "linux"
	OSDarwin  OS = "darwin"
	OSWindows OS = "windows"

	ArchAMD64 Arch = "amd64"
	ArchARM64 Arch = "arm64"
)

// platformProvider provides OS and architecture information.
// It defaults to the runtime's OS and architecture.
type platformProvider struct {
	os   string
	arch string
}

// withPlatform sets a custom platform for the software softwareToBeInstalled.
// This is primarily used for testing purposes to simulate different OS and architecture combinations.
func (si *SoftwareMetadata) withPlatform(currentOS, currentArch string) *SoftwareMetadata {
	s := *si
	s.platform = &platformProvider{os: currentOS, arch: currentArch}
	return &s
}

// getPlatform returns the platformProvider for the software softwareToBeInstalled.
// If no platformProvider is set, it returns the runtime's OS and architecture.
func (si *SoftwareMetadata) getPlatform() platformProvider {
	if si.platform == nil {
		return platformProvider{runtime.GOOS, runtime.GOARCH}
	}
	return *si.platform
}

// SoftwareCollection represents the root configuration structure
type SoftwareCollection struct {
	Software []SoftwareMetadata `yaml:"software"`
}

// SoftwareMetadata represents a single software package configuration
// with its versions which including binaries and configuration files
type SoftwareMetadata struct {
	Name     string                     `yaml:"name"`
	URL      string                     `yaml:"url"`
	Filename string                     `yaml:"filename"`
	Versions map[Version]VersionDetails `yaml:"versions"`
	platform *platformProvider
}

// VersionDetails represents the structure for a specific versionToBeInstalled
// including the binary and related configuration files
type VersionDetails struct {
	Binary  BinaryDetails  `yaml:"binary"`
	Configs []ConfigDetail `yaml:"configs,omitempty"`
}

// BinaryDetails represents the platform-specific information for a versionToBeInstalled
type BinaryDetails map[OS]map[Arch]Checksum

// Checksum contains platform-specific checksum information
type Checksum struct {
	Algorithm string `yaml:"algorithm"`
	Value     string `yaml:"checksum"`
}

// ConfigDetail represents a configuration file with its softwareToBeInstalled
type ConfigDetail struct {
	Name      string `yaml:"name"`
	URL       string `yaml:"url"`
	Filename  string `yaml:"filename"`
	Algorithm string `yaml:"algorithm"`
	Value     string `yaml:"checksum"`
}

// TemplateData contains the variables used in template substitution
type TemplateData struct {
	VERSION string
	OS      string
	ARCH    string
}

// LoadSoftwareConfig loads and parses the software.yaml configuration
func LoadSoftwareConfig() (*SoftwareCollection, error) {
	data, err := softwareConfigFS.ReadFile("software.yaml")
	if err != nil {
		return nil, NewConfigLoadError(err)
	}

	var config SoftwareCollection
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, NewConfigLoadError(err)
	}

	return &config, nil
}

// GetSoftwareByName finds a software item by name
func (sc *SoftwareCollection) GetSoftwareByName(name string) (*SoftwareMetadata, error) {
	for i, item := range sc.Software {
		if item.Name == name {
			return &sc.Software[i], nil
		}
	}
	return nil, NewSoftwareNotFoundError(name)
}

// GetChecksum retrieves the checksum for a specific versionToBeInstalled, OS, and architecture
func (si *SoftwareMetadata) GetChecksum(version string) (Checksum, error) {
	versionInfo, exists := si.Versions[Version(version)]
	if !exists {
		return Checksum{}, NewVersionNotFoundError(si.Name, version)
	}

	platform := si.getPlatform()

	osInfo, exists := versionInfo.Binary[OS(platform.os)]
	if !exists {
		return Checksum{}, NewPlatformNotFoundError(si.Name, version, platform.os, "")
	}

	checksum, exists := osInfo[Arch(platform.arch)]
	if !exists {
		return Checksum{}, NewPlatformNotFoundError(si.Name, version, platform.os, platform.arch)
	}

	return checksum, nil
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

// GetDownloadURL returns the download URL with template variables replaced
func (si *SoftwareMetadata) GetDownloadURL(version string) (string, error) {
	platform := si.getPlatform()

	data := TemplateData{
		VERSION: version,
		OS:      platform.os,
		ARCH:    platform.arch,
	}
	result, err := executeTemplate(si.URL, data)
	if err != nil {
		return "", NewTemplateError(err, si.Name)
	}
	return result, nil
}

// GetFilename returns the filename with template variables replaced
func (si *SoftwareMetadata) GetFilename(version string) (string, error) {
	platform := si.getPlatform()

	data := TemplateData{
		VERSION: version,
		OS:      platform.os,
		ARCH:    platform.arch,
	}
	result, err := executeTemplate(si.Filename, data)
	if err != nil {
		return "", NewTemplateError(err, si.Name)
	}
	return result, nil
}

// GetConfigs retrieves all configuration files for a specific versionToBeInstalled
func (si *SoftwareMetadata) GetConfigs(version string) ([]ConfigDetail, error) {
	versionInfo, exists := si.Versions[Version(version)]
	if !exists {
		return nil, NewVersionNotFoundError(si.Name, version)
	}

	return versionInfo.Configs, nil
}

// GetConfigByName retrieves a specific configuration file by name for a given versionToBeInstalled
func (si *SoftwareMetadata) GetConfigByName(version, configName string) (*ConfigDetail, error) {
	configs, err := si.GetConfigs(version)
	if err != nil {
		return nil, err
	}

	for _, config := range configs {
		if config.Name == configName {
			return &config, nil
		}
	}

	return nil, fmt.Errorf("configuration file '%s' not found for software '%s' versionToBeInstalled '%s'", configName, si.Name, version)
}

// GetLatestVersion returns the latest versionToBeInstalled available for this software item
// Only supports semantic versions - returns error for non-semantic versionToBeInstalled formats
func (si *SoftwareMetadata) GetLatestVersion() (string, error) {
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
