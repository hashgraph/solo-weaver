//go:build integration

package software

import (
	"bytes"
	"os"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
)

func Test_CrioInstaller_FullWorkflow_Success(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//
	installer, err := NewCrioInstaller()
	require.NoError(t, err, "Failed to create cri-o installer")

	fileManager, err := fsx.NewManager()
	require.NoError(t, err)

	//
	// When - Download
	//
	err = installer.Download()
	require.NoError(t, err, "Failed to download cri-o and/or its configuration")

	// Verify downloaded files exist
	files, err := os.ReadDir("/opt/provisioner/tmp/cri-o")
	require.NoError(t, err)

	require.Equal(t, 1, len(files), "There should be exactly one file in the download directory")
	// Check that the downloaded file has the expected name format by regex
	require.Regexp(t,
		regexp.MustCompile(`^cri-o\.[^-]+\.v[0-9]+\.[0-9]+\.[0-9]+\.tar\.gz$`),
		files[0].Name(),
		"Downloaded file name should match expected pattern",
	)

	//
	// When - Extract
	//
	err = installer.Extract()
	require.NoError(t, err, "Failed to extract cri-o")

	// Verify extraction directory exists and contains expected files
	extractedFiles, err := os.ReadDir("/opt/provisioner/tmp/cri-o/unpack")
	require.NoError(t, err)
	require.Greater(t, len(extractedFiles), 0, "Extraction directory should contain files")

	//
	// When - Install
	//
	//
	err = installer.Install()
	require.NoError(t, err, "Failed to install cri-o")

	// Verify 10-crio.conf is copied to /opt/provisioner/sandbox/etc/crio/crio.conf.d/10-crio.conf
	contents, err := os.ReadFile("/opt/provisioner/sandbox/etc/crio/crio.conf.d/10-crio.conf")
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`/usr/libexec/crio`), string(contents),
		"10-crio.conf should reference the correct libexec path")
	require.Regexp(t, regexp.MustCompile(`/etc/crio`), string(contents),
		"10-crio.conf should reference the correct etc crio path")

	//
	// When - Configure
	//
	err = installer.Configure()
	require.NoError(t, err, "Failed to configure cri-o")

	// Verify Symlink CRI-O /etc/containers/registries.conf.d
	linkTarget, err := os.Readlink("/etc/containers")
	require.NoError(t, err)
	require.Equal(t, "/opt/provisioner/sandbox/etc/containers", linkTarget, "containers etc symlink should point to sandbox etc directory")

	// Verify CRI-O service file is using the sandbox bin directory
	contents, err = os.ReadFile("/opt/provisioner/sandbox/usr/lib/systemd/system/crio.service")
	require.NoError(t, err)

	// Verify CRI-O service file contents from latest
	contents, err = os.ReadFile("/opt/provisioner/sandbox/usr/lib/systemd/system/crio.service.latest")
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`/opt/provisioner/sandbox/usr/local/bin/crio`), string(contents),
		"crio service file should reference the correct bin path")
	require.Regexp(t, regexp.MustCompile(`/opt/provisioner/sandbox/etc/default/crio`), string(contents),
		"crio service file should reference the correct default crio path")

	// Verify Symlink CRI-O /usr/lib/systemd/system/crio.service
	linkTarget, err = os.Readlink("/usr/lib/systemd/system/crio.service")
	require.NoError(t, err)
	require.Equal(t, "/opt/provisioner/sandbox/usr/lib/systemd/system/crio.service.latest", linkTarget, "crio service symlink should point to sandbox service directory")

	// Verify /opt/provisioner/sandbox/etc/default/crio
	contents, err = os.ReadFile("/opt/provisioner/sandbox/etc/default/crio")
	require.NoError(t, err)
	// there should be no occurrences of `sysconfig/crio` that file
	require.NotRegexp(t, regexp.MustCompile(`sysconfig/crio`), string(contents),
		"crio configuration should not reference sysconfig/crio for debian based distributions")

	// there should be occurrences of default/crio instead
	require.Regexp(t, regexp.MustCompile(`default/crio`), string(contents),
		"crio configuration should reference default/crio for debian based distributions")

	// check CRI-O configuration directory
	require.Regexp(t, regexp.MustCompile(`/opt/provisioner/sandbox/etc/crio/crio.conf.d`), string(contents),
		"crio config options should reference the correct etc crio path")

	// Verify 10-crio.conf is copied to /opt/provisioner/sandbox/etc/crio/crio.conf.d/10-crio.conf.latest and contains expected content
	contents, err = os.ReadFile("/opt/provisioner/sandbox/etc/crio/crio.conf.d/10-crio.conf.latest")
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`/opt/provisioner/sandbox/usr/libexec/crio`), string(contents),
		"10-crio.conf should reference the correct libexec path")
	require.Regexp(t, regexp.MustCompile(`/opt/provisioner/sandbox/etc/crio`), string(contents),
		"10-crio.conf should reference the correct etc crio path")

	// Verify contents of /opt/provisioner/sandbox/etc/crio/crio.conf.d/10-crio.conf toml file
	contents, err = os.ReadFile("/opt/provisioner/sandbox/etc/crio/crio.conf.d/10-crio.conf.latest")
	var tomlEntries map[string]any
	_, err = toml.NewDecoder(bytes.NewBufferString(string(contents))).Decode(&tomlEntries)
	require.NoError(t, err, "10-crio.conf should be a valid toml file")
	//sudo ${SANDBOX_BIN}/dasel put -w toml -r toml -f "${SANDBOX_DIR}/etc/crio/crio.conf.d/10-crio.conf" -v runc '.crio.runtime.default_runtime'
	require.Equal(t, "runc", tomlEntries["crio"].(map[string]any)["runtime"].(map[string]any)["default_runtime"],
		"crio.default_runtime should be correctly set")
	//sudo ${SANDBOX_BIN}/dasel put -w toml -r toml -f "${SANDBOX_DIR}/etc/crio/crio.conf.d/10-crio.conf" -v "${SANDBOX_DIR}/etc/crio/keys" '.crio.runtime.decryption_keys_path'
	require.Equal(t, "/opt/provisioner/sandbox/etc/crio/keys", tomlEntries["crio"].(map[string]any)["runtime"].(map[string]any)["decryption_keys_path"],
		"crio.decryption_keys_path should be correctly set")
	//sudo ${SANDBOX_BIN}/dasel put -w toml -r toml -f "${SANDBOX_DIR}/etc/crio/crio.conf.d/10-crio.conf" -v "${SANDBOX_DIR}/var/run/crio/exits" '.crio.runtime.container_exits_dir'
	require.Equal(t, "/opt/provisioner/sandbox/var/run/crio/exits", tomlEntries["crio"].(map[string]any)["runtime"].(map[string]any)["container_exits_dir"],
		"crio.container_exits_dir should be correctly set")
	//sudo ${SANDBOX_BIN}/dasel put -w toml -r toml -f "${SANDBOX_DIR}/etc/crio/crio.conf.d/10-crio.conf" -v "${SANDBOX_DIR}/var/run/crio" '.crio.runtime.container_attach_socket_dir'
	require.Equal(t, "/opt/provisioner/sandbox/var/run/crio", tomlEntries["crio"].(map[string]any)["runtime"].(map[string]any)["container_attach_socket_dir"],
		"crio.container_attach_socket_dir should be correctly set")
	//sudo ${SANDBOX_BIN}/dasel put -w toml -r toml -f "${SANDBOX_DIR}/etc/crio/crio.conf.d/10-crio.conf" -v "${SANDBOX_DIR}/var/run" '.crio.runtime.namespaces_dir'
	require.Equal(t, "/opt/provisioner/sandbox/var/run", tomlEntries["crio"].(map[string]any)["runtime"].(map[string]any)["namespaces_dir"],
		"crio.namespaces_dir should be correctly set")
	//sudo ${SANDBOX_BIN}/dasel put -w toml -r toml -f "${SANDBOX_DIR}/etc/crio/crio.conf.d/10-crio.conf" -v "${SANDBOX_LOCAL_BIN}/pinns" '.crio.runtime.pinns_path'
	require.Equal(t, "/opt/provisioner/sandbox/usr/local/bin/pinns", tomlEntries["crio"].(map[string]any)["runtime"].(map[string]any)["pinns_path"],
		"crio.pinns_path should be correctly set")
	//sudo ${SANDBOX_BIN}/dasel put -w toml -r toml -f "${SANDBOX_DIR}/etc/crio/crio.conf.d/10-crio.conf" -v "${SANDBOX_DIR}/run/runc" '.crio.runtime.runtimes.runc.runtime_root'
	require.Equal(t, "/opt/provisioner/sandbox/run/runc", tomlEntries["crio"].(map[string]any)["runtime"].(map[string]any)["runtimes"].(map[string]any)["runc"].(map[string]any)["runtime_root"],
		"crio.runtimes.runc.runtime_root should be correctly set")
	//sudo ${SANDBOX_BIN}/dasel put -w toml -r toml -f "${SANDBOX_DIR}/etc/crio/crio.conf.d/10-crio.conf" -v "${SANDBOX_DIR}/var/run/crio/crio.sock" '.crio.api.listen'
	require.Equal(t, "/opt/provisioner/sandbox/var/run/crio/crio.sock", tomlEntries["crio"].(map[string]any)["api"].(map[string]any)["listen"],
		"crio.api.listen should be correctly set")
	//sudo ${SANDBOX_BIN}/dasel put -w toml -r toml -f "${SANDBOX_DIR}/etc/crio/crio.conf.d/10-crio.conf" -v "${SANDBOX_DIR}/var/lib/containers/storage" '.crio.root'
	require.Equal(t, "/opt/provisioner/sandbox/var/lib/containers/storage", tomlEntries["crio"].(map[string]any)["root"],
		"crio.root should be correctly set")
	//sudo ${SANDBOX_BIN}/dasel put -w toml -r toml -f "${SANDBOX_DIR}/etc/crio/crio.conf.d/10-crio.conf" -v "${SANDBOX_DIR}/var/run/containers/storage" '.crio.runroot'
	require.Equal(t, "/opt/provisioner/sandbox/var/run/containers/storage", tomlEntries["crio"].(map[string]any)["runroot"],
		"crio.runroot should be correctly set")
	//sudo ${SANDBOX_BIN}/dasel put -w toml -r toml -f "${SANDBOX_DIR}/etc/crio/crio.conf.d/10-crio.conf" -v "${SANDBOX_DIR}/var/run/crio/version" '.crio.version_file'
	require.Equal(t, "/opt/provisioner/sandbox/var/run/crio/version", tomlEntries["crio"].(map[string]any)["version_file"],
		"crio.version_file should be correctly set")
	//sudo ${SANDBOX_BIN}/dasel put -w toml -r toml -f "${SANDBOX_DIR}/etc/crio/crio.conf.d/10-crio.conf" -v "${SANDBOX_DIR}/var/logs/crio/pods" '.crio.log_dir'
	require.Equal(t, "/opt/provisioner/sandbox/var/logs/crio/pods", tomlEntries["crio"].(map[string]any)["log_dir"],
		"crio.log_dir should be correctly set")
	//sudo ${SANDBOX_BIN}/dasel put -w toml -r toml -f "${SANDBOX_DIR}/etc/crio/crio.conf.d/10-crio.conf" -v "${SANDBOX_DIR}/var/lib/crio/clean.shutdown" '.crio.clean_shutdown_file'
	require.Equal(t, "/opt/provisioner/sandbox/var/lib/crio/clean.shutdown", tomlEntries["crio"].(map[string]any)["clean_shutdown_file"],
		"crio.clean_shutdown_file should be correctly set")
	//sudo ${SANDBOX_BIN}/dasel put -w toml -r toml -f "${SANDBOX_DIR}/etc/crio/crio.conf.d/10-crio.conf" -v "${SANDBOX_DIR}/etc/cni/net.d/" '.crio.network.network_dir'
	require.Equal(t, "/opt/provisioner/sandbox/etc/cni/net.d", tomlEntries["crio"].(map[string]any)["network"].(map[string]any)["network_dir"],
		"crio.network.network_dir should be correctly set")
	//sudo ${SANDBOX_BIN}/dasel put -w toml -r toml -f "${SANDBOX_DIR}/etc/crio/crio.conf.d/10-crio.conf" -v "${SANDBOX_DIR}/opt/cni/bin" -s 'crio.network.plugin_dirs.[]'
	require.Equal(t, "/opt/provisioner/sandbox/opt/cni/bin", tomlEntries["crio"].(map[string]any)["network"].(map[string]any)["plugin_dirs"].([]any)[0],
		"crio.network.plugin_dirs[0] should be correctly set")
	//sudo ${SANDBOX_BIN}/dasel put -w toml -r toml -f "${SANDBOX_DIR}/etc/crio/crio.conf.d/10-crio.conf" -v "${SANDBOX_DIR}/opt/nri/plugins" '.crio.nri.nri_plugin_dir'
	require.Equal(t, "/opt/provisioner/sandbox/opt/nri/plugins", tomlEntries["crio"].(map[string]any)["nri"].(map[string]any)["nri_plugin_dir"],
		"crio.nri.nri_plugin_dir should be correctly set")
	//sudo ${SANDBOX_BIN}/dasel put -w toml -r toml -f "${SANDBOX_DIR}/etc/crio/crio.conf.d/10-crio.conf" -v "${SANDBOX_DIR}/etc/nri/conf.d" '.crio.nri.nri_plugin_config_dir'
	require.Equal(t, "/opt/provisioner/sandbox/etc/nri/conf.d", tomlEntries["crio"].(map[string]any)["nri"].(map[string]any)["nri_plugin_config_dir"],
		"crio.nri.nri_plugin_config_dir should be correctly set")
	//sudo ${SANDBOX_BIN}/dasel put -w toml -r toml -f "${SANDBOX_DIR}/etc/crio/crio.conf.d/10-crio.conf" -v "${SANDBOX_DIR}/var/run/nri/nri.sock" '.crio.nri.nri_listen'
	require.Equal(t, "/opt/provisioner/sandbox/var/run/nri/nri.sock", tomlEntries["crio"].(map[string]any)["nri"].(map[string]any)["nri_listen"],
		"crio.nri.nri_listen should be correctly set")

	// Verify ~/.crio-install is created and paths in it exist
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)
	contents, err = os.ReadFile(path.Join(homeDir, ".crio-install"))
	require.NoError(t, err)
	// Every line should contains a /opt/provisioner/sandbox path that exists
	lines := strings.Split(string(contents), "\n")
	require.Greater(t, len(lines), 0, ".crio-install should contain at least one path")
	for _, line := range lines {
		if line != "" {
			line = strings.TrimRight(line, "/*")
			require.True(t, strings.HasPrefix(line, "/opt/provisioner/sandbox/"),
				".crio-install should only contain /opt/provisioner/sandbox paths")
			_, err = os.Stat(line)
			require.NoError(t, err, "Path %s from .crio-install should exist", line)
		}
	}

	//
	// When - Cleanup
	//
	err = installer.Cleanup()
	require.NoError(t, err, "Failed to cleanup crio installation")

	// Check download folder is cleaned up
	_, exists, err := fileManager.PathExists("/opt/provisioner/tmp/cri-o")
	require.NoError(t, err)
	require.False(t, exists, "crio download temp folder should be cleaned up after installation")
}
