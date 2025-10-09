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

// SoftwareConfig represents the root configuration structure
type SoftwareConfig struct {
	Software []SoftwareItem `yaml:"software"`
}

// SoftwareItem represents a single software package configuration
// with its versions
type SoftwareItem struct {
	Name     string                `yaml:"name"`
	URL      string                `yaml:"url"`
	Filename string                `yaml:"filename"`
	Versions map[Version]Platforms `yaml:"versions"`
}

// Platforms represents the platform-specific information for a version
type Platforms map[OS]map[Arch]Checksum

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

// LoadSoftwareConfig loads and parses the software.yaml configuration
func LoadSoftwareConfig() (*SoftwareConfig, error) {
	data, err := softwareConfigFS.ReadFile("software.yaml")
	if err != nil {
		return nil, NewConfigLoadError(err)
	}

	var config SoftwareConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, NewConfigLoadError(err)
	}

	return &config, nil
}

// GetSoftwareByName finds a software item by name
func (sc *SoftwareConfig) GetSoftwareByName(name string) (*SoftwareItem, error) {
	for _, item := range sc.Software {
		if item.Name == name {
			return &item, nil
		}
	}
	return nil, NewSoftwareNotFoundError(name)
}

// GetChecksum retrieves the checksum for a specific version, OS, and architecture
func (si *SoftwareItem) GetChecksum(version string) (Checksum, error) {
	versionInfo, exists := si.Versions[Version(version)]
	if !exists {
		return Checksum{}, NewVersionNotFoundError(si.Name, version)
	}

	os := runtime.GOOS
	osInfo, exists := versionInfo[OS(os)]
	if !exists {
		return Checksum{}, NewPlatformNotFoundError(si.Name, version, os, "")
	}

	arch := runtime.GOARCH
	checksum, exists := osInfo[Arch(arch)]
	if !exists {
		return Checksum{}, NewPlatformNotFoundError(si.Name, version, os, arch)
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
func (si *SoftwareItem) GetDownloadURL(version string) (string, error) {
	data := TemplateData{
		VERSION: version,
		OS:      runtime.GOOS,
		ARCH:    runtime.GOARCH,
	}
	result, err := executeTemplate(si.URL, data)
	if err != nil {
		return "", NewTemplateError(err, si.Name)
	}
	return result, nil
}

// GetFilename returns the filename with template variables replaced
func (si *SoftwareItem) GetFilename(version string) (string, error) {
	data := TemplateData{
		VERSION: version,
		OS:      runtime.GOOS,
		ARCH:    runtime.GOARCH,
	}
	result, err := executeTemplate(si.Filename, data)
	if err != nil {
		return "", NewTemplateError(err, si.Name)
	}
	return result, nil
}

// GetLatestVersion returns the latest version available for this software item
// Only supports semantic versions - returns error for non-semantic version formats
func (si *SoftwareItem) GetLatestVersion() (string, error) {
	if len(si.Versions) == 0 {
		return "", NewVersionNotFoundError(si.Name, "any")
	}

	// If there's only one version, return it if it's valid semantic version
	if len(si.Versions) == 1 {
		for version := range si.Versions {
			versionStr := string(version)
			if !isValidVersion(versionStr) {
				return "", fmt.Errorf("version %s for software %s is not in semantic version format", versionStr, si.Name)
			}
			return versionStr, nil
		}
	}

	// Collect and validate all version strings
	versions := make([]string, 0, len(si.Versions))

	for version := range si.Versions {
		versionStr := string(version)
		if !isValidVersion(versionStr) {
			return "", fmt.Errorf("version %s for software %s is not in semantic version format", versionStr, si.Name)
		}
		versions = append(versions, versionStr)
	}

	// Sort versions using semantic version comparison
	sort.Slice(versions, func(i, j int) bool {
		// Use the semantic version comparator for sorting
		result, err := compareVersions(versions[i], versions[j])
		if err != nil {
			// This shouldn't happen since we validated versions above, but handle gracefully
			return false
		}
		return result
	})

	// Return the first (latest) version
	return versions[0], nil
}

func compareVersions(version1, version2 string) (bool, error) {
	v1, err1 := semver.NewSemver(version1)
	v2, err2 := semver.NewSemver(version2)

	if err1 != nil {
		return false, fmt.Errorf("invalid semantic version format: %s", version1)
	}
	if err2 != nil {
		return false, fmt.Errorf("invalid semantic version format: %s", version2)
	}

	return v1.GreaterThan(v2), nil
}

func isValidVersion(version string) bool {
	_, err := semver.NewSemver(version)
	return err == nil
}
