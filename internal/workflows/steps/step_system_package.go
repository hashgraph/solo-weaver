package steps

import (
	"context"
	"fmt"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"golang.hedera.com/solo-provisioner/pkg/software"
)

// RefreshSystemPackageIndex refreshes the system package index.
// Essentially this is equivalent to running `apt-get update` on Debian-based systems
func RefreshSystemPackageIndex() automa.Builder {
	return automa.NewStepBuilder("refresh-system-package-index",
		automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
			err := software.RefreshPackageIndex()
			if err != nil {
				return nil, err
			}
			logx.As().Info().Msg("Package index refreshed successfully")
			return automa.StepSuccessReport("refresh-system-package-index"), nil
		}))
}

// AutoRemoveOrphanedPackages removes orphaned dependencies and frees disk space.
// Essentially this is equivalent to running `apt autoremove -y` on Debian-based systems
func AutoRemoveOrphanedPackages() automa.Builder {
	return automa.NewStepBuilder("autoremove-orphaned-packages",
		automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
			err := software.AutoRemove()
			if err != nil {
				return nil, err
			}
			logx.As().Info().Msg("Orphaned packages removed successfully")
			return automa.StepSuccessReport("autoremove-orphaned-packages"), nil
		}))
}

// InstallSystemPackage installs a system package using the provided installer function.
// The installer function should return a software.Package instance that knows how to install the package.
// If the package is already installed, it will skip the installation.
func InstallSystemPackage(name string, installer func() (software.Package, error)) automa.Builder {
	var installedByThisStep bool
	stepId := fmt.Sprintf("install-%s", name)
	validateInstaller := func() (software.Package, error) {
		if name == "" {
			return nil, fmt.Errorf("package name cannot be empty")
		}

		if installer == nil {
			return nil, fmt.Errorf("installer function cannot be nil")
		}

		pkg, err := installer()
		if err != nil {
			return nil, err
		}

		if pkg.Name() != name {
			return nil, fmt.Errorf("installer returned package with unexpected name: got %q, want %q",
				pkg.Name(), name)
		}

		return pkg, nil
	}

	return automa.NewStepBuilder(stepId,
		automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
			pkg, err := validateInstaller()
			if err != nil {
				return nil, err
			}

			if !pkg.IsInstalled() {
				logx.As().Debug().Msgf("Installing %s...", pkg.Name())

				info, err := pkg.Install()
				if err != nil {
					return nil, err
				}

				logx.As().Info().
					Str("name", info.Name).
					Str("version", info.Version).
					Str("status", string(info.Status)).
					Interface("package", info).
					Msgf("Package %q is installed by this step successfully", pkg.Name())
				installedByThisStep = true
			} else {
				logx.As().Info().Msgf("Package %q is already installed, skipping installation", pkg.Name())
			}

			return automa.StepSuccessReport(stepId), nil
		}),
		automa.WithOnRollback(func(ctx context.Context) (*automa.Report, error) {
			pkg, err := validateInstaller()
			if err != nil {
				return nil, err
			}

			if pkg.IsInstalled() && installedByThisStep {
				// Only uninstall if it was installedByThisStep in this step
				logx.As().Debug().Msgf("Uninstalling %s...", pkg.Name())
				info, err := pkg.Uninstall()
				if err != nil {
					return nil, err
				}

				logx.As().Info().
					Str("name", info.Name).
					Str("version", info.Version).
					Str("status", string(info.Status)).
					Interface("package", info).
					Msgf("Package %q is uninstalled successfully", pkg.Name())
			} else {
				logx.As().Info().Msgf("Package %q is not installed, skipping uninstallation", pkg.Name())
			}

			return automa.StepSuccessReport(stepId), nil
		}))
}

// RemoveSystemPackage removes a system package using the provided installer function.
// The installer function should return a software.Package instance that knows how to uninstall the package.
// If the package is not installed, it will skip the removal.
func RemoveSystemPackage(name string, installer func() (software.Package, error)) automa.Builder {
	var removedByThisStep bool
	stepId := fmt.Sprintf("remove-%s", name)
	validateInstaller := func() (software.Package, error) {
		if name == "" {
			return nil, fmt.Errorf("package name cannot be empty")
		}

		if installer == nil {
			return nil, fmt.Errorf("installer function cannot be nil")
		}

		pkg, err := installer()
		if err != nil {
			return nil, err
		}

		if pkg.Name() != name {
			return nil, fmt.Errorf("installer returned package with unexpected name: got %q, want %q",
				pkg.Name(), name)
		}

		return pkg, nil
	}

	return automa.NewStepBuilder(stepId,
		automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
			pkg, err := validateInstaller()
			if err != nil {
				return nil, err
			}

			if pkg.IsInstalled() {
				logx.As().Debug().Msgf("Removing %s...", pkg.Name())

				info, err := pkg.Uninstall()
				if err != nil {
					return nil, err
				}

				logx.As().Info().
					Str("name", info.Name).
					Str("version", info.Version).
					Str("status", string(info.Status)).
					Interface("package", info).
					Msgf("Package %q is uninstalled successfully", pkg.Name())
				removedByThisStep = true
			} else {
				logx.As().Info().Msgf("Package %q is not installed, skipping removal", pkg.Name())
			}

			return automa.StepSuccessReport(stepId), nil
		}),
		automa.WithOnRollback(func(ctx context.Context) (*automa.Report, error) {
			pkg, err := validateInstaller()
			if err != nil {
				return nil, err
			}

			if !pkg.IsInstalled() && removedByThisStep {
				// Only reinstall if it was removed in this step
				logx.As().Debug().Msgf("Reinstalling %s...", pkg.Name())
				info, err := pkg.Install()
				if err != nil {
					return nil, err
				}

				logx.As().Info().
					Str("name", info.Name).
					Str("version", info.Version).
					Str("status", string(info.Status)).
					Interface("package", info).
					Msgf("Package %q is installed successfully", pkg.Name())
			} else {
				logx.As().Info().Msgf("Package %q is already installed, skipping reinstallation", pkg.Name())
			}

			return automa.StepSuccessReport(stepId), nil
		}))
}
