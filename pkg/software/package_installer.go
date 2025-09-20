package software

import (
	"bufio"
	"github.com/automa-saga/logx"
	"github.com/bluet/syspkg"
	"github.com/bluet/syspkg/manager"
	"github.com/bluet/syspkg/manager/apt"
	"github.com/joomcode/errorx"
	"github.com/pkg/errors"
	"os"
	"runtime"
	"strings"
	"sync"
)

var (
	pkgManager syspkg.PackageManager
	once       sync.Once
)

func GetPackageManager() (syspkg.PackageManager, error) {
	var initErr error
	once.Do(func() {
		includeOptions := syspkg.IncludeOptions{AllAvailable: true}
		sysPackageManager, err := syspkg.New(includeOptions)
		if err != nil {
			initErr = errorx.IllegalState.New("failed to initialize package manager: %s", err.Error())
			return
		}

		pkgManagerName, err := DetectSystemPackageManager()
		if err != nil {
			initErr = err
			return
		}

		pm, err := sysPackageManager.GetPackageManager(pkgManagerName)
		if err != nil {
			initErr = errorx.IllegalState.New("failed to get package manager: %s", err.Error())
			return
		}

		pkgManager = pm
	})

	return pkgManager, initErr
}

func DetectSystemPackageManager() (string, error) {
	// Read the /etc/os-release file to determine the OS only if it is Linux
	if _, err := os.Stat("/etc/os-release"); errors.Is(err, os.ErrNotExist) {
		return "", errorx.IllegalState.New("unsupported operating system: %s", runtime.GOOS)
	}

	file, err := os.Open("/etc/os-release")
	if err != nil {
		return "", errorx.IllegalState.New("failed to open /etc/os-release: %s", err.Error())
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			logx.As().Warn().Err(err).Msg("failed to close /etc/os-release")
		}
	}(file)

	scanner := bufio.NewScanner(file)
	var id string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "ID=") {
			id = strings.TrimPrefix(line, "ID=")
			id = strings.Trim(id, "\"")
			break
		}
	}

	switch id {
	case "debian", "ubuntu":
		return "apt", nil
	case "fedora", "centos":
		return "dnf", nil
	case "alpine":
		return "apk", nil
	//case "opensuse", "suse":
	//	return "zypper", nil
	//case "flatpak":
	//	return "flatpak", nil
	//case "snap":
	//	return "snap", nil
	default:
		return "unknown", errorx.IllegalState.New("unsupported or unknown Linux distribution: %s", id)
	}
}

func RefreshPackageIndex() error {
	pm, err := GetPackageManager()
	if err != nil {
		return err
	}

	return pm.Refresh(&manager.Options{DryRun: false, Interactive: false, AssumeYes: true})
}

type option func(*PackageInstaller)

// PackageInstaller is the default implementation of the Package interface that uses standard system package
// manager to manage a system package
type PackageInstaller struct {
	pkgName    string
	pkgOptions manager.Options
	pkgManager syspkg.PackageManager
}

func (p *PackageInstaller) Install() (*syspkg.PackageInfo, error) {
	_, err := p.pkgManager.Install([]string{p.pkgName}, &p.pkgOptions)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to install package")
	}

	return p.Info()
}

func (p *PackageInstaller) Uninstall() (*syspkg.PackageInfo, error) {
	_, err := p.pkgManager.Delete([]string{p.pkgName}, &p.pkgOptions)
	if err != nil {
		return nil, errorx.IllegalState.New("failed to uninstall package: %s", err.Error())
	}

	return p.Info()
}

func (p *PackageInstaller) Upgrade() (*syspkg.PackageInfo, error) {
	pm, ok := p.pkgManager.(*apt.PackageManager)
	if !ok {
		return nil, errorx.IllegalState.New("upgrade is only supported for apt package manager")
	}

	_, err := pm.Upgrade([]string{p.pkgName}, &p.pkgOptions)
	if err != nil {
		return nil, errorx.IllegalState.New("failed to upgrade package: %s", err.Error())
	}

	return p.Info()
}

func (p *PackageInstaller) IsInstalled() bool {
	info, err := p.Info()
	if err != nil {
		return false
	}

	return info.Status == manager.PackageStatusInstalled
}

func (p *PackageInstaller) Info() (*syspkg.PackageInfo, error) {
	resp, err := p.pkgManager.ListInstalled(&p.pkgOptions)
	if err != nil {
		return nil, errorx.IllegalState.New("failed to list installed packages: %s", err.Error())
	}

	var info syspkg.PackageInfo
	var found bool
	for _, pkg := range resp {
		if pkg.Name == p.pkgName {
			info = pkg
			found = true
			break
		}
	}

	if !found {
		info, err = p.pkgManager.GetPackageInfo(p.pkgName, &p.pkgOptions)
		if err != nil {
			return nil, errorx.IllegalState.New("failed to find package: %s", err.Error())
		}
	}

	return &info, nil
}

func (p *PackageInstaller) Verify() error {
	if !p.IsInstalled() {
		return errorx.IllegalState.New("package is not installed")
	}

	return nil
}

func WithPackageName(name string) func(*PackageInstaller) {
	return func(pb *PackageInstaller) {
		pb.pkgName = name
	}
}

func WithPackageOptions(opts manager.Options) func(*PackageInstaller) {
	return func(pb *PackageInstaller) {
		pb.pkgOptions = opts
	}
}

func WithPackageManager(pm syspkg.PackageManager) func(*PackageInstaller) {
	return func(pb *PackageInstaller) {
		pb.pkgManager = pm
	}
}

func NewPackageInstaller(opts ...option) (*PackageInstaller, error) {
	p := &PackageInstaller{}

	for _, opt := range opts {
		opt(p)
	}

	if p.pkgManager == nil {
		pm, err := GetPackageManager()
		if err != nil {
			return nil, err
		}
		p.pkgManager = pm
	}

	return p, nil
}
