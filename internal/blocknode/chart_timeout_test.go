// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
)

// managerWithHelmMock builds a Manager wired to a mocked helm.Manager. Only the
// helm manager, logger and inputs are needed for InstallChart/UpgradeChart.
func managerWithHelmMock(t *testing.T, inputs models.BlockNodeInputs) (*Manager, *helm.MockManager) {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	hm := helm.NewMockManager(ctrl)
	m := &Manager{
		helmManager:     hm,
		logger:          testLogger(),
		blockNodeInputs: inputs,
	}
	return m, hm
}

func TestInstallChart_TimeoutPropagation(t *testing.T) {
	tests := []struct {
		name     string
		timeout  time.Duration
		expected time.Duration
	}{
		{name: "explicit timeout is honored", timeout: 10 * time.Minute, expected: 10 * time.Minute},
		{name: "zero falls back to default", timeout: 0, expected: helm.DefaultTimeout},
		{name: "negative falls back to default", timeout: -1, expected: helm.DefaultTimeout},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inputs := models.BlockNodeInputs{Release: "bn", Namespace: "ns", Timeout: tc.timeout}
			m, hm := managerWithHelmMock(t, inputs)

			hm.EXPECT().IsInstalled("bn", "ns").Return(false, nil)
			hm.EXPECT().
				InstallChart(gomock.Any(), "bn", gomock.Any(), gomock.Any(), "ns", gomock.Any()).
				DoAndReturn(func(_ context.Context, _, _, _, _ string, o helm.InstallChartOptions) (*release.Release, error) {
					assert.Equal(t, tc.expected, o.Timeout)
					return nil, nil
				})

			installed, err := m.InstallChart(context.Background(), "values.yaml")
			require.NoError(t, err)
			assert.True(t, installed)
		})
	}
}

func TestUpgradeChart_TimeoutPropagation(t *testing.T) {
	tests := []struct {
		name     string
		timeout  time.Duration
		expected time.Duration
	}{
		{name: "explicit timeout is honored", timeout: 15 * time.Minute, expected: 15 * time.Minute},
		{name: "zero falls back to default", timeout: 0, expected: helm.DefaultTimeout},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inputs := models.BlockNodeInputs{Release: "bn", Namespace: "ns", Timeout: tc.timeout}
			m, hm := managerWithHelmMock(t, inputs)

			hm.EXPECT().IsInstalled("bn", "ns").Return(true, nil)
			hm.EXPECT().
				UpgradeChart(gomock.Any(), "bn", gomock.Any(), gomock.Any(), "ns", gomock.Any()).
				DoAndReturn(func(_ context.Context, _, _, _, _ string, o helm.UpgradeChartOptions) (*release.Release, error) {
					assert.Equal(t, tc.expected, o.Timeout)
					return nil, nil
				})

			err := m.UpgradeChart(context.Background(), "values.yaml", false)
			require.NoError(t, err)
		})
	}
}

func TestIsHelmTimeoutError(t *testing.T) {
	// Mirrors the real chain: errorx wrapping the helm SDK's atomic-rollback text.
	atomicTimeout := errorx.IllegalState.Wrap(
		fmt.Errorf("release block-node failed, and has been uninstalled due to atomic being set: context deadline exceeded"),
		"failed to install block node chart")

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "wrapped context.DeadlineExceeded", err: errorx.Decorate(context.DeadlineExceeded, "wait"), want: true},
		{name: "atomic rollback deadline text", err: atomicTimeout, want: true},
		{name: "kube wait timeout text", err: fmt.Errorf("timed out waiting for the condition"), want: true},
		{name: "unrelated failure", err: errorx.IllegalState.New("ImagePullBackOff"), want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsHelmTimeoutError(tc.err))
		})
	}
}
