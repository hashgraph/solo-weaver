// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"

	"github.com/bluet/syspkg"
	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-weaver/internal/workflows/notify"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"golang.hedera.com/solo-weaver/pkg/software"
)

const (
	refreshSystemPackageStepId       = "refresh-system-package-index"
	autoRemoveOrphanedPackagesStepId = "autoremove-orphaned-packages"
)

func validateInstaller(name string, installer func() (software.Package, error)) (software.Package, error) {
	if name == "" {
		return nil, errorx.IllegalArgument.New("package name cannot be empty")
	}

	if installer == nil {
		return nil, errorx.IllegalArgument.New("installer function cannot be nil")
	}

	pkg, err := installer()
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "failed to get package from installer")
	}

	if pkg.Name() != name {
		return nil, errorx.IllegalArgument.New("installer returned package with unexpected name: got %q, want %q",
			pkg.Name(), name)
	}

	return pkg, nil
}

// RefreshSystemPackageIndex refreshes the system package index.
// Essentially this is equivalent to running `apt-get update` on Debian-based systems
func RefreshSystemPackageIndex() automa.Builder {
	return automa.NewStepBuilder().
		WithId(refreshSystemPackageStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			err := software.RefreshPackageIndex()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			return automa.SuccessReport(stp)
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, report *automa.Report) {
			notify.As().StepCompletion(ctx, stp, report, "Package index refreshed successfully")
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, report *automa.Report) {
			notify.As().StepFailure(ctx, stp, report, "Failed to refresh package index")
		})
}

// AutoRemoveOrphanedPackages removes orphaned dependencies and frees disk space.
// Essentially this is equivalent to running `apt autoremove -y` on Debian-based systems
func AutoRemoveOrphanedPackages() automa.Builder {
	return automa.NewStepBuilder().
		WithId(autoRemoveOrphanedPackagesStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			err := software.AutoRemove()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}
			return automa.SuccessReport(stp)
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Removing orphaned packages")
			return ctx, nil

		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, report *automa.Report) {
			notify.As().StepCompletion(ctx, stp, report, "Orphaned packages removed successfully")
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, report *automa.Report) {
			notify.As().StepFailure(ctx, stp, report, "Failed to remove orphaned packages")
		})
}

// InstallSystemPackage installs a system package using the provided installer function.
// The installer function should return a software.Package instance that knows how to install the package.
// If the package is already installed, it will skip the installation.
func InstallSystemPackage(name string, installer func() (software.Package, error)) automa.Builder {
	var installedByThisStep bool
	stepId := fmt.Sprintf("install-%s", name)

	return automa.NewStepBuilder().
		WithId(stepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			pkg, err := validateInstaller(name, installer)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			var info *syspkg.PackageInfo
			if !pkg.IsInstalled() {
				logx.As().Debug().Msgf("Installing %s...", pkg.Name())

				info, err = pkg.Install()
				if err != nil {
					return automa.FailureReport(stp, automa.WithError(err))
				}

				logx.As().Info().
					Str("name", info.Name).
					Str("version", info.Version).
					Str("status", string(info.Status)).
					Interface("package", info).
					Msgf("Package %q is installed by this step successfully", pkg.Name())
				installedByThisStep = true
			} else {
				info, err = pkg.Info()
				if err != nil {
					return automa.FailureReport(stp,
						automa.WithError(errorx.IllegalState.Wrap(err, "failed to get package info")))
				}

				logx.As().Info().Msgf("Package %q is already installed, skipping installation", pkg.Name())
			}

			return automa.SuccessReport(stp, automa.WithMetadata(map[string]string{
				"packageName":          info.Name,
				"packageVersion":       info.Version,
				"packageStatus":        string(info.Status),
				"packageManager":       info.PackageManager,
				"packageArch":          info.Arch,
				"packageCategory":      info.Category,
				"packageLatestVersion": info.NewVersion,
			}))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			pkg, err := validateInstaller(name, installer)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			if pkg.IsInstalled() && installedByThisStep {
				// Only uninstall if it was installedByThisStep in this step
				logx.As().Debug().Msgf("Uninstalling %s...", pkg.Name())
				info, err := pkg.Uninstall()
				if err != nil {
					return automa.FailureReport(stp, automa.WithError(err))
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

			return automa.SuccessReport(stp)
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Installing package %q", name)
			return ctx, nil

		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, report *automa.Report) {
			notify.As().StepCompletion(ctx, stp, report,
				"Package %q installation step completed successfully", name)
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, report *automa.Report) {
			notify.As().StepFailure(ctx, stp, report,
				"Package %q installation step failed", name)
		})
}

// RemoveSystemPackage removes a system package using the provided installer function.
// The installer function should return a software.Package instance that knows how to uninstall the package.
// If the package is not installed, it will skip the removal.
func RemoveSystemPackage(name string, installer func() (software.Package, error)) automa.Builder {
	var removedByThisStep bool
	stepId := fmt.Sprintf("remove-%s", name)
	return automa.NewStepBuilder().
		WithId(stepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			pkg, err := validateInstaller(name, installer)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			if pkg.IsInstalled() {
				logx.As().Debug().Msgf("Removing %s...", pkg.Name())

				info, err := pkg.Uninstall()
				if err != nil {
					return automa.FailureReport(stp, automa.WithError(err))
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

			return automa.SuccessReport(stp)
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			pkg, err := validateInstaller(name, installer)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			if !pkg.IsInstalled() && removedByThisStep {
				// Only reinstall if it was removed in this step
				logx.As().Debug().Msgf("Reinstalling %s...", pkg.Name())
				info, err := pkg.Install()
				if err != nil {
					return automa.FailureReport(stp,
						automa.WithError(automa.StepExecutionError.Wrap(err, "failed to reinstall package")))
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

			return automa.SuccessReport(stp)
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, report *automa.Report) {
			notify.As().StepCompletion(ctx, stp, report,
				"Package %q removal step completed successfully", name)
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, report *automa.Report) {
			notify.As().StepFailure(ctx, stp, report,
				"Package %q removal step failed: %v", name, report.Error)
		})
}
