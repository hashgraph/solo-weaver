package software

import (
	"fmt"
	"os"

	"path"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-provisioner/internal/core"
	"golang.hedera.com/solo-provisioner/pkg/hardware"
)

const (
	CrioServiceName = "crio"
)

var (
	etcContainersFolder = "/etc/containers"

	// Standard Linux directory paths matching shell script
	etcDir       = "/etc"
	optDir       = "/opt"
	usrDir       = "/usr"
	usrBinDir    = filepath.Join(usrDir, "bin")
	userLocalDir = filepath.Join(usrDir, "local")
	libexecDir   = filepath.Join(usrDir, "libexec")

	// Derived paths
	libexecCrioDir = filepath.Join(libexecDir, "crio")
	binDir         = filepath.Join(userLocalDir, "bin")
	shareDir       = filepath.Join(userLocalDir, "share")
	manDir         = filepath.Join(shareDir, "man")
	man5Dir        = filepath.Join(manDir, "man5")
	man8Dir        = filepath.Join(manDir, "man8")
	ociDir         = filepath.Join(shareDir, "oci-umount", "oci-umount.d")
	bashInstallDir = filepath.Join(shareDir, "bash-completion", "completions")
	fishInstallDir = filepath.Join(shareDir, "fish", "completions")
	zshInstallDir  = filepath.Join(shareDir, "zsh", "site-functions")
	userSystemdDir = filepath.Join(usrDir, "lib", "systemd", "system")

	cniDir                       = filepath.Join(etcDir, "cni", "net.d")
	optCniBinDir                 = filepath.Join(optDir, "cni", "bin")
	etcCrioDir                   = filepath.Join(etcDir, "crio")
	crioConfdDir                 = filepath.Join(etcCrioDir, "crio.conf.d")
	containersDir                = filepath.Join(etcDir, "containers")
	containersRegistriesConfdDir = filepath.Join(containersDir, "registries.conf.d")
)

type crioInstaller struct {
	*baseInstaller
}

func NewCrioInstaller(opts ...InstallerOption) (Software, error) {
	bi, err := newBaseInstaller("cri-o", opts...)
	if err != nil {
		return nil, err
	}

	return &crioInstaller{
		baseInstaller: bi,
	}, nil
}

// Install installs cri-o emulating the same steps performed by the `install` file under the compressed file
// DESTDIR="${SANDBOX_DIR}" SYSTEMDDIR="/usr/lib/systemd/system" sudo -E "$(command -v bash)" ./install
func (ci *crioInstaller) Install() error {
	// Variables matching the shell script structure
	srcDir := path.Join(ci.downloadFolder(), core.DefaultUnpackFolderName, "cri-o")
	destDir := core.Paths().SandboxDir

	// Get SYSCONFIGDIR based on OS
	sysconfigDir := getSysconfigDir()

	// Ensure directories exist
	dirs := []string{
		//install $SELINUX -d -m 755 "$DESTDIR$CNIDIR"
		//install $SELINUX -D -m 644 -t "$DESTDIR$CNIDIR" contrib/10-crio-bridge.conflist.disabled
		//install $SELINUX -D -m 644 -t "$DESTDIR$ETCDIR" etc/crictl.yaml
		cniDir,

		//install $SELINUX -D -m 755 -t "$DESTDIR$OPT_CNI_BIN_DIR" cni-plugins/*
		optCniBinDir,

		//install $SELINUX -d -m 755 "$DESTDIR$LIBEXEC_CRIO_DIR"
		//install $SELINUX -D -m 755 -t "$DESTDIR$LIBEXEC_CRIO_DIR" bin/conmon
		//install $SELINUX -D -m 755 -t "$DESTDIR$LIBEXEC_CRIO_DIR" bin/conmonrs
		//install $SELINUX -D -m 755 -t "$DESTDIR$LIBEXEC_CRIO_DIR" bin/crun
		//install $SELINUX -D -m 755 -t "$DESTDIR$LIBEXEC_CRIO_DIR" bin/runc
		libexecCrioDir,

		//install $SELINUX -d -m 755 "$DESTDIR$BASHINSTALLDIR"
		//install $SELINUX -D -m 644 -t "$DESTDIR$BASHINSTALLDIR" completions/bash/crio
		bashInstallDir,

		//install $SELINUX -d -m 755 "$DESTDIR$FISHINSTALLDIR"
		//install $SELINUX -D -m 644 -t "$DESTDIR$FISHINSTALLDIR" completions/fish/crio.fish
		fishInstallDir,

		//install $SELINUX -d -m 755 "$DESTDIR$ZSHINSTALLDIR"
		//install $SELINUX -D -m 644 -t "$DESTDIR$ZSHINSTALLDIR" completions/zsh/_crio
		zshInstallDir,

		//install $SELINUX -d -m 755 "$DESTDIR$CONTAINERS_REGISTRIES_CONFD_DIR"
		//install $SELINUX -D -m 644 -t "$DESTDIR$CONTAINERS_REGISTRIES_CONFD_DIR" contrib/registries.conf
		containersRegistriesConfdDir,

		//install $SELINUX -D -m 755 -t "$DESTDIR$BINDIR" bin/crio
		//install $SELINUX -D -m 755 -t "$DESTDIR$BINDIR" bin/pinns
		//install $SELINUX -D -m 755 -t "$DESTDIR$BINDIR" bin/crictl
		binDir,

		//install $SELINUX -D -m 644 -t "$DESTDIR$OCIDIR" etc/crio-umount.conf
		ociDir,

		//install $SELINUX -D -m 644 -t "$DESTDIR$SYSCONFIGDIR" etc/crio
		sysconfigDir,

		//install $SELINUX -D -m 644 -t "$DESTDIR$ETC_CRIO_DIR" contrib/policy.json
		//install $SELINUX -D -m 644 -t "$DESTDIR$ETC_CRIO_DIR/crio.conf.d" etc/10-crio.conf
		crioConfdDir,

		//install $SELINUX -D -m 644 -t "$DESTDIR$MANDIR/man5" man/crio.conf.5
		//install $SELINUX -D -m 644 -t "$DESTDIR$MANDIR/man5" man/crio.conf.d.5
		man5Dir,

		//install $SELINUX -D -m 644 -t "$DESTDIR$MANDIR/man8" man/crio.8
		man8Dir,

		//install $SELINUX -D -m 644 -t "$DESTDIR$SYSTEMDDIR" contrib/crio.service
		userSystemdDir,
	}
	for _, d := range dirs {
		err := ci.fileManager.CreateDirectory(filepath.Join(destDir, d), true)
		if err != nil {
			return err
		}
	}

	// Copy CNI plugins
	err := ci.copyCNIPlugins(srcDir, filepath.Join(destDir, optCniBinDir))
	if err != nil {
		return err
	}

	// Copy binary files
	//install $SELINUX -D -m 755 -t "$DESTDIR$LIBEXEC_CRIO_DIR" bin/conmon
	//install $SELINUX -D -m 755 -t "$DESTDIR$LIBEXEC_CRIO_DIR" bin/conmonrs
	//install $SELINUX -D -m 755 -t "$DESTDIR$LIBEXEC_CRIO_DIR" bin/crun
	//install $SELINUX -D -m 755 -t "$DESTDIR$LIBEXEC_CRIO_DIR" bin/runc
	//install $SELINUX -D -m 755 -t "$DESTDIR$BINDIR" bin/crio
	//install $SELINUX -D -m 755 -t "$DESTDIR$BINDIR" bin/pinns
	//install $SELINUX -D -m 755 -t "$DESTDIR$BINDIR" bin/crictl
	binaries := map[string]string{
		"conmon":   filepath.Join(destDir, libexecCrioDir, "conmon"),
		"conmonrs": filepath.Join(destDir, libexecCrioDir, "conmonrs"),
		"crun":     filepath.Join(destDir, libexecCrioDir, "crun"),
		"runc":     filepath.Join(destDir, libexecCrioDir, "runc"),
		"crio":     filepath.Join(destDir, binDir, "crio"),
		"pinns":    filepath.Join(destDir, binDir, "pinns"),
		"crictl":   filepath.Join(destDir, binDir, "crictl"),
	}
	for src, dst := range binaries {
		err := ci.installFile(filepath.Join(srcDir, "bin", src), dst, core.DefaultDirOrExecPerm)
		if err != nil {
			return err
		}
	}

	// Copy config files
	//install $SELINUX -D -m 644 -t "$DESTDIR$CNIDIR" contrib/10-crio-bridge.conflist.disabled
	//install $SELINUX -D -m 644 -t "$DESTDIR$ETCDIR" etc/crictl.yaml
	//install $SELINUX -D -m 644 -t "$DESTDIR$OCIDIR" etc/crio-umount.conf
	//install $SELINUX -D -m 644 -t "$DESTDIR$SYSCONFIGDIR" etc/crio
	//install $SELINUX -D -m 644 -t "$DESTDIR$ETC_CRIO_DIR" contrib/policy.json
	//install $SELINUX -D -m 644 -t "$DESTDIR$ETC_CRIO_DIR/crio.conf.d" etc/10-crio.conf
	//install $SELINUX -D -m 644 -t "$DESTDIR$MANDIR/man5" man/crio.conf.5
	//install $SELINUX -D -m 644 -t "$DESTDIR$MANDIR/man5" man/crio.conf.d.5
	//install $SELINUX -D -m 644 -t "$DESTDIR$MANDIR/man8" man/crio.8
	//install $SELINUX -D -m 644 -t "$DESTDIR$BASHINSTALLDIR" completions/bash/crio
	//install $SELINUX -D -m 644 -t "$DESTDIR$FISHINSTALLDIR" completions/fish/crio.fish
	//install $SELINUX -D -m 644 -t "$DESTDIR$ZSHINSTALLDIR" completions/zsh/_crio
	//install $SELINUX -D -m 644 -t "$DESTDIR$SYSTEMDDIR" contrib/crio.service
	//install $SELINUX -D -m 644 -t "$DESTDIR$CONTAINERS_REGISTRIES_CONFD_DIR" contrib/registries.conf
	configs := map[string]string{
		"contrib/10-crio-bridge.conflist.disabled": filepath.Join(destDir, cniDir, "10-crio-bridge.conflist.disabled"),
		"etc/crictl.yaml":                          filepath.Join(destDir, etcDir, "crictl.yaml"),
		"etc/crio-umount.conf":                     filepath.Join(destDir, ociDir, "crio-umount.conf"),
		"etc/crio":                                 filepath.Join(destDir, sysconfigDir, "crio"),
		"contrib/policy.json":                      filepath.Join(destDir, etcCrioDir, "policy.json"),
		"etc/10-crio.conf":                         filepath.Join(destDir, crioConfdDir, "10-crio.conf"),
		"man/crio.conf.5":                          filepath.Join(destDir, man5Dir, "crio.conf.5"),
		"man/crio.conf.d.5":                        filepath.Join(destDir, man5Dir, "crio.conf.d.5"),
		"man/crio.8":                               filepath.Join(destDir, man8Dir, "crio.8"),
		"completions/bash/crio":                    filepath.Join(destDir, bashInstallDir, "crio"),
		"completions/fish/crio.fish":               filepath.Join(destDir, fishInstallDir, "crio.fish"),
		"completions/zsh/_crio":                    filepath.Join(destDir, zshInstallDir, "_crio"),
		"contrib/crio.service":                     filepath.Join(destDir, userSystemdDir, "crio.service"),
		"contrib/registries.conf":                  filepath.Join(destDir, containersRegistriesConfdDir, "registries.conf"),
	}
	for src, dst := range configs {
		if err := ci.installFile(filepath.Join(srcDir, src), dst, core.DefaultFilePerm); err != nil {
			return err
		}
	}

	// Patch crio.conf.d/10-crio.conf with adjusted paths
	confPath := filepath.Join(destDir, crioConfdDir, "10-crio.conf")
	err = patchConfig(confPath, destDir, binDir, libexecDir, etcCrioDir)
	if err != nil {
		return err
	}

	// Generate crio-install list using template
	err = ci.generateCrioInstallList(destDir, sysconfigDir)
	if err != nil {
		return err
	}

	return nil
}

// copyCNIPlugins copies all CNI plugin binaries from the cni-plugins directory
// Matching the shell script logic:
// install $SELINUX -D -m 755 -t "$DESTDIR$OPT_CNI_BIN_DIR" cni-plugins/*
func (ci *crioInstaller) copyCNIPlugins(srcDir, destDir string) error {
	cniPluginsDir := filepath.Join(srcDir, "cni-plugins")

	entries, err := os.ReadDir(cniPluginsDir)
	if err != nil {
		return fmt.Errorf("failed to read cni-plugins directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		src := filepath.Join(cniPluginsDir, entry.Name())
		dst := filepath.Join(destDir, entry.Name())
		if err := ci.installFile(src, dst, core.DefaultDirOrExecPerm); err != nil {
			return err
		}
	}

	return nil
}

// patchConfig updates the CRI-O configuration file with the correct paths
// Matching the shell script logic:
// sed -i 's;/usr/bin;'"$DESTDIR$BINDIR"';g' "$DESTDIR$ETC_CRIO_DIR/crio.conf.d/10-crio.conf"
// sed -i 's;/usr/libexec;'"$DESTDIR$LIBEXECDIR"';g' "$DESTDIR$ETC_CRIO_DIR/crio.conf.d/10-crio.conf"
// sed -i 's;/etc/crio;'"$DESTDIR$ETC_CRIO_DIR"';g' "$DESTDIR$ETC_CRIO_DIR/crio.conf.d/10-crio.conf"
func patchConfig(path, destdir, bindir, libexecdir, etcdir string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := string(b)
	// Update the DESTDIR in the CRI-O configuration, matching shell script sed commands
	content = strings.ReplaceAll(content, usrBinDir, filepath.Join(destdir, bindir))
	content = strings.ReplaceAll(content, libexecDir, filepath.Join(destdir, libexecdir))
	content = strings.ReplaceAll(content, etcCrioDir, filepath.Join(destdir, etcdir))
	return os.WriteFile(path, []byte(content), core.DefaultFilePerm)
}

// getSysconfigDir returns the sysconfig directory for the current OS
// Unlike the shell install script, this function does not update the
// contents of the crio file because the file will be rewritten during
// Configure()
func getSysconfigDir() string {
	hostProfile := hardware.GetHostProfile()
	vendor := strings.ToLower(hostProfile.GetOSVendor())

	// Check if running on debian-based distribution
	if strings.Contains(vendor, "debian") || strings.Contains(vendor, "ubuntu") {
		return filepath.Join(etcDir, "default")
	}

	return filepath.Join(etcDir, "sysconfig")
}

// Configure configures the cri-o after installation
func (ci *crioInstaller) Configure() error {
	sandboxDir := core.Paths().SandboxDir

	err := ci.patchServiceFile()
	if err != nil {
		return err
	}

	// Symlink CRI-O /etc/containers/registries.conf.d
	sandboxEtcContainerDir := path.Join(sandboxDir, etcContainersFolder)
	err = ci.fileManager.CreateSymbolicLink(sandboxEtcContainerDir, etcContainersFolder, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create CRI-O symlink /etc/containers/registries.conf.d")
	}

	// Create the /etc/default/crio configuration file
	err = ci.generateEtcDefaultCrioConfigurationFile()
	if err != nil {
		return err
	}

	// Update CRI-O TOML configuration
	crioConfPath := filepath.Join(sandboxDir, crioConfdDir, "10-crio.conf")
	err = ci.updateCrioTomlConfig(crioConfPath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to update CRI-O TOML configuration")
	}

	// Setup CRI-O Service SymLink
	systemdServicePath := filepath.Join(userSystemdDir, "crio.service")
	sandboxServicePath := filepath.Join(sandboxDir, userSystemdDir, "crio.service")
	err = ci.fileManager.CreateSymbolicLink(sandboxServicePath, systemdServicePath, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create CRI-O service symlink")
	}

	return nil
}

// generateEtcDefaultCrioConfigurationFile generates the /etc/default/crio file in the sandbox
func (ci *crioInstaller) generateEtcDefaultCrioConfigurationFile() error {
	sandboxDir := core.Paths().SandboxDir

	crioConfigContent := fmt.Sprintf(`# /etc/default/crio

# use "--enable-metrics" and "--metrics-port value"
#CRIO_METRICS_OPTIONS="--enable-metrics"

#CRIO_NETWORK_OPTIONS=
#CRIO_STORAGE_OPTIONS=

# CRI-O configuration directory
CRIO_CONFIG_OPTIONS="--config-dir=%s/etc/crio/crio.conf.d"
`, sandboxDir)

	err := os.WriteFile(filepath.Join(sandboxDir, "etc", "default", "crio"), []byte(crioConfigContent), core.DefaultFilePerm)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create CRI-O default file")
	}

	return nil
}

// patchServiceFile patches the CRI-O systemd service file with sandbox paths
func (ci *crioInstaller) patchServiceFile() error {
	sandboxDir := core.Paths().SandboxDir
	sandboxLocalBin := core.Paths().SandboxLocalBinDir

	crioServicePath := filepath.Join(sandboxDir, "usr", "lib", "systemd", "system", "crio.service")

	// Patch the service file
	err := ci.replaceAllInFile(crioServicePath, "/usr/local/bin/crio", filepath.Join(sandboxLocalBin, "crio"))
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to patch crio.service file")
	}

	err = ci.replaceAllInFile(crioServicePath, "/etc/sysconfig/crio", filepath.Join(sandboxDir, "etc", "default", "crio"))
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to patch crio.service sysconfig path")
	}

	return nil
}

// updateCrioTomlConfig updates the CRI-O TOML configuration file with sandbox paths
func (ci *crioInstaller) updateCrioTomlConfig(confPath string) error {
	// Read the existing TOML file
	data, err := os.ReadFile(confPath)
	if err != nil {
		return err
	}

	// Parse the TOML content into a generic map to preserve all existing configuration
	var config map[string]interface{}
	err = toml.Unmarshal(data, &config)
	if err != nil {
		return err
	}

	// Update all the configuration values with sandbox paths
	ci.setCrioConfigPaths(config)

	// Marshal back to TOML
	file, err := os.Create(confPath)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	encoder := toml.NewEncoder(file)
	err = encoder.Encode(config)
	if err != nil {
		return err
	}

	return nil
}

// setCrioConfigPaths updates all CRI-O configuration paths in the config map
func (ci *crioInstaller) setCrioConfigPaths(config map[string]interface{}) {
	sandboxDir := core.Paths().SandboxDir
	sandboxLocalBin := core.Paths().SandboxLocalBinDir

	// Runtime configuration
	setNestedValue(config, "crio.runtime.default_runtime", "runc")
	setNestedValue(config, "crio.runtime.decryption_keys_path", filepath.Join(sandboxDir, "etc/crio/keys"))
	setNestedValue(config, "crio.runtime.container_exits_dir", filepath.Join(sandboxDir, "var/run/crio/exits"))
	setNestedValue(config, "crio.runtime.container_attach_socket_dir", filepath.Join(sandboxDir, "var/run/crio"))
	setNestedValue(config, "crio.runtime.namespaces_dir", filepath.Join(sandboxDir, "var/run"))
	setNestedValue(config, "crio.runtime.pinns_path", filepath.Join(sandboxLocalBin, "pinns"))
	setNestedValue(config, "crio.runtime.runtimes.runc.runtime_root", filepath.Join(sandboxDir, "run/runc"))

	// API configuration
	setNestedValue(config, "crio.api.listen", filepath.Join(sandboxDir, "var/run/crio/crio.sock"))

	// Storage configuration
	setNestedValue(config, "crio.root", filepath.Join(sandboxDir, "var/lib/containers/storage"))
	setNestedValue(config, "crio.runroot", filepath.Join(sandboxDir, "var/run/containers/storage"))
	setNestedValue(config, "crio.version_file", filepath.Join(sandboxDir, "var/run/crio/version"))
	setNestedValue(config, "crio.log_dir", filepath.Join(sandboxDir, "var/logs/crio/pods"))
	setNestedValue(config, "crio.clean_shutdown_file", filepath.Join(sandboxDir, "var/lib/crio/clean.shutdown"))

	// Network configuration
	setNestedValue(config, "crio.network.network_dir", filepath.Join(sandboxDir, "etc/cni/net.d/"))
	setNestedValue(config, "crio.network.plugin_dirs", []string{filepath.Join(sandboxDir, "opt/cni/bin")})

	// NRI configuration
	setNestedValue(config, "crio.nri.nri_plugin_dir", filepath.Join(sandboxDir, "opt/nri/plugins"))
	setNestedValue(config, "crio.nri.nri_plugin_config_dir", filepath.Join(sandboxDir, "etc/nri/conf.d"))
	setNestedValue(config, "crio.nri.nri_listen", filepath.Join(sandboxDir, "var/run/nri/nri.sock"))
}

// setNestedValue safely sets nested values in a map, creating intermediate maps as needed
// The value can be a string, slice, or any other type
func setNestedValue(config map[string]interface{}, path string, value interface{}) {
	keys := strings.Split(path, ".")
	m := config

	// Navigate/create the nested structure
	for i := 0; i < len(keys)-1; i++ {
		key := keys[i]
		if _, exists := m[key]; !exists {
			m[key] = make(map[string]interface{})
		}
		// Type assertion to continue navigating
		if nextMap, ok := m[key].(map[string]interface{}); ok {
			m = nextMap
		} else {
			// If the existing value isn't a map, replace it
			newMap := make(map[string]interface{})
			m[key] = newMap
			m = newMap
		}
	}
	// Set the final value
	m[keys[len(keys)-1]] = value
}

// Generate crio-install list generates the crio-install list file
// This matches the shell command:
// touch ~/.crio-install
// cat <<EOF | tee ~/.crio-install
// $DESTDIR$OPT_CNI_BIN_DIR/*
// ...
// EOF
func (ci *crioInstaller) generateCrioInstallList(destDir, sysconfigDir string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to get user home directory")
	}
	installListPath := filepath.Join(homeDir, ".crio-install")

	// Define relativePaths relative to destDir
	relativePaths := []string{
		filepath.Join(optCniBinDir, "*"),
		filepath.Join(cniDir, "10-crio-bridge.conflist.disabled"),
		filepath.Join(libexecCrioDir, "conmon"),
		filepath.Join(libexecCrioDir, "conmonrs"),
		filepath.Join(libexecCrioDir, "crun"),
		filepath.Join(libexecCrioDir, "runc"),
		filepath.Join(binDir, "crio"),
		filepath.Join(binDir, "pinns"),
		filepath.Join(binDir, "crictl"),
		filepath.Join(etcDir, "crictl.yaml"),
		filepath.Join(ociDir, "crio-umount.conf"),
		filepath.Join(sysconfigDir, "crio"),
		filepath.Join(etcCrioDir, "policy.json"),
		filepath.Join(crioConfdDir, "10-crio.conf"),
		filepath.Join(man5Dir, "crio.conf.5"),
		filepath.Join(man5Dir, "crio.conf.d.5"),
		filepath.Join(man8Dir, "crio.8"),
		filepath.Join(bashInstallDir, "crio"),
		filepath.Join(fishInstallDir, "crio.fish"),
		filepath.Join(zshInstallDir, "_crio"),
		filepath.Join(userSystemdDir, "crio.service"),
		filepath.Join(containersRegistriesConfdDir, "registries.conf"),
	}

	// Prepend destDir to each
	var fullPaths []string
	for _, p := range relativePaths {
		fullPaths = append(fullPaths, filepath.Join(destDir, p))
	}

	content := strings.Join(fullPaths, "\n") + "\n"

	if err := os.WriteFile(installListPath, []byte(content), core.DefaultFilePerm); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to write crio-install list file")
	}

	return nil
}
