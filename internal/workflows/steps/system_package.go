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

// InstallSystemPackage installs a system package using the provided installer function.
// The installer function should return a software.Package instance that knows how to install the package.
// If the package is already installed, it will skip the installation.
func InstallSystemPackage(name string, installer func() (software.Package, error)) automa.Builder {
	id := fmt.Sprintf("install-%s", name)
	return automa.NewStepBuilder(id, automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		pkg, err := installer()
		if err != nil {
			return nil, err
		}

		if !pkg.IsInstalled() {
			logx.As().Debug().Msgf("Installing %s...", name)

			info, err := pkg.Install()
			if err != nil {
				return nil, err
			}

			logx.As().Info().
				Str("name", info.Name).
				Str("version", info.Version).
				Str("status", string(info.Status)).
				Interface("package", info).
				Msgf("Package %q is installed successfully", info.Name)
		} else {
			logx.As().Info().Msgf("Package %q is already installed, skipping installation", name)
		}

		return automa.StepSuccessReport("install-iptables"), nil
	}))
}
