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

func checkClusterHealth() *automa.StepBuilder {
	return automa_steps.BashScriptStep("check-cluster-health", []string{
		"../../../test/scripts/health.sh",
	}, "")
}
