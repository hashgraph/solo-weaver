// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"slices"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"

	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// HostBinaryAt reports whether the installed binaries of name match version.
// It hashes the binaries, so it does not trust the recorded version.
// Missing binaries are not at version: an install that stopped part way left
// them out.
func HostBinaryAt(name, version string) (bool, error) {
	found, err := identifyInstalledVersions(name)
	if errorx.IsOfType(err, software.FileNotFoundError) {
		return false, nil
	}
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
	return upgradeHostBinary(name, version, archived, provider, nil)
}

// moveAside moves the installed release out of the way before an install. It
// returns restore, which puts it back after a failed install, and settle, run
// after one that worked.
type moveAside func() (restore, settle func() error, err error)

// upgradeHostBinary is UpgradeHostBinary with aside run between the unpack and
// the install, once the new release is on disk.
func upgradeHostBinary(name, version string, archived bool, provider func(opts ...software.InstallerOption) (software.Software, error), aside moveAside) automa.Builder {
	return automa.NewStepBuilder().WithId("upgrade-" + name).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepDetail(ctx, stp, fmt.Sprintf("upgrading %s to %s...", name, version))
			return ctx, nil
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			fail := func(err error, reason errx.Reason, hint string) *automa.Report {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reason, hint)))
			}
			sandboxHint := fmt.Sprintf("Check the %s files under %s, then re-run the upgrade", name, models.Paths().SandboxDir)

			at, err := HostBinaryAt(name, version)
			if err != nil {
				return fail(err, reasons.IntegrityCheckFailed, fmt.Sprintf("Check the %s binaries in the sandbox bin directory", name))
			}
			if at {
				return automa.SkippedReport(stp, automa.WithDetail(fmt.Sprintf("%s is already at %s", name, version)))
			}

			installer, err := provider(software.WithVersion(version))
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.Internal)))
			}
			fetch := []func() error{installer.Download}
			if archived {
				fetch = append(fetch, installer.Extract)
			}
			for _, do := range fetch {
				if err := do(); err != nil {
					return fail(err, reasons.DownloadFailed, fmt.Sprintf("Check that the host can download %s %s, then re-run the upgrade", name, version))
				}
			}

			var restore, settle func() error
			if aside != nil {
				if restore, settle, err = aside(); err != nil {
					return fail(err, ReasonHostCommandFailed, sandboxHint)
				}
			}
			// A failed install puts the previous release back, so the service
			// that runs it can start again.
			undo := func(err error) error {
				if restore == nil {
					return err
				}
				if rerr := restore(); rerr != nil {
					logx.As().Warn().Err(rerr).Str("software", name).Msg("Could not put the previous release back")
					return errorx.DecorateMany("the previous release could not be put back either", err, rerr)
				}
				logx.As().Info().Str("software", name).Msg("Put the previous release back after the failed install")
				return err
			}
			for _, do := range []func() error{installer.Install, installer.Configure} {
				if err := do(); err != nil {
					return fail(undo(err), ReasonHostCommandFailed, sandboxHint)
				}
			}
			if err := installer.Cleanup(); err != nil {
				return fail(err, ReasonHostCommandFailed, sandboxHint)
			}

			at, err = HostBinaryAt(name, version)
			if err == nil && !at {
				err = errorx.IllegalState.New("the installed %s binaries do not match %s after the upgrade", name, version)
			}
			if err != nil {
				return fail(undo(err), reasons.IntegrityCheckFailed, fmt.Sprintf("Re-run the upgrade; if it fails again, reinstall %s", name))
			}
			if settle != nil {
				if err := settle(); err != nil {
					logx.As().Warn().Err(err).Str("software", name).Msg("Could not tidy the backup of the previous release")
				}
			}
			return automa.SuccessReport(stp, automa.WithDetail(fmt.Sprintf("%s upgraded to %s", name, version)))
		})
}
