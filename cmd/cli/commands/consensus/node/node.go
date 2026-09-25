// SPDX-License-Identifier: Apache-2.0

package node

import (
	"strconv"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/spf13/cobra"
)

var (
	flagNamespace        string
	flagNodeId           int64
	flagAccountId        string
	flagWeight           int
	flagLedgerId         string
	flagChainId          string
	flagImageRepo        string
	flagImageTag         string
	flagUCImageRepo      string
	flagUCImageTag       string
	flagDeploymentPkgDir string
	flagGrpcTlsSecret    string
	flagSigningSecret    string
	flagImagePullSecret  string
	flagProfile          string
	flagContainerName    string
	flagJavaHeapMin      string
	flagJavaHeapMax      string
	flagJavaOpts         string
	flagCPULimit         string
	flagCPURequest       string
	flagMemoryLimit      string
	flagMemoryRequest    string

	// Volume backing configuration.
	flagDefaultVolumeType      string
	flagDefaultPVCSize         string
	flagDefaultPVCStorageClass string
	flagDefaultPVCAccessMode   string
	flagVolumes                []string
	flagVolumesFile            string
	flagHostPathUID            int
	flagHostPathGID            int

	nodeCmd = &cobra.Command{
		Use:   "node",
		Short: "Manage consensus node lifecycle",
		Long:  "Deploy and manage Hedera consensus nodes via the solo-operator's ConsensusCapsule CRD",
		RunE:  common.DefaultRunE,
	}
)

func init() {
	nodeCmd.PersistentFlags().StringVar(&flagNamespace, "namespace", models.ConsensusDefaultNamespace, "Kubernetes namespace (also used as the Orbit CR name). Deploy multiple networks in one cluster by using a distinct namespace per orbit (hiero-network-1, hiero-network-2, ...)")
	nodeCmd.PersistentFlags().Int64Var(&flagNodeId, "node-id", 0, "Consensus node ID (0-based)")
	nodeCmd.PersistentFlags().StringVar(&flagAccountId, "account-id", "0.0.3", "Node account ID (e.g. 0.0.3)")
	nodeCmd.PersistentFlags().IntVar(&flagWeight, "weight", 500, "Consensus weight for this node")
	nodeCmd.PersistentFlags().StringVar(&flagLedgerId, "ledger-id", "", "Hex ledger identity (e.g. 0x00 for mainnet, 0x01 for local/dev) — extracted from deployment package if not set")
	nodeCmd.PersistentFlags().StringVar(&flagChainId, "chain-id", "", "Decimal EVM chain ID (e.g. 295 for mainnet, 298 for local/dev) — extracted from deployment package if not set")
	nodeCmd.PersistentFlags().StringVar(&flagImageRepo, "image-repo", "", "Consensus node container image repository — extracted from deployment package if not set")
	nodeCmd.PersistentFlags().StringVar(&flagImageTag, "image-tag", "", "Consensus node container image tag — extracted from deployment package if not set")
	nodeCmd.PersistentFlags().StringVar(&flagUCImageRepo, "uc-image-repo", models.ConsensusDefaultUCImageRepo, "UC (Update Coordinator) sidecar image repository (the operator's own image; no operator default)")
	nodeCmd.PersistentFlags().StringVar(&flagUCImageTag, "uc-image-tag", models.ConsensusDefaultUCImageTag, "UC sidecar image tag (tracks the solo-operator version)")
	nodeCmd.PersistentFlags().StringVar(&flagDeploymentPkgDir, "deployment-package-dir", "", "Path to extracted HIP-1494 deployment package — config files at well-known paths override embedded defaults")
	nodeCmd.PersistentFlags().StringVar(&flagGrpcTlsSecret, "grpc-tls-secret", "", "Name of K8s Secret containing gRPC TLS key/cert (keys: hedera-node<N>.key, hedera-node<N>.crt)")
	nodeCmd.PersistentFlags().StringVar(&flagSigningSecret, "signing-secret", "", "Name of K8s Secret containing gossip signing key/cert (keys: private.pem, public.pem)")
	nodeCmd.PersistentFlags().StringVar(&flagImagePullSecret, "image-pull-secret", models.ConsensusDefaultImagePullSecret, "Name of a docker-registry Secret in the namespace used to pull private consensus/UC images; the operator threads it onto the pods. Empty to disable (public images)")

	// Consensus-node container sizing + JVM tuning. Defaults are a working baseline;
	// the explicit Java heap is required or the node stalls on startup.
	nodeCmd.PersistentFlags().StringVar(&flagContainerName, "container-name", models.ConsensusDefaultContainerName, "Consensus-node container name")
	nodeCmd.PersistentFlags().StringVar(&flagJavaHeapMin, "java-heap-min", models.ConsensusDefaultJavaHeapMin, "JVM minimum heap (-Xms) for the consensus node")
	nodeCmd.PersistentFlags().StringVar(&flagJavaHeapMax, "java-heap-max", models.ConsensusDefaultJavaHeapMax, "JVM maximum heap (-Xmx) for the consensus node")
	nodeCmd.PersistentFlags().StringVar(&flagJavaOpts, "java-opts", models.ConsensusDefaultJavaOpts, "Additional JVM options for the consensus node")
	nodeCmd.PersistentFlags().StringVar(&flagCPULimit, "cpu-limit", models.ConsensusDefaultCPULimit, "Consensus-node CPU limit (Kubernetes quantity, e.g. 2)")
	nodeCmd.PersistentFlags().StringVar(&flagCPURequest, "cpu-request", models.ConsensusDefaultCPURequest, "Consensus-node CPU request (e.g. 250m)")
	nodeCmd.PersistentFlags().StringVar(&flagMemoryLimit, "memory-limit", models.ConsensusDefaultMemoryLimit, "Consensus-node memory limit (e.g. 5Gi)")
	nodeCmd.PersistentFlags().StringVar(&flagMemoryRequest, "memory-request", models.ConsensusDefaultMemoryRequest, "Consensus-node memory request (e.g. 1Gi)")

	// Volume backing. Each data volume (upgrade/logs/stats/saved/state/blocks/records/
	// events) can be emptyDir, hostPath, or a PVC. --default-* set fallbacks; --volume
	// overrides one volume; --volumes-file supplies the same as YAML (flags win).
	nodeCmd.PersistentFlags().StringVar(&flagDefaultVolumeType, "default-volume-type", "", "Default backing for all data volumes: emptydir|hostpath|pvc (empty = emptydir)")
	nodeCmd.PersistentFlags().StringVar(&flagDefaultPVCSize, "default-pvc-size", "", "Default PVC size for pvc-backed volumes when size= is omitted (empty = per-volume default)")
	nodeCmd.PersistentFlags().StringVar(&flagDefaultPVCStorageClass, "default-pvc-storage-class", "", "Default StorageClass for pvc-backed volumes (empty = cluster default)")
	nodeCmd.PersistentFlags().StringVar(&flagDefaultPVCAccessMode, "default-pvc-access-mode", "", "Default access mode for pvc-backed volumes (empty = ReadWriteOnce)")
	nodeCmd.PersistentFlags().StringArrayVar(&flagVolumes, "volume", nil, "Per-volume backing override, repeatable: name=<vol>[,type=,path=,size=,storageClass=,accessMode=] (e.g. name=saved,size=500Gi,storageClass=fast-ssd)")
	nodeCmd.PersistentFlags().StringVar(&flagVolumesFile, "volumes-file", "", "YAML file of volume backings (defaults: + volumes:). CLI flags override the file")
	// hostPath dirs are chowned to the canonical hedera user/group (pkg/config, the
	// single source of "2000"). Hedera*Id() are strings; parse to int for IntVar (the
	// value is the trusted "2000" constant, so the error can't fire).
	hederaUID, _ := strconv.Atoi(config.HederaUserId())
	hederaGID, _ := strconv.Atoi(config.HederaGroupId())
	nodeCmd.PersistentFlags().IntVar(&flagHostPathUID, "hostpath-uid", hederaUID, "Owner UID for hostPath volume dirs (the hedera user the pod runs as)")
	nodeCmd.PersistentFlags().IntVar(&flagHostPathGID, "hostpath-gid", hederaGID, "Owner GID for hostPath volume dirs (the hedera group the pod runs as)")

	// flagProfile is a binding target for Cobra; the install command reads the value via FlagProfile().Value()
	common.FlagProfile().SetVarP(nodeCmd, &flagProfile, false)
	nodeCmd.AddCommand(installCmd)
}

// GetCmd returns the consensus node command group.
func GetCmd() *cobra.Command {
	return nodeCmd
}
