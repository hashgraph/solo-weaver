package software

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/internal/core"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
)

// TestScenario defines a test scenario with specific combinations of archives, binaries, and configs
type TestScenario struct {
	Name        string
	Archives    []ArchiveDetail
	Binaries    []BinaryDetail
	Configs     []ConfigDetail
	Files       map[string]string // Files to create in tar.gz archives
	Description string
}

// newTestInstallerWithScenario creates a test installer for a specific scenario
func newTestInstallerWithScenario(t *testing.T, scenario TestScenario) *baseInstaller {
	t.Helper()

	tempDir := t.TempDir()
	var tarGzChecksum string
	var filesChecksum map[string]string
	var err error

	// Create tar.gz if archives are defined
	if len(scenario.Archives) > 0 {
		tarGzPath := filepath.Join(tempDir, "test-artifact.tar.gz")
		tarGzChecksum, filesChecksum, err = createTestTarGz(tarGzPath, scenario.Files)
		require.NoError(t, err, "Failed to create test tar.gz")
	}

	// Setup mock HTTP server for downloads
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, ".tar.gz") && len(scenario.Archives) > 0:
			// Serve tar.gz archive
			tarGzPath := filepath.Join(tempDir, "test-artifact.tar.gz")
			fileContents, err := os.ReadFile(tarGzPath)
			require.NoError(t, err, "Failed to read test tar.gz")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(fileContents)
		case strings.Contains(r.URL.Path, "binary"):
			// Serve binary content
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("#!/bin/bash\necho 'test binary'\n"))
		case strings.Contains(r.URL.Path, "config"):
			// Serve config content
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("config_option=value\n"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	// Build archives with proper checksums
	archives := make([]ArchiveDetail, len(scenario.Archives))
	for i, archive := range scenario.Archives {
		archives[i] = ArchiveDetail{
			Name: archive.Name,
			URL:  server.URL + "/" + archive.Name,
			PlatformChecksum: PlatformChecksum{
				"test-os": {
					"test-arch": {
						Algorithm: "sha256",
						Value:     tarGzChecksum,
					},
				},
			},
		}
	}

	// Build binaries with proper checksums
	binaries := make([]BinaryDetail, len(scenario.Binaries))
	for i, binary := range scenario.Binaries {
		binaries[i] = BinaryDetail{
			Name:    binary.Name,
			URL:     binary.URL,
			Archive: binary.Archive,
		}

		// Set checksums based on binary source
		if binary.Archive != "" {
			// Binary from archive - use file checksum from tar.gz
			if filesChecksum != nil {
				binaries[i].PlatformChecksum = PlatformChecksum{
					"test-os": {
						"test-arch": {
							Algorithm: "sha256",
							Value:     filesChecksum[binary.Name],
						},
					},
				}
			}
		} else {
			// Binary from URL - calculate checksum of binary content
			binaryContent := "#!/bin/bash\necho 'test binary'\n"
			checksum := calculateSHA256([]byte(binaryContent))
			binaries[i].URL = server.URL + "/" + binary.Name
			binaries[i].PlatformChecksum = PlatformChecksum{
				"test-os": {
					"test-arch": {
						Algorithm: "sha256",
						Value:     checksum,
					},
				},
			}
		}
	}

	// Build configs with proper checksums
	configs := make([]ConfigDetail, len(scenario.Configs))
	for i, config := range scenario.Configs {
		configs[i] = ConfigDetail{
			Name:      config.Name,
			URL:       config.URL,
			Archive:   config.Archive,
			Algorithm: "sha256",
		}

		// Set checksums based on config source
		if config.Archive != "" {
			// Config from archive - use file checksum from tar.gz
			if filesChecksum != nil {
				configs[i].Value = filesChecksum[config.Name]
			}
		} else {
			// Config from URL - calculate checksum of config content
			configContent := "config_option=value\n"
			configs[i].URL = server.URL + "/" + config.Name
			configs[i].Value = calculateSHA256([]byte(configContent))
		}
	}

	fsxManager, err := fsx.NewManager()
	require.NoError(t, err)

	item := ArtifactMetadata{
		Name: "test-artifact",
		Versions: map[Version]VersionDetails{
			"1.0.0": {
				Archives: archives,
				Binaries: binaries,
				Configs:  configs,
			},
		},
	}

	return &baseInstaller{
		downloader:           NewDownloader(),
		software:             item.withPlatform("test-os", "test-arch"),
		fileManager:          fsxManager,
		versionToBeInstalled: "1.0.0",
	}
}

// calculateSHA256 calculates SHA256 checksum of given data
func calculateSHA256(data []byte) string {
	hash := sha256.New()
	hash.Write(data)
	return hex.EncodeToString(hash.Sum(nil))
}

// Test scenarios using table-driven tests
func Test_BaseInstaller_Scenarios(t *testing.T) {
	requireLinux(t)
	requireRoot(t)

	scenarios := []TestScenario{
		{
			Name:        "ArchivesOnly",
			Description: "Software distributed only as compressed archives",
			Archives: []ArchiveDetail{
				{Name: "test-artifact.tar.gz"},
			},
			Files: map[string]string{
				"README.md": "This is a test archive\n",
			},
		},
		{
			Name:        "BinariesOnlyByURL",
			Description: "Software distributed as direct binary downloads",
			Binaries: []BinaryDetail{
				{Name: "test-binary"},
			},
		},
		{
			Name:        "ConfigsOnlyByURL",
			Description: "Software distributed as direct config file downloads",
			Configs: []ConfigDetail{
				{Name: "test-config.conf"},
			},
		},
		{
			Name:        "ArchiveWithBinaries",
			Description: "Archive containing binary files",
			Archives: []ArchiveDetail{
				{Name: "test-artifact.tar.gz"},
			},
			Binaries: []BinaryDetail{
				{Name: "bin/test-binary", Archive: "test-artifact.tar.gz"},
			},
			Files: map[string]string{
				"bin/test-binary": "#!/bin/bash\necho 'test binary'\n",
			},
		},
		{
			Name:        "ArchiveWithConfigs",
			Description: "Archive containing configuration files",
			Archives: []ArchiveDetail{
				{Name: "test-artifact.tar.gz"},
			},
			Configs: []ConfigDetail{
				{Name: "config.conf", Archive: "test-artifact.tar.gz"},
			},
			Files: map[string]string{
				"config.conf": "config_option=value\n",
			},
		},
		{
			Name:        "ArchiveWithBinariesAndConfigs",
			Description: "Archive containing both binaries and config files",
			Archives: []ArchiveDetail{
				{Name: "test-artifact.tar.gz"},
			},
			Binaries: []BinaryDetail{
				{Name: "bin/test-binary", Archive: "test-artifact.tar.gz"},
			},
			Configs: []ConfigDetail{
				{Name: "config.conf", Archive: "test-artifact.tar.gz"},
			},
			Files: map[string]string{
				"bin/test-binary": "#!/bin/bash\necho 'test binary'\n",
				"config.conf":     "config_option=value\n",
			},
		},
		{
			Name:        "MultipleBinariesByURL",
			Description: "Multiple binaries downloaded directly",
			Binaries: []BinaryDetail{
				{Name: "binary1"},
				{Name: "binary2"},
			},
		},
		{
			Name:        "MultipleConfigs",
			Description: "Multiple config files downloaded directly",
			Configs: []ConfigDetail{
				{Name: "config1.conf"},
				{Name: "config2.conf"},
			},
		},
		{
			Name:        "MixedSources",
			Description: "Mixed scenario with archive, direct binary, and direct config",
			Archives: []ArchiveDetail{
				{Name: "test-artifact.tar.gz"},
			},
			Binaries: []BinaryDetail{
				{Name: "bin/archive-binary", Archive: "test-artifact.tar.gz"},
				{Name: "direct-binary"},
			},
			Configs: []ConfigDetail{
				{Name: "archive-config.conf", Archive: "test-artifact.tar.gz"},
				{Name: "direct-config.conf"},
			},
			Files: map[string]string{
				"bin/archive-binary":  "#!/bin/bash\necho 'archive binary'\n",
				"archive-config.conf": "archive_config=value\n",
			},
		},
		{
			Name:        "MultipleArchives",
			Description: "Multiple archives with different contents",
			Archives: []ArchiveDetail{
				{Name: "binaries.tar.gz"},
				{Name: "configs.tar.gz"},
			},
			Binaries: []BinaryDetail{
				{Name: "bin/binary1", Archive: "binaries.tar.gz"},
				{Name: "bin/binary2", Archive: "binaries.tar.gz"},
			},
			Configs: []ConfigDetail{
				{Name: "config1.conf", Archive: "configs.tar.gz"},
				{Name: "config2.conf", Archive: "configs.tar.gz"},
			},
			Files: map[string]string{
				"bin/binary1":  "#!/bin/bash\necho 'binary1'\n",
				"bin/binary2":  "#!/bin/bash\necho 'binary2'\n",
				"config1.conf": "config1=value\n",
				"config2.conf": "config2=value\n",
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			// Test Download
			t.Run("Download", func(t *testing.T) {
				resetTestEnvironment(t)
				installer := newTestInstallerWithScenario(t, scenario)
				err := installer.Download()
				require.NoError(t, err, "Download should succeed for scenario: %s", scenario.Description)

				// Verify downloaded files exist
				downloadFolder := installer.downloadFolder()

				// Check archives
				for _, archive := range scenario.Archives {
					archivePath := path.Join(downloadFolder, archive.Name)
					_, err := os.Stat(archivePath)
					require.NoError(t, err, "Archive %s should exist", archive.Name)
				}

				// Check directly downloaded binaries
				for _, binary := range scenario.Binaries {
					if binary.Archive == "" { // Only check direct downloads
						binaryPath := path.Join(downloadFolder, binary.Name)
						_, err := os.Stat(binaryPath)
						require.NoError(t, err, "Binary %s should exist", binary.Name)
					}
				}

				// Check directly downloaded configs
				for _, config := range scenario.Configs {
					if config.Archive == "" { // Only check direct downloads
						configPath := path.Join(downloadFolder, config.Name)
						_, err := os.Stat(configPath)
						require.NoError(t, err, "Config %s should exist", config.Name)
					}
				}
			})

			// Test Extract (only if archives exist)
			if len(scenario.Archives) > 0 {
				t.Run("Extract", func(t *testing.T) {
					resetTestEnvironment(t)
					installer := newTestInstallerWithScenario(t, scenario)

					// Download first
					err := installer.Download()
					require.NoError(t, err, "Download should succeed")

					// Then extract
					err = installer.Extract()
					require.NoError(t, err, "Extract should succeed for scenario: %s", scenario.Description)

					// Verify extracted files exist
					extractFolder := path.Join(installer.downloadFolder(), core.DefaultUnpackFolderName)

					// There should be files extracted from archives
					extractedFiles, err := os.ReadDir(extractFolder)
					require.NoError(t, err)
					require.Greater(t, len(extractedFiles), 0, "Extraction directory should contain files")

					// Check extracted binaries
					for _, binary := range scenario.Binaries {
						if binary.Archive != "" {
							binaryPath := path.Join(extractFolder, binary.Name)
							_, err := os.Stat(binaryPath)
							require.NoError(t, err, "Extracted binary %s should exist", binary.Name)
						}
					}

					// Check extracted configs
					for _, config := range scenario.Configs {
						if config.Archive != "" {
							configPath := path.Join(extractFolder, config.Name)
							_, err := os.Stat(configPath)
							require.NoError(t, err, "Extracted config %s should exist", config.Name)
						}
					}
				})
			}

			// Test Install (only if binaries exist)
			if len(scenario.Binaries) > 0 {
				t.Run("Install", func(t *testing.T) {
					resetTestEnvironment(t)
					installer := newTestInstallerWithScenario(t, scenario)

					// Download and extract first
					err := installer.Download()
					require.NoError(t, err, "Download should succeed")

					if len(scenario.Archives) > 0 {
						err = installer.Extract()
						require.NoError(t, err, "Extract should succeed")
					}

					// Then install
					err = installer.Install()
					require.NoError(t, err, "Install should succeed for scenario: %s", scenario.Description)

					// Verify installed binaries exist in sandbox
					sandboxBinDir := core.Paths().SandboxBinDir
					for _, binary := range scenario.Binaries {
						binaryBasename := path.Base(binary.Name)
						sandboxBinary := path.Join(sandboxBinDir, binaryBasename)
						_, err := os.Stat(sandboxBinary)
						require.NoError(t, err, "Installed binary %s should exist in sandbox", binaryBasename)
					}
				})
			}

			// Test IsInstalled
			t.Run("IsInstalled", func(t *testing.T) {
				resetTestEnvironment(t)
				installer := newTestInstallerWithScenario(t, scenario)

				// First check that nothing is installed initially
				isInstalled, err := installer.IsInstalled()
				require.NoError(t, err, "IsInstalled should not error initially")
				require.False(t, isInstalled, "IsInstalled should return false initially for scenario: %s", scenario.Description)

				// Download and extract first
				err = installer.Download()
				require.NoError(t, err, "Download should succeed")

				if len(scenario.Archives) > 0 {
					err = installer.Extract()
					require.NoError(t, err, "Extract should succeed")
				}

				// Install binaries if they exist
				if len(scenario.Binaries) > 0 {
					err = installer.Install()
					require.NoError(t, err, "Install should succeed")

					// Now verify installation
					isInstalled, err = installer.IsInstalled()
					require.NoError(t, err, "IsInstalled should not error after installation")
					require.True(t, isInstalled, "IsInstalled should return true after installation for scenario: %s", scenario.Description)
				}
			})

			// Test Configure and IsConfigured (only if binaries exist)
			if len(scenario.Binaries) > 0 {
				t.Run("Configure", func(t *testing.T) {
					resetTestEnvironment(t)
					installer := newTestInstallerWithScenario(t, scenario)

					// Download, extract, and install first
					err := installer.Download()
					require.NoError(t, err, "Download should succeed")

					if len(scenario.Archives) > 0 {
						err = installer.Extract()
						require.NoError(t, err, "Extract should succeed")
					}

					err = installer.Install()
					require.NoError(t, err, "Install should succeed")

					// Then configure
					err = installer.Configure()
					require.NoError(t, err, "Configure should succeed for scenario: %s", scenario.Description)

					// Verify symbolic links exist
					for _, binary := range scenario.Binaries {
						binaryBasename := path.Base(binary.Name)
						systemBinary := path.Join(core.SystemBinDir, binaryBasename)
						_, err := os.Lstat(systemBinary)
						require.NoError(t, err, "Symbolic link %s should exist", systemBinary)
					}
				})

				t.Run("IsConfigured", func(t *testing.T) {
					resetTestEnvironment(t)
					installer := newTestInstallerWithScenario(t, scenario)

					// First check that nothing is configured initially
					isConfigured, err := installer.IsConfigured()
					require.NoError(t, err, "IsConfigured should not error initially")
					require.False(t, isConfigured, "IsConfigured should return false initially for scenario: %s", scenario.Description)

					// Download, extract, and install first
					err = installer.Download()
					require.NoError(t, err, "Download should succeed")

					if len(scenario.Archives) > 0 {
						err = installer.Extract()
						require.NoError(t, err, "Extract should succeed")
					}

					err = installer.Install()
					require.NoError(t, err, "Install should succeed")

					// Should still not be configured before Configure() is called
					isConfigured, err = installer.IsConfigured()
					require.NoError(t, err, "IsConfigured should not error after installation")
					require.False(t, isConfigured, "IsConfigured should return false before Configure() for scenario: %s", scenario.Description)

					// Configure the software
					err = installer.Configure()
					require.NoError(t, err, "Configure should succeed")

					// Now verify configuration
					isConfigured, err = installer.IsConfigured()
					require.NoError(t, err, "IsConfigured should not error after configuration")
					require.True(t, isConfigured, "IsConfigured should return true after Configure() for scenario: %s", scenario.Description)
				})

				// Test Cleanup
				t.Run("Cleanup", func(t *testing.T) {
					resetTestEnvironment(t)
					installer := newTestInstallerWithScenario(t, scenario)

					// Download and extract first
					err := installer.Download()
					require.NoError(t, err, "Download should succeed")

					if len(scenario.Archives) > 0 {
						err = installer.Extract()
						require.NoError(t, err, "Extract should succeed")
					}

					// Install binaries if they exist
					if len(scenario.Binaries) > 0 {
						err = installer.Install()
						require.NoError(t, err, "Install should succeed")

						// Now verify installation
						isInstalled, err := installer.IsInstalled()
						require.NoError(t, err, "IsInstalled should not error after installation")
						require.True(t, isInstalled, "IsInstalled should return true after installation for scenario: %s", scenario.Description)
					}

					// Then cleanup
					err = installer.Cleanup()
					require.NoError(t, err, "Cleanup should succeed for scenario: %s", scenario.Description)

					// Verify files under download folder were removed
					downloadFolder := installer.downloadFolder()
					_, err = os.Stat(downloadFolder)
					require.True(t, os.IsNotExist(err), "Download folder should be removed after installation")
				})

			}
		})
	}
}

// Legacy tests - keeping existing test structure for compatibility
func newTestInstaller(t *testing.T) *baseInstaller {
	t.Helper()

	// TempDir auto-cleans at the end of the test
	tempDir := t.TempDir()

	// Build the tar.gz used by the server
	tarGzPath := filepath.Join(tempDir, "test-artifact.tar.gz")
	checksum, filesChecksum, err := createTestTarGz(tarGzPath, nil)
	require.NoError(t, err, "Failed to create test tar.gz")

	fileContents, err := os.ReadFile(tarGzPath)
	require.NoError(t, err, "Failed to read test tar.gz")

	// Keep server alive for the duration of the test
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fileContents)
	}))
	t.Cleanup(server.Close)

	fsxManager, err := fsx.NewManager()
	require.NoError(t, err)
	item := ArtifactMetadata{
		Name: "test-artifact",
		Versions: map[Version]VersionDetails{
			"1.0.0": {
				Archives: []ArchiveDetail{
					{
						Name: "test-artifact.tar.gz",
						URL:  server.URL + "/test-artifact.tar.gz",
						PlatformChecksum: PlatformChecksum{
							"test-os": {
								"test-arch": {
									Algorithm: "sha256",
									Value:     checksum,
								},
							},
						},
					},
				},
				Binaries: []BinaryDetail{
					{
						Name:    "bin/test-binary",
						Archive: "test-artifact.tar.gz",
						PlatformChecksum: PlatformChecksum{
							"test-os": {
								"test-arch": {
									Algorithm: "sha256",
									Value:     filesChecksum["bin/test-binary"],
								},
							},
						},
					},
				},
				Configs: []ConfigDetail{
					{
						Name:      "config.conf",
						Archive:   "test-artifact.tar.gz",
						Algorithm: "sha256",
						Value:     filesChecksum["config.conf"],
					},
				},
			},
		},
	}

	return &baseInstaller{
		downloader:           NewDownloader(),
		software:             item.withPlatform("test-os", "test-arch"),
		fileManager:          fsxManager,
		versionToBeInstalled: "1.0.0",
	}
}

// Test successful download
func Test_BaseInstaller_Download_Success(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//
	installer := newTestInstaller(t)

	//
	// When
	//
	err := installer.Download()

	//
	// Then
	//
	require.NoError(t, err, "Failed to download test-artifact")
}

// Test when permission to create file is denied
func Test_BaseInstaller_Download_PermissionError(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//
	installer := newTestInstaller(t)

	// Create a regular file where the directory should be created
	// This will cause MkdirAll to fail with permission/file exists error
	conflictingFile := tmpFolder
	err := os.MkdirAll("/opt/provisioner", core.DefaultDirOrExecPerm)
	require.NoError(t, err, "Failed to create /opt/provisioner directory")
	err = os.WriteFile(conflictingFile, []byte("blocking file"), core.DefaultFilePerm)
	require.NoError(t, err, "Failed to create blocking file")

	// Override cleanup to remove the file we created
	t.Cleanup(func() {
		_ = os.Remove(conflictingFile)
	})

	//
	// When
	//
	err = installer.Download()

	//
	// Then
	//
	require.Error(t, err, "Download should fail due to permission error")
	require.True(t, errorx.IsOfType(err, DownloadError), "Error should be of type DownloadError")
}

// Test when download fails due to invalid configuration
func Test_BaseInstaller_Download_Fails(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//
	installer := newTestInstaller(t)
	installer.versionToBeInstalled = "invalidversion" // Set to a version that doesn't exist

	//
	// When
	//
	err := installer.Download()

	//
	// Then
	//
	require.Error(t, err, "Download should fail")
	require.True(t, errorx.IsOfType(err, VersionNotFoundError), "Error should be of type VersionNotFoundError")
}

// Test when checksum fails (this test might need adjustment based on actual config)
func Test_BaseInstaller_Download_ChecksumFails(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	installer := newTestInstaller(t)

	// Corrupt the checksum - update the first binary's checksum for the test platform
	vd := installer.software.Versions["1.0.0"]
	require.Len(t, vd.Archives, 1, "There should be at least one archive in version details")

	// Get the first binary and update its checksum
	archive := vd.Archives[0]
	if platformChecksums, ok := archive.PlatformChecksum["test-os"]; ok {
		if checksum, ok := platformChecksums["test-arch"]; ok {
			checksum.Value = "invalidchecksum"
			platformChecksums["test-arch"] = checksum
			archive.PlatformChecksum["test-os"] = platformChecksums
			vd.Archives[0] = archive
			installer.software.Versions["1.0.0"] = vd
		}
	}

	//
	// When
	//
	err := installer.Download()

	//
	// Then
	//
	require.Error(t, err, "Download should fail")
	require.True(t, errorx.IsOfType(err, ChecksumError), "Error should be of type ChecksumError")

}

// Test idempotency with existing valid file
func Test_BaseInstaller_Download_Idempotency_ExistingFile(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//
	installer := newTestInstaller(t)

	err := installer.Download()
	require.NoError(t, err, "Failed to download test-artifact")

	//
	// When
	//

	// Trigger Download again
	err = installer.Download()

	//
	// Then
	//
	require.NoError(t, err, "Download should succeed again due to idempotency")
}

func Test_BaseInstaller_Download_Idempotency_ExistingFile_WrongChecksum(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//
	installer := newTestInstaller(t)

	// create empty file to emulate first download with wrong checksum
	err := os.MkdirAll(installer.downloadFolder(), core.DefaultDirOrExecPerm)
	require.NoError(t, err, "Failed to create download folder")

	destinationFile := path.Join(installer.downloadFolder(), "test-artifact.tar.gz")

	err = os.WriteFile(destinationFile, []byte(""), core.DefaultDirOrExecPerm)
	require.NoError(t, err, "Failed to create empty file")

	//
	// When
	//
	err = installer.Download()

	//
	// Then
	//
	require.NoError(t, err, "Download should succeed again due to idempotency")
	// Check that the file exists
	_, err = os.Stat(destinationFile)
	require.NoError(t, err, "File should exist after download")
}

func Test_BaseInstaller_Extract_Success(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//
	installer := newTestInstaller(t)

	//
	// When
	//
	err := installer.Download()
	require.NoError(t, err, "Failed to download test-artifact")

	err = installer.Extract()

	//
	// Then
	//
	require.NoError(t, err, "Failed to extract test-artifact")
	// validate there are multiple files under extracted folder
	extractedFolder := tmpFolder + "/test-artifact/unpack"
	files, err := os.ReadDir(extractedFolder)
	require.NoError(t, err, "Failed to read extracted folder")
	require.Greater(t, len(files), 0, "No files found in extracted folder")
}

func Test_BaseInstaller_Extract_Error(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//

	// Create a regular file where the directory should be created
	conflictingFile := tmpFolder + "/test-artifact/unpack"
	err := os.MkdirAll(tmpFolder+"/test-artifact", core.DefaultDirOrExecPerm)
	require.NoError(t, err, "Failed to create cri-o directory")
	err = os.WriteFile(conflictingFile, []byte("blocking file"), core.DefaultFilePerm)
	require.NoError(t, err, "Failed to create blocking file")

	// Override cleanup to remove the file we created
	t.Cleanup(func() {
		_ = os.Remove(conflictingFile)
	})

	//
	// When
	//
	installer := newTestInstaller(t)

	err = installer.Download()
	require.NoError(t, err, "Failed to download test-artifact")

	err = installer.Extract()

	//
	// Then
	//
	require.Error(t, err, "Extract should fail due to permission error")
	require.True(t, errorx.IsOfType(err, ExtractionError), "Error should be of type ExtractError")
}

func Test_BaseInstaller_replaceAllInFile(t *testing.T) {
	//
	// Given
	//
	fsxManager, err := fsx.NewManager()
	require.NoError(t, err)

	ki := kubeadmInstaller{
		baseInstaller: &baseInstaller{
			fileManager: fsxManager,
		},
	}

	// Create a temp dir and file
	tmpDir := t.TempDir()
	origPath := filepath.Join(tmpDir, "10-kubeadm.conf")
	origContent := "ExecStart=/usr/bin/kubelet $KUBELET_KUBEADM_ARGS\n"
	err = os.WriteFile(origPath, []byte(origContent), core.DefaultFilePerm)
	require.NoError(t, err, "failed to write temp file")

	//
	// When
	//
	newKubeletPath := "/custom/bin/kubelet"
	err = ki.replaceAllInFile(origPath, "/usr/bin/kubelet", newKubeletPath)
	require.NoError(t, err, "failed to replace kubelet path in file")

	//
	// Then
	//
	// Read back and check
	updated, err := os.ReadFile(origPath)
	require.NoError(t, err, "failed to read updated file")

	require.Contains(t, string(updated), newKubeletPath, "updated file should contain new kubelet path")
	require.NotContains(t, string(updated), "/usr/bin/kubelet", "old kubelet path should not be present in file")
}

func Test_BaseInstaller_Uninstall_Success(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//

	// Create a test installer with actual file manager
	fsxManager, err := fsx.NewManager()
	require.NoError(t, err)

	software := &ArtifactMetadata{
		Name: "test-software",
		Versions: map[Version]VersionDetails{
			"1.0.0": {
				Binaries: []BinaryDetail{
					{Name: "test-binary"},
				},
			},
		},
	}

	installer := &baseInstaller{
		software:             software,
		versionToBeInstalled: "1.0.0",
		fileManager:          fsxManager,
	}

	// Create sandbox directory structure
	sandboxBinDir := core.Paths().SandboxBinDir
	err = os.MkdirAll(sandboxBinDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	sandboxBinary := path.Join(sandboxBinDir, "test-binary")
	err = os.WriteFile(sandboxBinary, []byte("#!/bin/bash\necho 'test'\n"), core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	// Create symlink in system bin directory
	systemBinary := path.Join(core.SystemBinDir, "test-binary")
	err = os.Symlink(sandboxBinary, systemBinary)
	require.NoError(t, err)

	// Verify everything exists before uninstall
	_, err = os.Lstat(systemBinary)
	require.NoError(t, err, "Symlink should exist before uninstall")
	_, err = os.Stat(sandboxBinary)
	require.NoError(t, err, "Sandbox binary should exist before uninstall")

	//
	// When
	//

	// Execute uninstall
	err = installer.Uninstall()
	require.NoError(t, err)

	//
	// Then
	//

	// Verify only sandbox binary is removed, symlink should remain
	_, err = os.Lstat(systemBinary)
	require.NoError(t, err, "Symlink should still exist after uninstall (not removed by Uninstall)")
	_, err = os.Stat(sandboxBinary)
	require.True(t, os.IsNotExist(err), "Sandbox binary should be removed after uninstall")

	// Clean up the symlink we created
	t.Cleanup(func() {
		_ = os.Remove(systemBinary)
	})
}

func Test_BaseInstaller_Uninstall_PermissionError(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//

	// Create a test installer with actual file manager
	fsxManager, err := fsx.NewManager()
	require.NoError(t, err)

	software := &ArtifactMetadata{
		Name: "test-software",
		Versions: map[Version]VersionDetails{
			"1.0.0": {
				Binaries: []BinaryDetail{
					{Name: "test-binary"},
				},
			},
		},
	}

	installer := &baseInstaller{
		software:             software,
		versionToBeInstalled: "1.0.0",
		fileManager:          fsxManager,
	}

	// Create sandbox directory structure
	sandboxBinDir := core.Paths().SandboxBinDir
	err = os.MkdirAll(sandboxBinDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	sandboxBinary := path.Join(sandboxBinDir, "test-binary")
	err = os.WriteFile(sandboxBinary, []byte("#!/bin/bash\necho 'test'\n"), core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	// Make the sandbox bin directory read-only
	cmd := exec.Command("chattr", "+i", sandboxBinDir)
	err = cmd.Run()
	require.NoError(t, err, "Failed to make sandbox bin directory read-only")

	// Restore permissions after test
	t.Cleanup(func() {
		_ = exec.Command("chattr", "-i", sandboxBinDir).Run()
		_ = os.RemoveAll(sandboxBinary)
	})

	//
	// When
	//

	// Execute uninstall - should fail due to read-only directory
	err = installer.Uninstall()

	//
	// Then
	//
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, InstallationError))

}

func Test_BaseInstaller_Uninstall_NoDownloadFolder(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//

	// Create a test installer with actual file manager
	fsxManager, err := fsx.NewManager()
	require.NoError(t, err)

	software := &ArtifactMetadata{
		Name: "test-software",
		Versions: map[Version]VersionDetails{
			"1.0.0": {
				Binaries: []BinaryDetail{
					{Name: "test-binary"},
				},
			},
		},
	}

	installer := &baseInstaller{
		software:             software,
		versionToBeInstalled: "1.0.0",
		fileManager:          fsxManager,
	}

	// Create sandbox bin directory with a binary
	sandboxBinDir := core.Paths().SandboxBinDir
	err = os.MkdirAll(sandboxBinDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	sandboxBinary := path.Join(sandboxBinDir, "test-binary")
	err = os.WriteFile(sandboxBinary, []byte("#!/bin/bash\necho 'test'\n"), core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	// Don't create download folder - it shouldn't exist

	// Verify sandbox binary exists but download folder doesn't
	_, err = os.Stat(sandboxBinary)
	require.NoError(t, err, "Sandbox binary should exist")

	downloadFolder := path.Join(core.Paths().TempDir, "test-software")
	_, err = os.Stat(downloadFolder)
	require.True(t, os.IsNotExist(err), "Download folder should not exist")

	//
	// When
	//

	// Execute uninstall - should succeed even without download folder
	err = installer.Uninstall()

	//
	// Then
	//
	require.NoError(t, err)

	// Verify sandbox binary is cleaned up
	_, err = os.Stat(sandboxBinary)
	require.True(t, os.IsNotExist(err), "Sandbox binary should be removed after uninstall")
}

func Test_BaseInstaller_Uninstall_VersionNotFound(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//

	// Create a test installer with actual file manager
	fsxManager, err := fsx.NewManager()
	require.NoError(t, err)

	software := &ArtifactMetadata{
		Name: "test-software",
		Versions: map[Version]VersionDetails{
			"1.0.0": {
				Binaries: []BinaryDetail{
					{Name: "test-binary"},
				},
			},
		},
	}

	installer := &baseInstaller{
		software:             software,
		versionToBeInstalled: "2.0.0", // Version not in metadata
		fileManager:          fsxManager,
	}

	//
	// When
	//

	// Execute uninstall with non-existent version
	err = installer.Uninstall()

	//
	// Then
	//
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, InstallationError))
}

func Test_BaseInstaller_Uninstall_MultipleBinaries(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//

	// Create a test installer with actual file manager
	fsxManager, err := fsx.NewManager()
	require.NoError(t, err)

	software := &ArtifactMetadata{
		Name: "test-software",
		Versions: map[Version]VersionDetails{
			"1.0.0": {
				Binaries: []BinaryDetail{
					{Name: "binary1"},
					{Name: "binary2"},
					{Name: "subdir/binary3"},
				},
			},
		},
	}

	installer := &baseInstaller{
		software:             software,
		versionToBeInstalled: "1.0.0",
		fileManager:          fsxManager,
	}

	// Create sandbox directory structure
	sandboxBinDir := core.Paths().SandboxBinDir
	err = os.MkdirAll(sandboxBinDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	// Create binaries in sandbox
	sandboxBinary1 := path.Join(sandboxBinDir, "binary1")
	err = os.WriteFile(sandboxBinary1, []byte("#!/bin/bash\necho 'binary1'\n"), core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	sandboxBinary2 := path.Join(sandboxBinDir, "binary2")
	err = os.WriteFile(sandboxBinary2, []byte("#!/bin/bash\necho 'binary2'\n"), core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	sandboxBinary3 := path.Join(sandboxBinDir, "binary3")
	err = os.WriteFile(sandboxBinary3, []byte("#!/bin/bash\necho 'binary3'\n"), core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	// Create symlinks for all binaries
	systemBinary1 := path.Join(core.SystemBinDir, "binary1")
	err = os.Symlink(sandboxBinary1, systemBinary1)
	require.NoError(t, err)

	systemBinary2 := path.Join(core.SystemBinDir, "binary2")
	err = os.Symlink(sandboxBinary2, systemBinary2)
	require.NoError(t, err)

	systemBinary3 := path.Join(core.SystemBinDir, "binary3")
	err = os.Symlink(sandboxBinary3, systemBinary3)
	require.NoError(t, err)

	//
	// When
	//

	// Execute uninstall
	err = installer.Uninstall()

	//
	// Then
	//
	require.NoError(t, err)

	// Verify symlinks still exist (not removed by Uninstall)
	_, err = os.Lstat(systemBinary1)
	require.NoError(t, err, "Symlink for binary1 should still exist after uninstall")
	_, err = os.Lstat(systemBinary2)
	require.NoError(t, err, "Symlink for binary2 should still exist after uninstall")
	_, err = os.Lstat(systemBinary3)
	require.NoError(t, err, "Symlink for binary3 should still exist after uninstall")

	// Verify all sandbox binaries are removed
	_, err = os.Stat(sandboxBinary1)
	require.True(t, os.IsNotExist(err), "Sandbox binary1 should be removed")
	_, err = os.Stat(sandboxBinary2)
	require.True(t, os.IsNotExist(err), "Sandbox binary2 should be removed")
	_, err = os.Stat(sandboxBinary3)
	require.True(t, os.IsNotExist(err), "Sandbox binary3 should be removed")

	// Clean up the symlinks we created
	t.Cleanup(func() {
		_ = os.Remove(systemBinary1)
		_ = os.Remove(systemBinary2)
		_ = os.Remove(systemBinary3)
	})
}

func Test_BaseInstaller_Uninstall_SymlinkPointsToOurBinary(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//

	// Create a test installer with actual file manager
	fsxManager, err := fsx.NewManager()
	require.NoError(t, err)

	software := &ArtifactMetadata{
		Name: "test-software",
		Versions: map[Version]VersionDetails{
			"1.0.0": {
				Binaries: []BinaryDetail{
					{Name: "test-binary"},
				},
			},
		},
	}

	installer := &baseInstaller{
		software:             software,
		versionToBeInstalled: "1.0.0",
		fileManager:          fsxManager,
	}

	// Create sandbox directory structure
	sandboxBinDir := core.Paths().SandboxBinDir
	err = os.MkdirAll(sandboxBinDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	sandboxBinary := path.Join(sandboxBinDir, "test-binary")
	err = os.WriteFile(sandboxBinary, []byte("#!/bin/bash\necho 'test'\n"), core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	// Create symlink that points to our sandbox binary
	systemBinary := path.Join(core.SystemBinDir, "test-binary")
	err = os.Symlink(sandboxBinary, systemBinary)
	require.NoError(t, err)

	// Verify symlink points to our binary
	linkTarget, err := os.Readlink(systemBinary)
	require.NoError(t, err)
	require.Equal(t, sandboxBinary, linkTarget)

	//
	// When
	//

	// Execute uninstall
	err = installer.Uninstall()

	//
	// Then
	//
	require.NoError(t, err)

	// Verify symlink still exists (not removed by Uninstall)
	_, err = os.Lstat(systemBinary)
	require.NoError(t, err, "Symlink should still exist after uninstall (not removed by Uninstall)")

	// Verify sandbox binary is removed
	_, err = os.Stat(sandboxBinary)
	require.True(t, os.IsNotExist(err), "Sandbox binary should be removed after uninstall")

	// Clean up the symlink we created
	t.Cleanup(func() {
		_ = os.Remove(systemBinary)
	})
}

// Tests for RestoreConfiguration method
func Test_BaseInstaller_RestoreConfiguration_Success(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//

	// Create a test installer with actual file manager
	fsxManager, err := fsx.NewManager()
	require.NoError(t, err)

	software := &ArtifactMetadata{
		Name: "test-software",
		Versions: map[Version]VersionDetails{
			"1.0.0": {
				Binaries: []BinaryDetail{
					{Name: "test-binary"},
				},
			},
		},
	}

	installer := &baseInstaller{
		software:             software,
		versionToBeInstalled: "1.0.0",
		fileManager:          fsxManager,
	}

	// Create sandbox directory structure
	sandboxBinDir := core.Paths().SandboxBinDir
	err = os.MkdirAll(sandboxBinDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	sandboxBinary := path.Join(sandboxBinDir, "test-binary")
	err = os.WriteFile(sandboxBinary, []byte("#!/bin/bash\necho 'test'\n"), core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	// Create symlink in system bin directory that points to our sandbox binary
	systemBinary := path.Join(core.SystemBinDir, "test-binary")
	err = os.Symlink(sandboxBinary, systemBinary)
	require.NoError(t, err)

	// Verify symlink exists and points to our binary before RestoreConfiguration
	_, err = os.Lstat(systemBinary)
	require.NoError(t, err, "Symlink should exist before RestoreConfiguration")
	linkTarget, err := os.Readlink(systemBinary)
	require.NoError(t, err)
	require.Equal(t, sandboxBinary, linkTarget)

	//
	// When
	//

	// Execute RestoreConfiguration
	err = installer.RestoreConfiguration()
	require.NoError(t, err)

	//
	// Then
	//

	// Verify symlink is removed
	_, err = os.Lstat(systemBinary)
	require.True(t, os.IsNotExist(err), "Symlink should be removed after RestoreConfiguration")

	// Verify sandbox binary still exists (RestoreConfiguration doesn't remove it)
	_, err = os.Stat(sandboxBinary)
	require.NoError(t, err, "Sandbox binary should still exist after RestoreConfiguration")
}

func Test_BaseInstaller_RestoreConfiguration_SymlinkPointsToOtherBinary(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//

	// Create a test installer with actual file manager
	fsxManager, err := fsx.NewManager()
	require.NoError(t, err)

	software := &ArtifactMetadata{
		Name: "test-software",
		Versions: map[Version]VersionDetails{
			"1.0.0": {
				Binaries: []BinaryDetail{
					{Name: "test-binary"},
				},
			},
		},
	}

	installer := &baseInstaller{
		software:             software,
		versionToBeInstalled: "1.0.0",
		fileManager:          fsxManager,
	}

	// Create sandbox directory structure
	sandboxBinDir := core.Paths().SandboxBinDir
	err = os.MkdirAll(sandboxBinDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	sandboxBinary := path.Join(sandboxBinDir, "test-binary")
	err = os.WriteFile(sandboxBinary, []byte("#!/bin/bash\necho 'test'\n"), core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	// Create a different binary (not in our sandbox)
	otherBinary := path.Join(t.TempDir(), "other-binary")
	err = os.WriteFile(otherBinary, []byte("#!/bin/bash\necho 'other'\n"), core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	// Create symlink that points to a different binary (not ours)
	systemBinary := path.Join(core.SystemBinDir, "test-binary")
	err = os.Symlink(otherBinary, systemBinary)
	require.NoError(t, err)

	// Verify symlink points to the other binary
	linkTarget, err := os.Readlink(systemBinary)
	require.NoError(t, err)
	require.Equal(t, otherBinary, linkTarget)

	//
	// When
	//

	// Execute RestoreConfiguration
	err = installer.RestoreConfiguration()

	//
	// Then
	//
	require.NoError(t, err)

	// Verify symlink is NOT removed (because it doesn't point to our binary)
	_, err = os.Lstat(systemBinary)
	require.NoError(t, err, "Symlink should NOT be removed because it points to another binary")

	// Verify it still points to the other binary
	linkTarget, err = os.Readlink(systemBinary)
	require.NoError(t, err)
	require.Equal(t, otherBinary, linkTarget)

	// Clean up the symlink we created
	t.Cleanup(func() {
		_ = os.Remove(systemBinary)
	})
}

func Test_BaseInstaller_RestoreConfiguration_MultipleBinaries(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//

	// Create a test installer with actual file manager
	fsxManager, err := fsx.NewManager()
	require.NoError(t, err)

	software := &ArtifactMetadata{
		Name: "test-software",
		Versions: map[Version]VersionDetails{
			"1.0.0": {
				Binaries: []BinaryDetail{
					{Name: "binary1"},
					{Name: "binary2"},
					{Name: "subdir/binary3"},
				},
			},
		},
	}

	installer := &baseInstaller{
		software:             software,
		versionToBeInstalled: "1.0.0",
		fileManager:          fsxManager,
	}

	// Create sandbox directory structure
	sandboxBinDir := core.Paths().SandboxBinDir
	err = os.MkdirAll(sandboxBinDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	// Create binaries in sandbox
	sandboxBinary1 := path.Join(sandboxBinDir, "binary1")
	err = os.WriteFile(sandboxBinary1, []byte("#!/bin/bash\necho 'binary1'\n"), core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	sandboxBinary2 := path.Join(sandboxBinDir, "binary2")
	err = os.WriteFile(sandboxBinary2, []byte("#!/bin/bash\necho 'binary2'\n"), core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	sandboxBinary3 := path.Join(sandboxBinDir, "binary3")
	err = os.WriteFile(sandboxBinary3, []byte("#!/bin/bash\necho 'binary3'\n"), core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	// Create symlinks for all binaries that point to our sandbox binaries
	systemBinary1 := path.Join(core.SystemBinDir, "binary1")
	err = os.Symlink(sandboxBinary1, systemBinary1)
	require.NoError(t, err)

	systemBinary2 := path.Join(core.SystemBinDir, "binary2")
	err = os.Symlink(sandboxBinary2, systemBinary2)
	require.NoError(t, err)

	systemBinary3 := path.Join(core.SystemBinDir, "binary3")
	err = os.Symlink(sandboxBinary3, systemBinary3)
	require.NoError(t, err)

	//
	// When
	//

	// Execute RestoreConfiguration
	err = installer.RestoreConfiguration()

	//
	// Then
	//
	require.NoError(t, err)

	// Verify all symlinks are removed
	_, err = os.Lstat(systemBinary1)
	require.True(t, os.IsNotExist(err), "Symlink for binary1 should be removed")
	_, err = os.Lstat(systemBinary2)
	require.True(t, os.IsNotExist(err), "Symlink for binary2 should be removed")
	_, err = os.Lstat(systemBinary3)
	require.True(t, os.IsNotExist(err), "Symlink for binary3 should be removed")

	// Verify all sandbox binaries still exist
	_, err = os.Stat(sandboxBinary1)
	require.NoError(t, err, "Sandbox binary1 should still exist")
	_, err = os.Stat(sandboxBinary2)
	require.NoError(t, err, "Sandbox binary2 should still exist")
	_, err = os.Stat(sandboxBinary3)
	require.NoError(t, err, "Sandbox binary3 should still exist")
}

func Test_BaseInstaller_RestoreConfiguration_SymlinkError(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//

	// Create a test installer with actual file manager
	fsxManager, err := fsx.NewManager()
	require.NoError(t, err)

	software := &ArtifactMetadata{
		Name: "test-software",
		Versions: map[Version]VersionDetails{
			"1.0.0": {
				Binaries: []BinaryDetail{
					{Name: "test-binary"},
				},
			},
		},
	}

	installer := &baseInstaller{
		software:             software,
		versionToBeInstalled: "1.0.0",
		fileManager:          fsxManager,
	}

	// Create sandbox directory structure
	sandboxBinDir := core.Paths().SandboxBinDir
	err = os.MkdirAll(sandboxBinDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	sandboxBinary := path.Join(sandboxBinDir, "test-binary")
	err = os.WriteFile(sandboxBinary, []byte("#!/bin/bash\necho 'test'\n"), core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	// Create symlink in system bin directory
	systemBinary := path.Join(core.SystemBinDir, "test-binary")
	err = os.Symlink(sandboxBinary, systemBinary)
	require.NoError(t, err)

	// Remove write permissions from system bin directory
	cmd := exec.Command("chattr", "+i", core.SystemBinDir)
	err = cmd.Run()
	require.NoError(t, err, "Failed to make system bin directory read-only")

	// Restore permissions after test
	t.Cleanup(func() {
		_ = exec.Command("chattr", "-i", core.SystemBinDir).Run()
		_ = os.Remove(systemBinary)
	})

	//
	// When
	//

	// Execute RestoreConfiguration - should fail due to read-only system bin directory
	err = installer.RestoreConfiguration()

	//
	// Then
	//
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, InstallationError))
}

func Test_BaseInstaller_RestoreConfiguration_VersionNotFound(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//

	// Create a test installer with actual file manager
	fsxManager, err := fsx.NewManager()
	require.NoError(t, err)

	software := &ArtifactMetadata{
		Name: "test-software",
		Versions: map[Version]VersionDetails{
			"1.0.0": {
				Binaries: []BinaryDetail{
					{Name: "test-binary"},
				},
			},
		},
	}

	installer := &baseInstaller{
		software:             software,
		versionToBeInstalled: "2.0.0", // Version not in metadata
		fileManager:          fsxManager,
	}

	//
	// When
	//

	// Execute RestoreConfiguration with non-existent version
	err = installer.RestoreConfiguration()

	//
	// Then
	//
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, InstallationError))
}

func Test_BaseInstaller_RestoreConfiguration_NoSymlinks(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//

	// Create a test installer with actual file manager
	fsxManager, err := fsx.NewManager()
	require.NoError(t, err)

	software := &ArtifactMetadata{
		Name: "test-software",
		Versions: map[Version]VersionDetails{
			"1.0.0": {
				Binaries: []BinaryDetail{
					{Name: "test-binary"},
				},
			},
		},
	}

	installer := &baseInstaller{
		software:             software,
		versionToBeInstalled: "1.0.0",
		fileManager:          fsxManager,
	}

	// Create sandbox directory structure but no symlinks
	sandboxBinDir := core.Paths().SandboxBinDir
	err = os.MkdirAll(sandboxBinDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	sandboxBinary := path.Join(sandboxBinDir, "test-binary")
	err = os.WriteFile(sandboxBinary, []byte("#!/bin/bash\necho 'test'\n"), core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	// Don't create any symlinks

	//
	// When
	//

	// Execute RestoreConfiguration - should succeed even with no symlinks
	err = installer.RestoreConfiguration()

	//
	// Then
	//
	require.NoError(t, err)

	// Verify sandbox binary still exists
	_, err = os.Stat(sandboxBinary)
	require.NoError(t, err, "Sandbox binary should still exist")
}
