// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"helm.sh/helm/v3/pkg/release"
)

func TestHelmReleaseInfo_InstalledAbsent(t *testing.T) {
	tests := []struct {
		status    release.Status
		installed bool
		absent    bool
	}{
		{status: "", absent: true},
		{status: release.StatusUninstalled, absent: true},
		{status: release.StatusDeployed, installed: true},
		{status: release.StatusFailed, installed: true},
		{status: release.StatusPendingInstall},
		{status: release.StatusPendingUpgrade},
		{status: release.StatusPendingRollback},
		{status: release.StatusUninstalling},
		{status: release.StatusSuperseded},
		{status: release.StatusUnknown, absent: true},
	}
	for _, tc := range tests {
		t.Run(string(tc.status), func(t *testing.T) {
			info := HelmReleaseInfo{Status: tc.status}
			assert.Equal(t, tc.installed, info.Installed())
			assert.Equal(t, tc.absent, info.Absent())
		})
	}
}
