// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package blocknode

import (
	"testing"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
)

func releaseInfo(status release.Status) state.HelmReleaseInfo {
	return state.HelmReleaseInfo{Name: "block-node", Namespace: "block-node", Status: status}
}

func TestRequireInstalled(t *testing.T) {
	verbs := []string{"upgrade", "reconfigure", "reset", "uninstall"}
	tests := []struct {
		status release.Status
		force  bool
		// wantErr maps a verb to the expected error substring; a verb absent from
		// the map must pass.
		wantErr map[string]string
	}{
		{status: release.StatusDeployed},
		{status: release.StatusDeployed, force: true},
		{status: release.StatusFailed},
		{status: release.StatusFailed, force: true},
		{status: "", wantErr: allVerbs(verbs, "not installed")},
		{status: "", force: true},
		{status: release.StatusUnknown, wantErr: allVerbs(verbs, "not installed")},
		{status: release.StatusUnknown, force: true},
		{status: release.StatusUninstalled, wantErr: allVerbs(verbs, "not installed")},
		{status: release.StatusUninstalled, force: true},
		{status: release.StatusPendingUpgrade, wantErr: allVerbs(verbs, `"pending-upgrade"`)},
		{status: release.StatusPendingUpgrade, force: true, wantErr: allVerbs(verbs[:3], `"pending-upgrade"`)},
		{status: release.StatusPendingRollback, wantErr: allVerbs(verbs, `"pending-rollback"`)},
		{status: release.StatusPendingInstall, wantErr: allVerbs(verbs, `"pending-install"`)},
		{status: release.StatusPendingInstall, force: true, wantErr: allVerbs(verbs[:3], `"pending-install"`)},
		{status: release.StatusUninstalling, wantErr: allVerbs(verbs, `"uninstalling"`)},
		{status: release.StatusUninstalling, force: true, wantErr: allVerbs(verbs[:3], `"uninstalling"`)},
	}
	for _, tc := range tests {
		for _, verb := range verbs {
			name := string(tc.status) + "/" + verb
			if tc.force {
				name += "/force"
			}
			t.Run(name, func(t *testing.T) {
				err := requireInstalled(releaseInfo(tc.status), verb, tc.force)
				want, ok := tc.wantErr[verb]
				if !ok {
					require.NoError(t, err)
					return
				}
				require.Error(t, err)
				assert.Contains(t, err.Error(), want)
				assert.Contains(t, err.Error(), "cannot "+verb)
			})
		}
	}
}

func allVerbs(verbs []string, msg string) map[string]string {
	m := make(map[string]string, len(verbs))
	for _, v := range verbs {
		m[v] = msg
	}
	return m
}

func TestStuckReleaseError_Hints(t *testing.T) {
	tests := []struct {
		status release.Status
		hint   string
	}{
		{status: release.StatusPendingUpgrade, hint: "helm rollback block-node <revision> -n block-node"},
		{status: release.StatusPendingRollback, hint: "helm rollback block-node <revision> -n block-node"},
		{status: release.StatusPendingInstall, hint: "solo-provisioner block node uninstall --force"},
		{status: release.StatusUninstalling, hint: "solo-provisioner block node uninstall --force"},
	}
	for _, tc := range tests {
		t.Run(tc.status.String(), func(t *testing.T) {
			err := stuckReleaseError(releaseInfo(tc.status), "upgrade")
			assertResolutionMentions(t, err, tc.hint)
			raw, _ := errorx.ExtractProperty(err, models.ErrPropertyResolution)
			assert.NotContains(t, raw, "block node install")
		})
	}
}
