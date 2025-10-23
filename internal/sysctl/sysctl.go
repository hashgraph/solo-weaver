package sysctl

import (
	"os"
	"path"
	"sort"
	"strings"

	"github.com/lorenzosaino/go-sysctl"
	"golang.hedera.com/solo-provisioner/internal/core"
	"golang.hedera.com/solo-provisioner/internal/templates"
)

const (
	TemplatesDir  = "files/sysctl"
	EtcSysctlDir  = "/etc/sysctl.d"
	EtcSysctlConf = "/etc/sysctl.conf"
)

// use var to allow mocking in tests
var (
	sysctlConfigSourceDir      = TemplatesDir
	sysctlConfigDestinationDir = EtcSysctlDir
	defaultSysctlConf          = EtcSysctlConf
)

// BackupSettings backs up the current sysctl settings that will be modified by the template configurations
// If the backup file already exists, it will not be overwritten.
// It returns the path to the backup file.
func BackupSettings(backupFile string) (string, error) {
	// if backup file already exists, do not overwrite
	if _, err := os.Stat(backupFile); err == nil {
		return backupFile, nil
	}

	currentCandidateSettings, err := CurrentCandidateSettings()
	if err != nil {
		return "", err
	}

	// store current settings in back up location
	var lines []string
	for k, v := range currentCandidateSettings {
		// split v by new line and concat to k
		parts := strings.Split(v, "\n")
		for _, part := range parts {
			// important to have spaces around '=' for consistency with sysctl -a output
			lines = append(lines, k+" = "+part)
		}
	}

	err = os.MkdirAll(path.Dir(backupFile), core.DefaultDirOrExecPerm)
	if err != nil {
		return "", err
	}

	err = os.WriteFile(backupFile, []byte(strings.Join(lines, "\n")), core.DefaultFilePerm)
	if err != nil {
		return "", err
	}

	return backupFile, nil
}

// CopyConfiguration copies sysctl configuration files from the embedded templates to the /etc/sysctl.d directory.
// It overwrites existing files in the destination directory.
// It returns a list of copied files.
func CopyConfiguration() ([]string, error) {
	return templates.CopyTemplateFiles(sysctlConfigSourceDir, sysctlConfigDestinationDir)
}

// LoadAllConfiguration reloads sysctl settings from configuration files in /etc/sysctl.d and /etc/sysctl.conf.
// It returns a list of configuration files that were used to reload sysctl settings.
// If no configuration files are found, it will still reload sysctl settings from /etc/sysctl.conf if it exists.
func LoadAllConfiguration() ([]string, error) {
	currentConfigFiles, err := FindSysctlConfigFiles()
	if err != nil {
		return currentConfigFiles, err
	}

	return LoadConfigurationFrom(currentConfigFiles)
}

// DesiredCandidateSettings returns a map of desired sysctl settings that are defined in the template configuration files.
// It loads the settings from the template files and returns them as a map.
// It returns a map of the desired settings and an error if any.
func DesiredCandidateSettings() (map[string]string, error) {
	// now load the settings from the template files and only keep a backup of those settings
	tempDir := path.Join(core.Paths().TempDir, "templates", "sysctl")
	err := os.MkdirAll(tempDir, core.DefaultDirOrExecPerm)
	if err != nil {
		return nil, err
	}

	templateFiles, err := templates.CopyTemplateFiles(sysctlConfigSourceDir, tempDir)
	if err != nil {
		return nil, err
	}

	// for each config file, load the settings and filter the current settings to only keep those
	candidateSettings, err := sysctl.LoadConfig(templateFiles...)

	return candidateSettings, err
}

// CurrentCandidateSettings returns a map of current sysctl settings that are defined in the template configuration files.
// It loads the current sysctl settings and the settings from the template files, and filters the current settings
// to only keep those that are defined in the template files.
// It returns a map of the filtered settings and an error if any.
func CurrentCandidateSettings() (map[string]string, error) {
	currentSettings, err := sysctl.GetAll()
	if err != nil {
		return nil, err
	}

	candidateSettings, err := DesiredCandidateSettings()
	if err != nil {
		return nil, err
	}

	// find the sysctl values for the keys in candidateSettings
	currentValuesForCandidates := make(map[string]string)
	if len(candidateSettings) > 0 {
		for k := range candidateSettings {
			if c, ok := currentSettings[k]; ok {
				currentValuesForCandidates[k] = c
			}
		}
	}

	return currentValuesForCandidates, nil
}

// RestoreSettings restores sysctl settings from the given backup file.
// It returns an error if the backup file does not exist or if the settings could not be applied.
func RestoreSettings(backupFile string) error {
	// check that backup file exists
	if _, err := os.Stat(backupFile); os.IsNotExist(err) {
		return os.ErrNotExist
	}

	// load settings from backup file
	err := sysctl.LoadConfigAndApply(backupFile)
	if err != nil {
		return err
	}

	return nil
}

// DeleteConfiguration removes sysctl configuration files from the /etc/sysctl.d directory that were
// copied from the templates.
// It returns a list of removed files.
func DeleteConfiguration() ([]string, error) {
	return templates.RemoveTemplateFiles(sysctlConfigSourceDir, sysctlConfigDestinationDir)
}

// FindSysctlConfigFiles returns a sorted list of sysctl configuration files in /etc/sysctl.d
func FindSysctlConfigFiles() ([]string, error) {
	// Reload sysctl settings
	dirEntries, err := os.ReadDir(sysctlConfigDestinationDir)
	if err != nil {
		return nil, err
	}

	var configFiles []string
	for _, entry := range dirEntries {
		if entry.IsDir() {
			continue
		}

		// only add config files ending with .conf
		if !strings.HasSuffix(entry.Name(), ".conf") {
			continue
		}

		configFiles = append(configFiles, path.Join(sysctlConfigDestinationDir, entry.Name()))
	}

	if len(configFiles) == 0 {
		return nil, nil
	}

	sort.Strings(configFiles)

	return configFiles, nil
}

// LoadConfigurationFrom reloads sysctl settings from the given configuration files.
// If the configFiles slice is empty, it will check for the existence of /etc/sysctl.conf
// and reload settings from there if it exists.
// It returns a list of configuration files that were used to reload sysctl settings.
func LoadConfigurationFrom(configFiles []string) ([]string, error) {
	var err error
	if len(configFiles) == 0 {
		// check that /etc/sysctl.conf exists
		if _, err = os.Stat(defaultSysctlConf); os.IsNotExist(err) {
			return configFiles, nil // nothing to reload
		}
	}

	// even if no config files are found, we still want to reload sysctl settings to apply any changes made to /etc/sysctl.conf
	// so we pass an empty slice to LoadConfigAndApply
	// if configFiles is empty, LoadConfigAndApply will use /etc/sysctl.conf
	// as per its implementation
	err = sysctl.LoadConfigAndApply(configFiles...)
	if err != nil {
		return configFiles, err
	}

	return configFiles, nil
}
