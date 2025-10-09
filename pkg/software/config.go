package software

import (
	"bytes"
	"embed"
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
func (si *SoftwareItem) GetLatestVersion() (string, error) {
	if len(si.Versions) == 0 {
		return "", NewVersionNotFoundError(si.Name, "any")
	}

	// If there's only one version, return it
	if len(si.Versions) == 1 {
		for version := range si.Versions {
			return string(version), nil
		}
	}

	// Collect all version strings
	versions := make([]string, 0, len(si.Versions))
	for version := range si.Versions {
		versions = append(versions, string(version))
	}

	// Sort versions using semantic version comparison
	sort.Slice(versions, func(i, j int) bool {
		// Parse versions and compare - newer versions come first (descending order)
		versionI, errI := semver.NewSemver(versions[i])
		versionJ, errJ := semver.NewSemver(versions[j])

		// If parsing fails for either version, fall back to string comparison
		if errI != nil || errJ != nil {
			return versions[i] > versions[j]
		}

		// Return true if versionI is greater than versionJ (for descending sort)
		return versionI.GreaterThan(versionJ)
	})

	// Return the first (latest) version
	return versions[0], nil
}
