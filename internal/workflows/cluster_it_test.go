//go:build e2e

package workflows

import (
	"context"
	"fmt"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/automa/automa_steps"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/internal/core"
	"golang.hedera.com/solo-provisioner/internal/workflows/steps"
)

// Reset returns a step that resets the Kubernetes cluster and cleans up all related resources
// This is a best effort cleanup and may not cover all edge cases
func reset() automa.Builder {
	return automa_steps.BashScriptStep("reset-all", []string{
		"helm uninstall metallb -n metallb-system || true",
		"kubectl delete namespace metallb-system || true",
		"cilium uninstall || true",
		"sudo kubeadm reset --force || true",
		"sudo rm -rf /etc/cni/net.d || true",
		"sudo systemctl stop kubelet || true",
		"sudo systemctl disable kubelet || true",
		"sudo rm -f /usr/lib/systemd/system/kubelet.service",
		"sudo systemctl stop crio || true",
		"sudo systemctl disable crio || true",
		"sudo rm -f /usr/lib/systemd/system/crio.service",
		"sudo systemctl daemon-reload || true",
		"sudo rm -f /usr/local/bin/kubeadm || true",
		"sudo rm -f /usr/local/bin/kubelet || true",
		"sudo rm -f /usr/local/bin/cilium || true",
		"sudo rm -rf $HOME/.kube || true",
		"sudo rm -f /etc/sysctl.d/*.conf || true",
		`for path in \
				  	/opt/provisioner/sandbox/var/run \
				  	/opt/provisioner/sandbox/var/lib/containers/storage/overlay; do 
				  mount | grep "$path" | awk '{print $3}' | while read mnt; do 
					echo "Unmounting $mnt" 
					sudo umount -l "$mnt" || true 
				  done 
				done`,
		"sudo umount /var/lib/kubelet || true",
		"sudo umount /var/run/cilium || true",
		"sudo umount /etc/kubernetes || true",
		"sudo swapoff -a",
		"sudo lsof -t -i :6443 | xargs -r sudo kill -9",
		fmt.Sprintf("sudo rm -rf %s || true", core.Paths().HomeDir),
	}, "")
}

func Test_NewSetupClusterWorkflow_Integration(t *testing.T) {
	rf, err := reset().Build()
	if err != nil {
		t.Fatalf("failed to build reset workflow: %v", err)
	}

	resetReport := rf.Execute(context.Background())
	require.NotNil(t, resetReport)
	require.NoError(t, resetReport.Error)

	wf, err := NewSetupClusterWorkflow("local").Build()
	if err != nil {
		t.Fatalf("failed to build workflow: %v", err)
	}

	report := wf.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)

	steps.PrintWorkflowReport(report, "")
	require.Equal(t, automa.StatusSuccess, report.Status)
}
