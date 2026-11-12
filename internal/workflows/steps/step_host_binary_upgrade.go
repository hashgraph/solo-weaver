// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"slices"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/joomcode/errorx"

	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// HostBinaryAt reports whether the installed binaries of name match version.
// It hashes the binaries, so it does not trust the recorded version.
func HostBinaryAt(name, version string) (bool, error) {
	found, err := identifyInstalledVersions(name)
	if err != nil {
		return false, err
	}
	return slices.Contains(found, version), nil
}

// UpgradeHostBinary installs version of a host artifact over whatever version
// is installed. The install steps skip anything already installed, whatever
// its version; this step compares the binaries with version instead. Set
// archived when the catalog ships the binaries inside an archive, so they are
// unpacked before install. A service that runs the binary must be stopped by
// the caller first.
func UpgradeHostBinary(name, version string, archived bool, provider func(opts ...software.InstallerOption) (software.Software, error)) automa.Builder {
	return automa.NewStepBuilder().WithId("upgrade-" + name).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepDetail(ctx, stp, fmt.Sprintf("upgrading %s to %s...", name, version))
			return ctx, nil
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			at, err := HostBinaryAt(name, version)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.IntegrityCheckFailed,
					fmt.Sprintf("Check the %s binaries in the sandbox bin directory", name))))
			}
			if at {
				return automa.SkippedReport(stp, automa.WithDetail(fmt.Sprintf("%s is already at %s", name, version)))
			}

			installer, err := provider(software.WithVersion(version))
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.Internal)))
			}
			phases := []func() error{installer.Download}
			if archived {
				phases = append(phases, installer.Extract)
			}
			phases = append(phases, installer.Install, installer.Configure, installer.Cleanup)
			for _, do := range phases {
				if err := do(); err != nil {
					return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.DownloadFailed,
						fmt.Sprintf("Check that the host can download %s %s, then re-run the upgrade", name, version))))
				}
			}

			at, err = HostBinaryAt(name, version)
			if err == nil && !at {
				err = errorx.IllegalState.New("the installed %s binaries do not match %s after the upgrade", name, version)
			}
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.IntegrityCheckFailed,
					fmt.Sprintf("Re-run the upgrade; if it fails again, reinstall %s", name))))
			}
			return automa.SuccessReport(stp, automa.WithDetail(fmt.Sprintf("%s upgraded to %s", name, version)))
		})
}
