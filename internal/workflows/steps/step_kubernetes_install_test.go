// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/pkg/software"
)

const installStepVersion = "1.36.5"

type installStepFactory func(func(...software.InstallerOption) (software.Software, error), software.MachineRuntime) automa.Builder

var kubernetesInstallSteps = map[string]struct {
	step    installStepFactory
	extract bool
}{
	"kubelet": {installKubelet, false},
	"kubeadm": {installKubeadm, false},
	"kubectl": {installKubectl, false},
	"cri-o":   {installCrio, true},
}

// An installed component, at whatever version, is skipped without touching its
// files, and its rollback must not remove what an earlier release installed.
func TestKubernetesInstallSteps_AlreadyInstalledIsSkippedAndNotRolledBack(t *testing.T) {
	for name, tc := range kubernetesInstallSteps {
		t.Run(name, func(t *testing.T) {
			inst := software.NewMockSoftware(gomock.NewController(t))
			inst.EXPECT().GetSoftwareName().Return(name).AnyTimes()
			inst.EXPECT().Version().Return(installStepVersion).AnyTimes()
			inst.EXPECT().IsInstalled().Return(true, nil)
			// No Download, Extract, Install, Cleanup or Uninstall: gomock fails
			// the test on any unexpected call.

			step, err := tc.step(func(...software.InstallerOption) (software.Software, error) { return inst, nil }, nil).Build()
			require.NoError(t, err)

			report := step.Execute(context.Background())
			require.NoError(t, report.Error)
			assert.Equal(t, automa.StatusSkipped, report.Status)

			rollback := step.Rollback(context.Background())
			require.NoError(t, rollback.Error)
			assert.Equal(t, automa.StatusSkipped, rollback.Status)
		})
	}
}

func TestKubernetesInstallSteps_FreshInstallIsRolledBack(t *testing.T) {
	for name, tc := range kubernetesInstallSteps {
		t.Run(name, func(t *testing.T) {
			inst := software.NewMockSoftware(gomock.NewController(t))
			inst.EXPECT().GetSoftwareName().Return(name).AnyTimes()
			inst.EXPECT().Version().Return(installStepVersion).AnyTimes()
			inst.EXPECT().IsInstalled().Return(false, nil)
			inst.EXPECT().Download().Return(nil)
			if tc.extract {
				inst.EXPECT().Extract().Return(nil)
			}
			inst.EXPECT().Install().Return(nil)
			inst.EXPECT().Cleanup().Return(nil)
			inst.EXPECT().Uninstall().Return(nil)

			step, err := tc.step(func(...software.InstallerOption) (software.Software, error) { return inst, nil }, nil).Build()
			require.NoError(t, err)

			report := step.Execute(context.Background())
			require.NoError(t, report.Error)
			assert.Equal(t, automa.StatusSuccess, report.Status)

			rollback := step.Rollback(context.Background())
			require.NoError(t, rollback.Error)
			assert.Equal(t, automa.StatusSuccess, rollback.Status)
		})
	}
}
