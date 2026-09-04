// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/bluet/syspkg"
	"github.com/bluet/syspkg/manager"
	"github.com/golang/mock/gomock"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin the invariant that an optional package step can never abort
// its phase, whatever the package manager does.

// optionalPackageStub builds a step whose installer returns the given mock.
func optionalPackageStub(pkg software.Package, err error) *automa.StepBuilder {
	return InstallOptionalSystemPackage("bash-completion", func() (software.Package, error) {
		return pkg, err
	})
}

func runOptionalStep(t *testing.T, builder *automa.StepBuilder) *automa.Report {
	t.Helper()
	step, err := builder.Build()
	require.NoError(t, err)
	return step.Execute(context.Background())
}

func TestInstallOptionalSystemPackage_InstallFailureDoesNotFailTheStep(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pkg := software.NewMockPackage(ctrl)
	pkg.EXPECT().Name().Return("bash-completion").AnyTimes()
	pkg.EXPECT().IsInstalled().Return(false)
	pkg.EXPECT().Install().Return(nil, assert.AnError)

	report := runOptionalStep(t, optionalPackageStub(pkg, nil))

	assert.Equal(t, automa.StatusSkipped, report.Status)
	require.NoError(t, report.Error)
}

func TestInstallOptionalSystemPackage_UnresolvableInstallerDoesNotFailTheStep(t *testing.T) {
	report := runOptionalStep(t, optionalPackageStub(nil, assert.AnError))

	assert.Equal(t, automa.StatusSkipped, report.Status)
	require.NoError(t, report.Error)
}

func TestInstallOptionalSystemPackage_AlreadyInstalledSucceeds(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pkg := software.NewMockPackage(ctrl)
	pkg.EXPECT().Name().Return("bash-completion").AnyTimes()
	pkg.EXPECT().IsInstalled().Return(true)

	report := runOptionalStep(t, optionalPackageStub(pkg, nil))

	assert.Equal(t, automa.StatusSuccess, report.Status)
	require.NoError(t, report.Error)
	assert.Equal(t, "true", report.Metadata[AlreadyInstalled])
}

func TestInstallOptionalSystemPackage_InstallsWhenMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pkg := software.NewMockPackage(ctrl)
	pkg.EXPECT().Name().Return("bash-completion").AnyTimes()
	pkg.EXPECT().IsInstalled().Return(false)
	pkg.EXPECT().Install().Return(&syspkg.PackageInfo{
		Name:    "bash-completion",
		Version: "1:2.11-6",
		Status:  manager.PackageStatusInstalled,
	}, nil)

	report := runOptionalStep(t, optionalPackageStub(pkg, nil))

	assert.Equal(t, automa.StatusSuccess, report.Status)
	require.NoError(t, report.Error)
	assert.Equal(t, "bash-completion", report.Metadata["packageName"])
	assert.Equal(t, "1:2.11-6", report.Metadata["packageVersion"])
}
