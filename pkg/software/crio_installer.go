package software

import (
	"bytes"
	"fmt"
	"os"

	"path"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-weaver/internal/core"
	"golang.hedera.com/solo-weaver/internal/tomlx"
	"golang.hedera.com/solo-weaver/pkg/hardware"
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
	srcDir := path.Join(ci.downloadFolder(), core.DefaultUnpackFolderName, "cri-o")
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
	latestCrioConfPath := getLatestPath(getCrioConfPath())
	err = ci.updateCrioTomlConfig(latestCrioConfPath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to update CRI-O TOML configuration")
	}

	// Setup CRI-O Service SymLink
	systemdServicePath := filepath.Join(userSystemdDir, "crio.service")
	latestSandboxServicePath := getLatestPath(getCrioServicePath())
	err = ci.fileManager.CreateSymbolicLink(latestSandboxServicePath, systemdServicePath, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create CRI-O service symlink")
	}

	return nil
}

// patchCrioConf updates the CRI-O configuration file with the correct paths
// Matching the shell script logic:
// sed -i 's;/usr/bin;'"$DESTDIR$BINDIR"';g' "$DESTDIR$ETC_CRIO_DIR/crio.conf.d/10-crio.conf"
// sed -i 's;/usr/libexec;'"$DESTDIR$LIBEXECDIR"';g' "$DESTDIR$ETC_CRIO_DIR/crio.conf.d/10-crio.conf"
// sed -i 's;/etc/crio;'"$DESTDIR$ETC_CRIO_DIR"';g' "$DESTDIR$ETC_CRIO_DIR/crio.conf.d/10-crio.conf"
func (ci *crioInstaller) patchCrioConf(destdir, bindir, libexecdir, etcdir string) error {
	originalConfPath := getCrioConfPath()
	latestConfPath := getLatestPath(originalConfPath)

	// Create latest subfolder if it doesn't exist
	latestDir := path.Dir(latestConfPath)
	err := ci.fileManager.CreateDirectory(latestDir, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create latest subfolder at %s", latestDir)
	}

	// Create latest file which will have some strings replaced
	err = ci.fileManager.CopyFile(originalConfPath, latestConfPath, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create crio.conf file in latest subfolder")
	}

	// Replace binary paths with sandbox path
	// Update the DESTDIR in the CRI-O configuration, matching shell script sed commands
	err = ci.replaceAllInFile(latestConfPath, usrBinDir, filepath.Join(destdir, bindir))
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to replace /usr/bin in crio.conf")
	}
	err = ci.replaceAllInFile(latestConfPath, libexecDir, filepath.Join(destdir, libexecdir))
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to replace /usr/libexec in crio.conf")
	}
	err = ci.replaceAllInFile(latestConfPath, etcCrioDir, filepath.Join(destdir, etcdir))
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

// patchServiceFile patches the CRI-O systemd service file with sandbox paths
func (ci *crioInstaller) patchServiceFile() error {
	originalServiceFilePath := getCrioServicePath()
	latestServiceFilePath := getLatestPath(originalServiceFilePath)

	// Create latest subfolder if it doesn't exist
	latestDir := path.Dir(latestServiceFilePath)
	err := ci.fileManager.CreateDirectory(latestDir, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create latest subfolder at %s", latestDir)
	}

	// Create latest file which will have some strings replaced
	err = ci.fileManager.CopyFile(originalServiceFilePath, latestServiceFilePath, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create crio.service file in latest subfolder")
	}

	// Patch the service file
	err = ci.replaceAllInFile(latestServiceFilePath, "/usr/local/bin/crio", filepath.Join(core.Paths().SandboxLocalBinDir, "crio"))
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to patch crio.service file")
	}

	err = ci.replaceAllInFile(latestServiceFilePath, "/etc/sysconfig/crio", filepath.Join(core.Paths().SandboxDir, "etc", "default", CrioDefaultConfigFile))
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to patch crio.service sysconfig path")
	}

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

	// Remove latest configuration file
	latestConfPath := getLatestPath(getCrioConfPath())
	err = ci.fileManager.RemoveAll(latestConfPath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to remove latest configuration file for CRI-O at %s", latestConfPath)
	}

	latestServicePath := getLatestPath(getCrioServicePath())
	err = ci.fileManager.RemoveAll(latestServicePath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to remove latest service file for CRI-O at %s", latestServicePath)
	}

	// Remove /etc/default/crio configuration file from sandbox
	sandboxDir := core.Paths().SandboxDir
	etcDefaultCrioPath := filepath.Join(sandboxDir, "etc", "default", CrioDefaultConfigFile)
	err = ci.fileManager.RemoveAll(etcDefaultCrioPath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to remove /etc/default/crio configuration file")
	}

	return nil
}

// IsInstalled checks if CRI-O has been fully installed
// This includes checking binaries are installed, core directories exist, and configuration files are present
func (ci *crioInstaller) IsInstalled() (bool, error) {
	sandboxDir := core.Paths().SandboxDir

	cniPluginsInstalled, err := ci.areCniPluginsInstalled(sandboxDir)
	if err != nil {
		return false, err
	}
	if !cniPluginsInstalled {
		return false, nil
	}

	areBinaryInstalled, err := ci.areBinariesInstalled(sandboxDir)
	if err != nil {
		return false, err
	}
	if !areBinaryInstalled {
		return false, nil
	}

	areConfigFilesInstalled, err := ci.areConfigFilesInstalled(sandboxDir)
	if err != nil {
		return false, err
	}
	if !areConfigFilesInstalled {
		return false, nil
	}

	return true, nil
}

// areCniPluginsInstalled checks CNI plugins files are present by making sure the number of files
// under the CNI bin directory matches the expected count
func (ci *crioInstaller) areCniPluginsInstalled(sandboxDir string) (bool, error) {
	targetPath := filepath.Join(sandboxDir, optCniBinDir)
	if _, exists, err := ci.fileManager.PathExists(targetPath); err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to check CNI plugins directory existence")
	} else if !exists {
		return false, nil
	}

	entries, err := os.ReadDir(targetPath)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to read CNI plugins directory")
	}
	if len(entries) == 0 {
		return false, nil
	}
	return true, nil
}

// areBinariesInstalled checks if all CRI-O binaries are installed
// by verifying their existence and checksums
func (ci *crioInstaller) areBinariesInstalled(sandboxDir string) (bool, error) {
	// Map of binary source paths to their expected destination sandbox paths
	binaryFilesMapping := map[string]string{
		"cri-o/bin/crio":     filepath.Join(sandboxDir, binDir, "crio"),
		"cri-o/bin/crictl":   filepath.Join(sandboxDir, binDir, "crictl"),
		"cri-o/bin/pinns":    filepath.Join(sandboxDir, binDir, "pinns"),
		"cri-o/bin/conmon":   filepath.Join(sandboxDir, libexecCrioDir, "conmon"),
		"cri-o/bin/conmonrs": filepath.Join(sandboxDir, libexecCrioDir, "conmonrs"),
		"cri-o/bin/runc":     filepath.Join(sandboxDir, libexecCrioDir, "runc"),
		"cri-o/bin/crun":     filepath.Join(sandboxDir, libexecCrioDir, "crun"),
	}

	versionInfo, exists := ci.software.Versions[Version(ci.versionToBeInstalled)]
	platform := ci.software.getPlatform()

	if !exists {
		return false, NewVersionNotFoundError(ci.software.Name, ci.versionToBeInstalled)
	}
	for _, binary := range versionInfo.Binaries {
		// Check if binary mapping exists FIRST
		pathInSandbox, found := binaryFilesMapping[binary.Name]
		if !found || pathInSandbox == "" {
			return false, errorx.IllegalState.New("binary mapping not found for: %s (this is likely a configuration error)", binary.Name)
		}

		// Check if binary exists in the sandbox
		if _, exists, err := ci.fileManager.PathExists(pathInSandbox); err != nil {
			return false, errorx.IllegalState.Wrap(err, "failed to check binary existence: %s at path: %s", binary.Name, pathInSandbox)
		} else if !exists {
			return false, nil
		}

		// Get expected checksum for this binary
		osInfo, exists := binary.PlatformChecksum[platform.os]
		if !exists {
			return false, NewPlatformNotFoundError(ci.software.Name, ci.versionToBeInstalled, platform.os, "")
		}
		checksum, exists := osInfo[platform.arch]
		if !exists {
			return false, NewPlatformNotFoundError(ci.software.Name, ci.versionToBeInstalled, platform.os, platform.arch)
		}

		err := VerifyChecksum(pathInSandbox, checksum.Value, checksum.Algorithm)
		if err != nil {
			return false, errorx.IllegalState.Wrap(err, "checksum verification failed for binary: %s at path: %s", binary.Name, pathInSandbox)
		}
	}

	return true, nil
}

// areConfigFilesInstalled checks if all essential CRI-O configuration files are installed
func (ci *crioInstaller) areConfigFilesInstalled(sandboxDir string) (bool, error) {
	// Check essential configuration files
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
		if _, exists, err := ci.fileManager.PathExists(config); err != nil {
			return false, err
		} else if !exists {
			return false, nil
		}
	}

	return true, nil
}

// IsConfigured checks if CRI-O has been fully configured
// This includes checking configuration files and symlinks
// to ensure they are correctly set up
func (ci *crioInstaller) IsConfigured() (bool, error) {
	// Check .crio-install is valid
	crioInstallValid, err := ci.isCrioInstallListValid()
	if err != nil {
		return false, err
	}
	if !crioInstallValid {
		return false, nil
	}

	// Check if /etc/containers/registries.conf.d symlink
	if !ci.fileManager.IsSymbolicLink(etcContainersFolder) {
		return false, nil
	}

	// Check /etc/default/crio is valid
	etcDefaultCrioValid, err := ci.isEtcDefaultCrioValid()
	if err != nil {
		return false, err
	}
	if !etcDefaultCrioValid {
		return false, nil
	}

	// Check if 10-crio.conf.latest is valid
	latestConfValid, err := ci.isLatestCrioConfValid()
	if err != nil {
		return false, err
	}
	if !latestConfValid {
		return false, nil
	}

	// Check if crio.service.latest is valid
	latestCrioServiceValid, err := ci.isLatestCrioServiceValid()
	if err != nil {
		return false, err
	}
	if !latestCrioServiceValid {
		return false, nil
	}

	// Check symlink for crio.service is valid
	crioServiceSymlinkValid, err := ci.isCrioServiceSymlinkValid()
	if err != nil {
		return false, err
	}
	if !crioServiceSymlinkValid {
		return false, nil
	}

	return true, nil
}

// isLatestCrioConfValid checks if the 10-crio.conf file in the latest subfolder is valid
// by comparing its content with the expected content
func (ci *crioInstaller) isLatestCrioConfValid() (bool, error) {
	latestConfPath := getLatestPath(getCrioConfPath())

	// Check if the file in latest subfolder exists
	fi, exists, err := ci.fileManager.PathExists(latestConfPath)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to check if latest 10-crio.conf exists at %s", latestConfPath)
	}
	if !exists || !ci.fileManager.IsRegularFileByFileInfo(fi) {
		return false, nil
	}

	// Read and parse TOML configuration file
	contents, err := os.ReadFile(latestConfPath)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to read file at %s", latestConfPath)
	}

	var tomlEntries map[string]any
	if _, err = toml.NewDecoder(bytes.NewBufferString(string(contents))).Decode(&tomlEntries); err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to parse toml file at %s", latestConfPath)
	}

	// Get expected configuration using shared logic
	expectedConfig := ci.generateExpectedCrioConfig()

	// Validate each configuration value by iterating through the expected config
	return ci.tomlManager.ValidateConfigValues(tomlEntries, expectedConfig), nil
}

// isCrioInstallListValid checks if the .crio-install file exists and contains the correct content
// by comparing it with the expected paths that would be generated by generateCrioInstallList()
func (ci *crioInstaller) isCrioInstallListValid() (bool, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to get user home directory")
	}
	installListPath := filepath.Join(homeDir, CrioInstallFile)

	// Check if the .crio-install file exists
	fi, exists, err := ci.fileManager.PathExists(installListPath)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to check if .crio-install exists at %s", installListPath)
	}
	if !exists || !ci.fileManager.IsRegularFileByFileInfo(fi) {
		return false, nil
	}

	// Read the current content
	currentContent, err := os.ReadFile(installListPath)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to read .crio-install file")
	}

	// Generate expected content using the same logic as generateCrioInstallList()
	sandboxDir := core.Paths().SandboxDir
	expectedContent, err := ci.generateExpectedCrioInstallContent(sandboxDir)
	if err != nil {
		return false, err
	}

	// Compare the content
	return string(currentContent) == expectedContent, nil
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

// isEtcDefaultCrioValid checks if the /etc/default/crio file exists and contains the correct content
// by comparing it with the expected content that would be generated by generateEtcDefaultCrioConfigurationFile()
func (ci *crioInstaller) isEtcDefaultCrioValid() (bool, error) {
	sandboxDir := core.Paths().SandboxDir
	etcDefaultCrioPath := filepath.Join(sandboxDir, "etc", "default", CrioDefaultConfigFile)

	// Check if the /etc/default/crio file exists
	fi, exists, err := ci.fileManager.PathExists(etcDefaultCrioPath)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to check if /etc/default/crio exists at %s", etcDefaultCrioPath)
	}
	if !exists || !ci.fileManager.IsRegularFileByFileInfo(fi) {
		return false, nil
	}

	// Read the current content
	currentContent, err := os.ReadFile(etcDefaultCrioPath)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to read /etc/default/crio file")
	}

	// Generate expected content using the same logic as generateEtcDefaultCrioConfigurationFile()
	expectedContent := ci.generateExpectedEtcDefaultCrioContent()

	// Compare the content
	return string(currentContent) == expectedContent, nil
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

// isLatestCrioServiceValid checks if the crio.service file in the latest subfolder exists and contains valid content
// by comparing it with the expected content that would be generated by patchServiceFile()
func (ci *crioInstaller) isLatestCrioServiceValid() (bool, error) {
	latestServicePath := getLatestPath(getCrioServicePath())

	// Check if the file in latest subfolder exists
	fi, exists, err := ci.fileManager.PathExists(latestServicePath)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to check if crio.service.latest exists at %s", latestServicePath)
	}
	if !exists || !ci.fileManager.IsRegularFileByFileInfo(fi) {
		return false, nil
	}

	// Read the current content, and validate the content contains the expected sandbox paths
	currentContent, err := os.ReadFile(latestServicePath)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to read crio.service.latest file")
	}

	contentStr := string(currentContent)
	expectedSandboxBinPath := filepath.Join(core.Paths().SandboxLocalBinDir, "crio")
	expectedSandboxConfigPath := filepath.Join(core.Paths().SandboxDir, "etc", "default", CrioDefaultConfigFile)

	// Check if the service file has been properly patched with sandbox paths
	if !strings.Contains(contentStr, expectedSandboxBinPath) {
		return false, nil
	}
	if !strings.Contains(contentStr, expectedSandboxConfigPath) {
		return false, nil
	}

	return true, nil
}

// isCrioServiceSymlinkValid checks if the crio.service symlink exists and points to the correct .latest file
func (ci *crioInstaller) isCrioServiceSymlinkValid() (bool, error) {
	systemServicePath := filepath.Join(userSystemdDir, CrioServiceFile)
	expectedTarget := getLatestPath(getCrioServicePath())

	// Check if the symlink exists
	if !ci.fileManager.IsSymbolicLink(systemServicePath) {
		return false, nil
	}

	// Check if symlink points to the correct target
	actualTarget, err := os.Readlink(systemServicePath)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to read symlink target for %s", systemServicePath)
	}

	return actualTarget == expectedTarget, nil
}

// generateExpectedCrioConfig returns the expected CRI-O TOML configuration
// with sandbox-specific paths. This method centralizes the configuration definition
// for both setting during Configure() and validation during IsConfigured().
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
