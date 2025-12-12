// SPDX-License-Identifier: Apache-2.0

package software

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/tomlx"
	"github.com/hashgraph/solo-weaver/pkg/hardware"
	"github.com/joomcode/errorx"
)

const (
	CrioServiceName = "crio"

	// File names - extracted as constants to avoid duplication
	CrioConfFile          = "10-crio.conf"
	CrioServiceFile       = "crio.service"
	PolicyJsonFile        = "policy.json"
	CrictlYamlFile        = "crictl.yaml"
	CrioUmountConfFile    = "crio-umount.conf"
	CrioConfDisabledFile  = "10-crio-bridge.conflist.disabled"
	CrioConf5File         = "crio.conf.5"
	CrioConfd5File        = "crio.conf.d.5"
	Crio8File             = "crio.8"
	CrioFishFile          = "crio.fish"
	RegistriesConfFile    = "registries.conf"
	CrioInstallFile       = ".crio-install"
	CrioDefaultConfigFile = "crio"

	// Directory names
	ContribDir = "contrib/"
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

// GetRegistriesConfPath returns the full path to the registries.conf file in the sandbox
// This is used by tests to install custom registry mirror configuration
func GetRegistriesConfPath() string {
	return filepath.Join(core.Paths().SandboxDir, containersRegistriesConfdDir, RegistriesConfFile)
}

type crioInstaller struct {
	*baseInstaller
	tomlManager *tomlx.TomlConfigManager
}

func NewCrioInstaller(opts ...InstallerOption) (Software, error) {
	bi, err := newBaseInstaller("cri-o", opts...)
	if err != nil {
		return nil, err
	}

	return &crioInstaller{
		baseInstaller: bi,
		tomlManager:   tomlx.NewTomlConfigManager(),
	}, nil
}

// Install installs cri-o emulating the same steps performed by the `install` file under the compressed file
// DESTDIR="${SANDBOX_DIR}" SYSTEMDDIR="/usr/lib/systemd/system" sudo -E "$(command -v bash)" ./install
func (ci *crioInstaller) Install() error {
	// Variables matching the shell script structure
	srcDir := path.Join(ci.extractFolder(), "cri-o")
	destDir := core.Paths().SandboxDir

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
		getSysconfigDir(),

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
			return NewInstallationError(err, ci.software.Name, ci.versionToBeInstalled)
		}
	}

	// Copy CNI plugins
	err := ci.copyCNIPlugins(srcDir, filepath.Join(destDir, optCniBinDir))
	if err != nil {
		return NewInstallationError(err, ci.software.Name, ci.versionToBeInstalled)
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
			return NewInstallationError(err, ci.software.Name, ci.versionToBeInstalled)
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
		ContribDir + CrioConfDisabledFile:  filepath.Join(destDir, cniDir, CrioConfDisabledFile),
		"etc/" + CrictlYamlFile:            filepath.Join(destDir, etcDir, CrictlYamlFile),
		"etc/" + CrioUmountConfFile:        filepath.Join(destDir, ociDir, CrioUmountConfFile),
		"etc/" + CrioDefaultConfigFile:     filepath.Join(destDir, getSysconfigDir(), CrioDefaultConfigFile),
		ContribDir + PolicyJsonFile:        filepath.Join(destDir, etcCrioDir, PolicyJsonFile),
		"etc/" + CrioConfFile:              filepath.Join(destDir, crioConfdDir, CrioConfFile),
		"man/" + CrioConf5File:             filepath.Join(destDir, man5Dir, CrioConf5File),
		"man/" + CrioConfd5File:            filepath.Join(destDir, man5Dir, CrioConfd5File),
		"man/" + Crio8File:                 filepath.Join(destDir, man8Dir, Crio8File),
		"completions/bash/crio":            filepath.Join(destDir, bashInstallDir, "crio"),
		"completions/fish/" + CrioFishFile: filepath.Join(destDir, fishInstallDir, CrioFishFile),
		"completions/zsh/_crio":            filepath.Join(destDir, zshInstallDir, "_crio"),
		ContribDir + CrioServiceFile:       filepath.Join(destDir, userSystemdDir, CrioServiceFile),
		ContribDir + RegistriesConfFile:    filepath.Join(destDir, containersRegistriesConfdDir, RegistriesConfFile),
	}
	for src, dst := range configs {
		if err := ci.installFile(filepath.Join(srcDir, src), dst, core.DefaultFilePerm); err != nil {
			return NewInstallationError(err, ci.software.Name, ci.versionToBeInstalled)
		}
	}

	// Record installed state
	_ = ci.GetStateManager().RecordState(ci.GetSoftwareName(), state.TypeInstalled, ci.Version())

	return nil
}

// copyCNIPlugins copies all CNI plugin binaries from the cni-plugins directory
// Matching the shell script logic:
// install $SELINUX -D -m 755 -t "$DESTDIR$OPT_CNI_BIN_DIR" cni-plugins/*
func (ci *crioInstaller) copyCNIPlugins(srcDir, destDir string) error {
	cniPluginsDir := filepath.Join(srcDir, "cni-plugins")

	// Make sure the cni-plugins directory exists
	if _, exists, err := ci.fileManager.PathExists(cniPluginsDir); err != nil {
		return NewInstallationError(errorx.IllegalState.Wrap(err, "failed to check cni-plugins directory"), ci.software.Name, ci.versionToBeInstalled)
	} else if !exists {
		return NewInstallationError(errorx.IllegalState.New("cni-plugins directory not found in extracted archive"), ci.software.Name, ci.versionToBeInstalled)
	}

	entries, err := os.ReadDir(cniPluginsDir)
	if err != nil {
		return NewInstallationError(errorx.IllegalState.Wrap(err, "failed to read cni-plugins directory"), ci.software.Name, ci.versionToBeInstalled)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		src := filepath.Join(cniPluginsDir, entry.Name())
		dst := filepath.Join(destDir, entry.Name())
		if err := ci.installFile(src, dst, core.DefaultDirOrExecPerm); err != nil {
			return NewInstallationError(err, ci.software.Name, ci.versionToBeInstalled)
		}
	}

	return nil
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

	// Patch crio.conf.d/10-crio.conf with adjusted paths
	err := ci.patchCrioConf(sandboxDir, binDir, libexecDir, etcCrioDir)
	if err != nil {
		return err
	}

	// Generate crio-install list using template
	err = ci.generateCrioInstallList(sandboxDir)
	if err != nil {
		return err
	}

	// Patch CRI-O systemd service file
	err = ci.patchServiceFile()
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
	crioConfPath := getCrioConfPath()
	err = ci.updateCrioTomlConfig(crioConfPath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to update CRI-O TOML configuration")
	}

	// Setup CRI-O Service SymLink
	systemdServicePath := filepath.Join(userSystemdDir, "crio.service")
	sandboxServicePath := getCrioServicePath()
	err = ci.fileManager.CreateSymbolicLink(sandboxServicePath, systemdServicePath, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create CRI-O service symlink")
	}

	// Record configured state
	_ = ci.GetStateManager().RecordState(ci.GetSoftwareName(), state.TypeConfigured, ci.Version())

	return nil
}

// patchCrioConf updates the CRI-O configuration file with the correct paths in place
// Matching the shell script logic:
// sed -i 's;/usr/bin;'"$DESTDIR$BINDIR"';g' "$DESTDIR$ETC_CRIO_DIR/crio.conf.d/10-crio.conf"
// sed -i 's;/usr/libexec;'"$DESTDIR$LIBEXECDIR"';g' "$DESTDIR$ETC_CRIO_DIR/crio.conf.d/10-crio.conf"
// sed -i 's;/etc/crio;'"$DESTDIR$ETC_CRIO_DIR"';g' "$DESTDIR$ETC_CRIO_DIR/crio.conf.d/10-crio.conf"
func (ci *crioInstaller) patchCrioConf(destdir, bindir, libexecdir, etcdir string) error {
	confPath := getCrioConfPath()

	// Replace binary paths with sandbox path
	// Update the DESTDIR in the CRI-O configuration, matching shell script sed commands
	err := ci.replaceAllInFile(confPath, usrBinDir, filepath.Join(destdir, bindir))
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to replace /usr/bin in crio.conf")
	}
	err = ci.replaceAllInFile(confPath, libexecDir, filepath.Join(destdir, libexecdir))
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to replace /usr/libexec in crio.conf")
	}
	err = ci.replaceAllInFile(confPath, etcCrioDir, filepath.Join(destdir, etcdir))
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to replace /etc/crio in crio.conf")
	}

	return nil
}

// getCrioConfPath returns the path to the 10-crio.conf file in the sandbox
func getCrioConfPath() string {
	return path.Join(core.Paths().SandboxDir, crioConfdDir, CrioConfFile)
}

// generateEtcDefaultCrioConfigurationFile generates the /etc/default/crio file in the sandbox
func (ci *crioInstaller) generateEtcDefaultCrioConfigurationFile() error {
	sandboxDir := core.Paths().SandboxDir

	// Generate the content using the shared logic
	crioConfigContent := ci.generateExpectedEtcDefaultCrioContent()

	err := os.WriteFile(filepath.Join(sandboxDir, "etc", "default", CrioDefaultConfigFile), []byte(crioConfigContent), core.DefaultFilePerm)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create CRI-O default file")
	}

	return nil
}

// patchServiceFile patches the CRI-O systemd service file with sandbox paths in place
func (ci *crioInstaller) patchServiceFile() error {
	serviceFilePath := getCrioServicePath()

	// Patch the service file
	err := ci.replaceAllInFile(serviceFilePath, "/usr/local/bin/crio", filepath.Join(core.Paths().SandboxLocalBinDir, "crio"))
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to patch crio.service file")
	}

	err = ci.replaceAllInFile(serviceFilePath, "/etc/sysconfig/crio", filepath.Join(core.Paths().SandboxDir, "etc", "default", CrioDefaultConfigFile))
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to patch crio.service sysconfig path")
	}

	// Record checksum using base installer helper with absolute path
	return nil
}

// getCrioServicePath returns the path to the crio.service file in the sandbox
func getCrioServicePath() string {
	return path.Join(core.Paths().SandboxDir, "usr", "lib", "systemd", "system", CrioServiceFile)
}

// updateCrioTomlConfig updates the CRI-O TOML configuration file with sandbox paths
func (ci *crioInstaller) updateCrioTomlConfig(confPath string) error {
	// Get expected configuration using shared logic
	expectedConfig := ci.generateExpectedCrioConfig()

	// Use TomlConfigManager to update the file
	return ci.tomlManager.UpdateTomlFile(confPath, expectedConfig)
}

// Generate crio-install list generates the crio-install list file
// This matches the shell command:
// touch ~/.crio-install
// cat <<EOF | tee ~/.crio-install
// $DESTDIR$OPT_CNI_BIN_DIR/*
// ...
// EOF
func (ci *crioInstaller) generateCrioInstallList(destDir string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to get user home directory")
	}
	installListPath := filepath.Join(homeDir, CrioInstallFile)

	// Generate the content using the shared logic
	content, err := ci.generateExpectedCrioInstallContent(destDir)
	if err != nil {
		return err
	}

	if err := os.WriteFile(installListPath, []byte(content), core.DefaultFilePerm); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to write crio-install list file")
	}

	return nil
}

// Uninstall removes the CRI-O software from the sandbox and cleans up related files
// This reverses the operations performed by Install()
func (ci *crioInstaller) Uninstall() error {
	sandboxDir := core.Paths().SandboxDir

	// Remove all installed binaries
	binaries := []string{
		filepath.Join(sandboxDir, libexecCrioDir, "conmon"),
		filepath.Join(sandboxDir, libexecCrioDir, "conmonrs"),
		filepath.Join(sandboxDir, libexecCrioDir, "crun"),
		filepath.Join(sandboxDir, libexecCrioDir, "runc"),
		filepath.Join(sandboxDir, binDir, "crio"),
		filepath.Join(sandboxDir, binDir, "pinns"),
		filepath.Join(sandboxDir, binDir, "crictl"),
	}

	for _, binary := range binaries {
		err := ci.fileManager.RemoveAll(binary)
		if err != nil {
			return errorx.IllegalState.Wrap(err, "failed to remove CRI-O binary %s", binary)
		}
	}

	// Remove CNI plugins directory
	cniPluginsDir := filepath.Join(sandboxDir, optCniBinDir)
	err := ci.fileManager.RemoveAll(cniPluginsDir)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to remove CNI plugins directory %s", cniPluginsDir)
	}

	// Remove configuration files
	configFiles := []string{
		filepath.Join(sandboxDir, cniDir, CrioConfDisabledFile),
		filepath.Join(sandboxDir, etcDir, CrictlYamlFile),
		filepath.Join(sandboxDir, ociDir, CrioUmountConfFile),
		filepath.Join(sandboxDir, getSysconfigDir(), CrioDefaultConfigFile),
		filepath.Join(sandboxDir, etcCrioDir, PolicyJsonFile),
		filepath.Join(sandboxDir, crioConfdDir, CrioConfFile),
		filepath.Join(sandboxDir, man5Dir, CrioConf5File),
		filepath.Join(sandboxDir, man5Dir, CrioConfd5File),
		filepath.Join(sandboxDir, man8Dir, Crio8File),
		filepath.Join(sandboxDir, bashInstallDir, "crio"),
		filepath.Join(sandboxDir, fishInstallDir, CrioFishFile),
		filepath.Join(sandboxDir, zshInstallDir, "_crio"),
		filepath.Join(sandboxDir, userSystemdDir, CrioServiceFile),
		filepath.Join(sandboxDir, containersRegistriesConfdDir, RegistriesConfFile),
	}

	for _, config := range configFiles {
		err := ci.fileManager.RemoveAll(config)
		if err != nil {
			return errorx.IllegalState.Wrap(err, "failed to remove CRI-O config file %s", config)
		}
	}

	// Remove directories created during installation (only if empty)
	installDirs := []string{
		filepath.Join(sandboxDir, libexecCrioDir),
		filepath.Join(sandboxDir, bashInstallDir),
		filepath.Join(sandboxDir, fishInstallDir),
		filepath.Join(sandboxDir, zshInstallDir),
		filepath.Join(sandboxDir, containersRegistriesConfdDir),
		filepath.Join(sandboxDir, ociDir),
		filepath.Join(sandboxDir, crioConfdDir),
		filepath.Join(sandboxDir, man5Dir),
		filepath.Join(sandboxDir, man8Dir),
		filepath.Join(sandboxDir, userSystemdDir),
		filepath.Join(sandboxDir, cniDir),
	}

	for _, dir := range installDirs {
		err := ci.fileManager.RemoveAll(dir)
		if err != nil {
			return errorx.IllegalState.Wrap(err, "failed to remove CRI-O install directory %s", dir)
		}
	}

	// Remove .crio-install file
	homeDir, err := os.UserHomeDir()
	if err == nil {
		installListPath := filepath.Join(homeDir, CrioInstallFile)
		err := ci.fileManager.RemoveAll(installListPath)
		if err != nil {
			return errorx.IllegalState.Wrap(err, "failed to remove CRI-O install list file %s", installListPath)
		}
	}

	// Remove installed state
	_ = ci.GetStateManager().RemoveState(ci.GetSoftwareName(), state.TypeInstalled)

	return nil
}

// RemoveConfiguration removes symlinks and restores the configuration files
// This reverses the operations performed by Configure()
func (ci *crioInstaller) RemoveConfiguration() error {
	// Remove CRI-O service symlink
	systemdServicePath := filepath.Join(userSystemdDir, "crio.service")
	err := ci.fileManager.RemoveAll(systemdServicePath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to remove CRI-O service symlink")
	}

	// Remove /etc/containers/registries.conf.d symlink
	err = ci.fileManager.RemoveAll(etcContainersFolder)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to remove CRI-O /etc/containers/registries.conf.d symlink")
	}

	// Remove /etc/default/crio configuration file from sandbox
	sandboxDir := core.Paths().SandboxDir
	etcDefaultCrioPath := filepath.Join(sandboxDir, "etc", "default", CrioDefaultConfigFile)
	err = ci.fileManager.RemoveAll(etcDefaultCrioPath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to remove /etc/default/crio configuration file")
	}

	// Remove configured state
	_ = ci.GetStateManager().RemoveState(ci.GetSoftwareName(), state.TypeConfigured)

	return nil
}

// generateExpectedCrioInstallContent generates the expected content for .crio-install file
// This mirrors the logic in generateCrioInstallList() without writing to file
func (ci *crioInstaller) generateExpectedCrioInstallContent(destDir string) (string, error) {
	// Define relativePaths relative to destDir (same as in generateCrioInstallList())
	relativePaths := []string{
		filepath.Join(optCniBinDir, "*"),
		filepath.Join(cniDir, CrioConfDisabledFile),
		filepath.Join(libexecCrioDir, "conmon"),
		filepath.Join(libexecCrioDir, "conmonrs"),
		filepath.Join(libexecCrioDir, "crun"),
		filepath.Join(libexecCrioDir, "runc"),
		filepath.Join(binDir, "crio"),
		filepath.Join(binDir, "pinns"),
		filepath.Join(binDir, "crictl"),
		filepath.Join(etcDir, CrictlYamlFile),
		filepath.Join(ociDir, CrioUmountConfFile),
		filepath.Join(getSysconfigDir(), CrioDefaultConfigFile),
		filepath.Join(etcCrioDir, PolicyJsonFile),
		filepath.Join(crioConfdDir, CrioConfFile),
		filepath.Join(man5Dir, CrioConf5File),
		filepath.Join(man5Dir, CrioConfd5File),
		filepath.Join(man8Dir, Crio8File),
		filepath.Join(bashInstallDir, "crio"),
		filepath.Join(fishInstallDir, CrioFishFile),
		filepath.Join(zshInstallDir, "_crio"),
		filepath.Join(userSystemdDir, CrioServiceFile),
		filepath.Join(containersRegistriesConfdDir, RegistriesConfFile),
	}

	// Prepend destDir to each
	var fullPaths []string
	for _, p := range relativePaths {
		fullPaths = append(fullPaths, filepath.Join(destDir, p))
	}

	return strings.Join(fullPaths, "\n") + "\n", nil
}

// generateExpectedEtcDefaultCrioContent generates the expected content for /etc/default/crio file
// This mirrors the logic in generateEtcDefaultCrioConfigurationFile() without writing to file
func (ci *crioInstaller) generateExpectedEtcDefaultCrioContent() string {
	sandboxDir := core.Paths().SandboxDir

	return fmt.Sprintf(`# /etc/default/crio

# use "--enable-metrics" and "--metrics-port value"
#CRIO_METRICS_OPTIONS="--enable-metrics"

#CRIO_NETWORK_OPTIONS=
#CRIO_STORAGE_OPTIONS=

# CRI-O configuration directory
CRIO_CONFIG_OPTIONS="--config-dir=%s/etc/crio/crio.conf.d"
`, sandboxDir)
}

// generateExpectedCrioConfig returns the expected CRI-O TOML configuration
// with sandbox-specific paths.
//
// The configuration includes:
// - Runtime configuration: default runtime, container paths, runtime roots
// - API configuration: socket paths for CRI-O API communication
// - Storage configuration: container storage paths and version files
// - Network configuration: CNI network directories and plugin paths
// - NRI configuration: Node Resource Interface plugin paths and sockets
//
// All paths are adjusted to use the sandbox directory structure rather than
// system-wide paths, enabling isolated testing and development environments.
func (ci *crioInstaller) generateExpectedCrioConfig() map[string]interface{} {
	sandboxDir := core.Paths().SandboxDir
	sandboxLocalBin := core.Paths().SandboxLocalBinDir

	config := make(map[string]interface{})

	// Runtime configuration
	ci.tomlManager.SetNestedValue(config, "crio.runtime.default_runtime", "runc")
	ci.tomlManager.SetNestedValue(config, "crio.runtime.decryption_keys_path", filepath.Join(sandboxDir, "etc/crio/keys"))
	ci.tomlManager.SetNestedValue(config, "crio.runtime.container_exits_dir", filepath.Join(sandboxDir, "var/run/crio/exits"))
	ci.tomlManager.SetNestedValue(config, "crio.runtime.container_attach_socket_dir", filepath.Join(sandboxDir, "var/run/crio"))
	ci.tomlManager.SetNestedValue(config, "crio.runtime.namespaces_dir", filepath.Join(sandboxDir, "var/run"))
	ci.tomlManager.SetNestedValue(config, "crio.runtime.pinns_path", filepath.Join(sandboxLocalBin, "pinns"))
	ci.tomlManager.SetNestedValue(config, "crio.runtime.runtimes.runc.runtime_root", filepath.Join(sandboxDir, "run/runc"))

	// API configuration
	ci.tomlManager.SetNestedValue(config, "crio.api.listen", filepath.Join(sandboxDir, "var/run/crio/crio.sock"))

	// Storage configuration
	ci.tomlManager.SetNestedValue(config, "crio.root", filepath.Join(sandboxDir, "var/lib/containers/storage"))
	ci.tomlManager.SetNestedValue(config, "crio.runroot", filepath.Join(sandboxDir, "var/run/containers/storage"))
	ci.tomlManager.SetNestedValue(config, "crio.version_file", filepath.Join(sandboxDir, "var/run/crio/version"))
	ci.tomlManager.SetNestedValue(config, "crio.log_dir", filepath.Join(sandboxDir, "var/logs/crio/pods"))
	ci.tomlManager.SetNestedValue(config, "crio.clean_shutdown_file", filepath.Join(sandboxDir, "var/lib/crio/clean.shutdown"))

	// Network configuration
	ci.tomlManager.SetNestedValue(config, "crio.network.network_dir", filepath.Join(sandboxDir, "etc/cni/net.d"))
	ci.tomlManager.SetNestedValue(config, "crio.network.plugin_dirs", []interface{}{filepath.Join(sandboxDir, "opt/cni/bin")})

	// NRI configuration
	ci.tomlManager.SetNestedValue(config, "crio.nri.nri_plugin_dir", filepath.Join(sandboxDir, "opt/nri/plugins"))
	ci.tomlManager.SetNestedValue(config, "crio.nri.nri_plugin_config_dir", filepath.Join(sandboxDir, "etc/nri/conf.d"))
	ci.tomlManager.SetNestedValue(config, "crio.nri.nri_listen", filepath.Join(sandboxDir, "var/run/nri/nri.sock"))

	return config
}
