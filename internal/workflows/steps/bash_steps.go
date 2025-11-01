package steps

import (
	"fmt"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/automa/automa_steps"
	"github.com/automa-saga/logx"
	"golang.hedera.com/solo-provisioner/internal/core"
)

// TODO: Move these constants to the appropriate step files once the implementation is done
const (
	InstallMetalLBStepId      = "install-metallb"
	DeployMetallbConfigStepId = "deploy-metallb-config"
)

func installMetalLB(version string) *automa.StepBuilder {
	return automa_steps.BashScriptStep(InstallMetalLBStepId, []string{
		fmt.Sprintf("sudo %s/helm repo add metallb https://metallb.github.io/metallb", core.Paths().SandboxBinDir),
		fmt.Sprintf("sudo %s/helm install metallb metallb/metallb --version %s \\\n"+
			"--set speaker.frr.enabled=false \\\n"+
			"--namespace metallb-system --create-namespace --atomic --wait",
			core.Paths().SandboxBinDir, version),
		"sleep 60",
	}, "")
}

func configureMetalLB() *automa.StepBuilder {
	machineIp, err := runCmd(`ip route get 1 | head -1 | sed 's/^.*src \(.*\)$/\1/' | awk '{print $1}'`)
	if err != nil {
		machineIp = "0.0.0.0"
		logx.As().Warn().Err(err).Str("machine_ip", machineIp).
			Msg("failed to get machine IP, defaulting to 0.0.0.0")
	}

	configScript := fmt.Sprintf(
		`set -eo pipefail; cat <<EOF | %s/kubectl apply -f - 
---
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: private-address-pool
  namespace: metallb-system
spec:
  addresses:
    - 192.168.99.0/24
---
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: public-address-pool
  namespace: metallb-system
spec:
  addresses:
    - %s/32
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: primary-l2-advertisement
  namespace: metallb-system
spec:
  ipAddressPools:
    - private-address-pool
    - public-address-pool
EOF`, core.Paths().SandboxBinDir, machineIp)
	return automa_steps.BashScriptStep(DeployMetallbConfigStepId, []string{
		configScript,
	}, "")
}

// CheckClusterHealth performs a series of checks to ensure the Kubernetes cluster is healthy
// This is a basic smoke tests to verify the cluster setup and can be extended as needed
func checkClusterHealth() *automa.StepBuilder {
	script := `
set -e

# Check if kubectl can access the cluster
kubectl get nodes

# Check all nodes are Ready
kubectl get nodes --no-headers | awk '{print $2}' | grep -q '^Ready$'
if [ $? -eq 0 ]; then exit 1; fi

# List of required namespaces
namespaces="cilium-secrets default kube-node-lease kube-public kube-system metallb-system"

for ns in $namespaces; do
  status=$(kubectl get namespace $ns --no-headers 2>/dev/null | awk '{print $2}')
  if [ "$status" != "Active" ]; then
    echo "Namespace $ns is not Active or does not exist (status: $status)"
    exit 1
  fi
done

# List of pod name prefixes to check in kube-system
prefixes="cilium- cilium-operator- coredns- etcd- hubble-relay- kube-apiserver- kube-controller-manager- kube-scheduler- metallb-controller- metallb-speaker-"

for prefix in $prefixes; do
  kubectl get pods -n kube-system --no-headers | awk -v p="$prefix" '$1 ~ "^"p {print $1, $2}' | while read name ready; do
    if ! [[ "$ready" =~ ^([0-9]+)/\1$ ]]; then
      echo "Pod $name is not fully ready ($ready)"
      exit 1
    fi
  done
done

# List of required services in the format namespace:service
services="default:kubernetes kube-system:hubble-peer kube-system:hubble-relay kube-system:kube-dns metallb-system:metallb-webhook-service"

for svc in $services; do
  ns=$(echo $svc | cut -d: -f1)
  name=$(echo $svc | cut -d: -f2)
  if ! kubectl get svc -n "$ns" "$name" --no-headers 2>/dev/null | grep -q .; then
    echo "Service $name not found in namespace $ns"
    exit 1
  fi
done

# List of required CRDs
crds="bfdprofiles.metallb.io bgpadvertisements.metallb.io bgppeers.metallb.io ciliumcidrgroups.cilium.io ciliumclusterwidenetworkpolicies.cilium.io ciliumendpoints.cilium.io ciliumidentities.cilium.io ciliuml2announcementpolicies.cilium.io ciliumloadbalancerippools.cilium.io ciliumnetworkpolicies.cilium.io ciliumnodeconfigs.cilium.io ciliumnodes.cilium.io ciliumpodippools.cilium.io communities.metallb.io ipaddresspools.metallb.io l2advertisements.metallb.io servicebgpstatuses.metallb.io servicel2statuses.metallb.io"

for crd in $crds; do
  if ! kubectl get crd "$crd" --no-headers 2>/dev/null | grep -q .; then
    echo "CRD $crd not found"
    exit 1
  fi
done

echo "Cluster health check passed"
`
	return automa_steps.BashScriptStep("check-cluster-health", []string{
		script,
	}, "")
}
