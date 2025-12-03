// SPDX-License-Identifier: Apache-2.0

package software

import (
	"sync"

	"github.com/bluet/syspkg"
	"github.com/bluet/syspkg/manager"
	"github.com/bluet/syspkg/manager/apt"
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
			initErr = NewInstallationError(err, "package-manager", "")
			return
		}

		// Let syspkg automatically detect the best available package manager
		pm, err := sysPackageManager.GetPackageManager("") // Empty string returns first available
		if err != nil {
			initErr = NewInstallationError(err, "package-manager", "")
			return
		}

		pkgManager = pm
	})

	return pkgManager, initErr
}

func RefreshPackageIndex() error {
	pm, err := GetPackageManager()
	if err != nil {
		return err
	}

	return pm.Refresh(&manager.Options{DryRun: false, Interactive: false, AssumeYes: true})
}

// AutoRemove removes orphaned dependencies to free disk space
// This is equivalent to running `apt autoremove -y` on Debian-based systems

// AutoRemover is an interface for package managers that support autoremove.
type AutoRemover interface {
	AutoRemove(opts *manager.Options) ([]manager.PackageInfo, error)
}

func AutoRemove() error {
	pm, err := GetPackageManager()
	if err != nil {
		return err
	}

	// Check if the package manager supports AutoRemove
	autoRemover, ok := pm.(AutoRemover)
	if !ok {
		return NewInstallationError(nil, "autoremove", "")
	}

	_, err = autoRemover.AutoRemove(&manager.Options{DryRun: false, Interactive: false, AssumeYes: true})
	if err != nil {
		return NewInstallationError(err, "autoremove", "")
	}

	return nil
}

type option func(*PackageInstaller)

// PackageInstaller is the default implementation of the Package interface that uses standard system package
// manager to manage a system package
type PackageInstaller struct {
	pkgName    string
	pkgOptions manager.Options
	pkgManager syspkg.PackageManager
}

func (p *PackageInstaller) Name() string {
	return p.pkgName
}

func (p *PackageInstaller) Install() (*syspkg.PackageInfo, error) {
	_, err := p.pkgManager.Install([]string{p.pkgName}, &p.pkgOptions)
	if err != nil {
		return nil, NewInstallationError(err, p.pkgName, "")
	}

	return p.Info()
}

func (p *PackageInstaller) Uninstall() (*syspkg.PackageInfo, error) {
	_, err := p.pkgManager.Delete([]string{p.pkgName}, &p.pkgOptions)
	if err != nil {
		return nil, NewInstallationError(err, p.pkgName, "")
	}

	return p.Info()
}

func (p *PackageInstaller) Upgrade() (*syspkg.PackageInfo, error) {
	pm, ok := p.pkgManager.(*apt.PackageManager)
	if !ok {
		return nil, NewInstallationError(nil, p.pkgName, "")
	}

	_, err := pm.Upgrade([]string{p.pkgName}, &p.pkgOptions)
	if err != nil {
		return nil, NewInstallationError(err, p.pkgName, "")
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
	// Instead of using ListInstalled, use Find to get more reliable results
	// as the current syspkg apt ListInstalled implementation does not check whether only the config of a package is there.
	resp, err := p.pkgManager.Find([]string{p.pkgName}, &p.pkgOptions)
	if err != nil {
		return nil, NewInstallationError(err, p.pkgName, "")
	}

	// go through the list and verify if the package is found
	for _, pkg := range resp {
		if pkg.Name == p.pkgName {
			return &pkg, nil
		}
	}

	return nil, NewInstallationError(nil, p.pkgName, "")
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
