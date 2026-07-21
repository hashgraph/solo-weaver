// SPDX-License-Identifier: Apache-2.0

package software

import (
	"path"
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/require"
)

// Test_kubeletCrioOrderingDropIn pins the kubelet drop-in that orders kubelet after
// cri-o, so cAdvisor reaches a live cri-o socket at startup and can register the
// "crio-images" imagefs label (issue #22).
func Test_kubeletCrioOrderingDropIn(t *testing.T) {
	require.Contains(t, kubeletCrioOrderingDropIn, "[Unit]")
	require.Contains(t, kubeletCrioOrderingDropIn, "After=crio.service")
	require.Contains(t, kubeletCrioOrderingDropIn, "Wants=crio.service")
}

// Test_kubeletCrioOrderingPaths pins the sandbox and host locations of the ordering drop-in.
func Test_kubeletCrioOrderingPaths(t *testing.T) {
	require.Equal(t, "10-crio-ordering.conf", kubeletCrioOrderingFileName)
	require.Equal(t, "kubelet.service.d", kubeletServiceDropInDirName)

	installer, err := NewKubeletInstaller()
	require.NoError(t, err)

	ki, ok := installer.(*kubeletInstaller)
	require.True(t, ok, "expected *kubeletInstaller")

	require.Equal(t,
		path.Join(models.Paths().SandboxDir, models.SystemdUnitFilesDir, kubeletServiceDropInDirName),
		ki.getKubeletDropInDir())
	require.Equal(t,
		path.Join(ki.getKubeletDropInDir(), kubeletCrioOrderingFileName),
		ki.getKubeletCrioOrderingSandboxPath())
	require.Equal(t,
		path.Join(models.SystemdUnitFilesDir, kubeletServiceDropInDirName, kubeletCrioOrderingFileName),
		ki.getKubeletCrioOrderingSystemPath())
}
