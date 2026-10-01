// SPDX-License-Identifier: Apache-2.0

package models

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hashgraph/solo-weaver/pkg/deps"
	"github.com/joomcode/errorx"
)

// Consensus-node container defaults. Used as flag defaults and as the fallback
// when an input field is left empty. The Java heap must be set explicitly; without
// it the JVM defaults MaxHeapSize to 25% of the memory limit, which stalls the node
// on startup. The memory limit must cover heap + direct memory PLUS the JVM's native
// footprint (metaspace, thread stacks, GC/JIT code cache, netty buffers), which a
// consensus node consumes heavily — a 1g-heap/512m-direct node still exceeds a 2Gi
// limit once warmed up and gets OOMKilled. These defaults are a single/local-node
// baseline (2g heap + 512m direct + native fit under 4Gi); production networks
// should raise them via flags.
const (
	// ConsensusDefaultNamespace is the default namespace / Orbit name for a
	// consensus deployment (sourced from pkg/deps alongside the other install-plan
	// defaults). Deploy multiple networks by using a distinct namespace per orbit.
	ConsensusDefaultNamespace = deps.CONSENSUS_NODE_NAMESPACE

	ConsensusDefaultContainerName = "consensus-node"
	ConsensusDefaultJavaHeapMin   = "512m"
	ConsensusDefaultJavaHeapMax   = "2g"
	ConsensusDefaultJavaOpts      = "-XX:+UseG1GC -XX:MaxDirectMemorySize=512m --add-opens java.base/jdk.internal.misc=ALL-UNNAMED --add-opens java.base/java.nio=ALL-UNNAMED -Dio.netty.tryReflectionSetAccessible=true"
	ConsensusDefaultCPULimit      = "2"
	ConsensusDefaultCPURequest    = "250m"
	ConsensusDefaultMemoryLimit   = "4Gi"
	ConsensusDefaultMemoryRequest = "2Gi"

	// UC (Update Coordinator) sidecar image. The operator provides NO built-in
	// default for it, so the capsule must declare it explicitly or the operator
	// rejects the capsule with ImageRequired. It is the
	// operator's own image, so the tag tracks the operator/chart version. Sourced
	// from pkg/deps alongside the block-node install-plan defaults.
	ConsensusDefaultUCImageRepo = deps.CONSENSUS_NODE_UC_IMAGE
	ConsensusDefaultUCImageTag  = deps.CONSENSUS_NODE_UC_VERSION

	// ConsensusDefaultImagePullSecret is the name of the image-pull secret the
	// operator threads onto the consensus-node and UC containers so they can pull
	// the private consensus/UC images (e.g. from ghcr.io). It must name a
	// docker-registry secret that exists in the node's namespace; create it with
	// `task uat:secrets`. Empty disables it (public images only).
	ConsensusDefaultImagePullSecret = deps.CONSENSUS_NODE_IMAGE_PULL_SECRET
)

// ImageRepositoryRef is one candidate registry for a container image.
type ImageRepositoryRef struct {
	Repository string `json:"repository"`
	ImageName  string `json:"imageName"`
	ImageTag   string `json:"imageTag"`
}

// PullSecretSelector picks a pull-secret name by registry host. ByHost holds
// per-host names; Default is the fallback. HasDefault tells an empty default
// (public pull) apart from no default at all.
type PullSecretSelector struct {
	Default    string            `json:"default,omitempty"`
	HasDefault bool              `json:"hasDefault,omitempty"`
	ByHost     map[string]string `json:"byHost,omitempty"`
}

// SecretForHost picks the secret for a host: the ByHost entry first, then the
// Default, else "" (public pull).
func (s PullSecretSelector) SecretForHost(host string) string {
	if name, ok := s.ByHost[host]; ok {
		return name
	}
	if s.HasDefault {
		return s.Default
	}
	return ""
}

// Hosts returns the ByHost keys, sorted for stable error messages.
func (s PullSecretSelector) Hosts() []string {
	hosts := make([]string, 0, len(s.ByHost))
	for h := range s.ByHost {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	return hosts
}

// ParsePullSecretSelector reads repeatable --image-pull-secret values. Each is
// a bare NAME (default for all registries) or HOST=NAME (one host). Only one
// bare NAME is allowed, and each host may appear once.
func ParsePullSecretSelector(values []string) (PullSecretSelector, error) {
	var sel PullSecretSelector
	for _, v := range values {
		host, name, keyed := strings.Cut(v, "=")
		if !keyed {
			if sel.HasDefault {
				return PullSecretSelector{}, errorx.IllegalArgument.New(
					"multiple default --image-pull-secret values (%q and %q); only one bare NAME is allowed", sel.Default, v)
			}
			sel.Default, sel.HasDefault = v, true
			continue
		}
		host = strings.TrimSpace(host)
		if host == "" {
			return PullSecretSelector{}, errorx.IllegalArgument.New(
				"--image-pull-secret %q has an empty registry host; use HOST=NAME (e.g. ghcr.io=ghcr-creds)", v)
		}
		if _, dup := sel.ByHost[host]; dup {
			return PullSecretSelector{}, errorx.IllegalArgument.New(
				"duplicate --image-pull-secret for host %q", host)
		}
		if sel.ByHost == nil {
			sel.ByHost = make(map[string]string)
		}
		sel.ByHost[host] = name
	}
	return sel, nil
}

// Registry selection strategies for a multi-registry SoftwareVersionSource. These
// match the operator's SelectionStrategy enum. Weaver defaults to Sequential when
// emitting a source so the manifest's primary registry is tried first.
const (
	RegistrySelectionRandom     = "Random"
	RegistrySelectionSequential = "Sequential"
)

// NormalizeRegistrySelectionStrategy validates and canonicalizes the
// --registry-selection-strategy value. It accepts any case, returns the exact
// enum the operator expects, and treats empty as "use the weaver default"
// (applied by the capsule step when emitting a source).
func NormalizeRegistrySelectionStrategy(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return "", nil
	case "random":
		return RegistrySelectionRandom, nil
	case "sequential":
		return RegistrySelectionSequential, nil
	default:
		return "", errorx.IllegalArgument.New(
			"invalid registry selection strategy %q (allowed: Random, Sequential)", s)
	}
}

// RegistryHost returns the host part of an image reference, which is the text
// before the first "/" after any scheme. For example, "ghcr.io/hashgraph/x"
// returns "ghcr.io". A reference with no "/" is returned as-is.
func RegistryHost(image string) string {
	s := image
	if i := strings.Index(s, "://"); i != -1 {
		s = s[i+3:]
	}
	if i := strings.IndexByte(s, '/'); i != -1 {
		return s[:i]
	}
	return s
}

// ImageSource is the multi-registry image source resolved from the deployment
// manifest, mapped onto the operator's SoftwareVersionSource. LayerHashes is the
// per-platform ("linux/amd64") shared set every candidate must match; only
// deterministic images (shared hashes across registries) are representable, so a
// nil ImageSource means "keep the single SoftwareVersion". SelectionStrategy
// mirrors the manifest's preference (Random/Sequential, or "" for none); the
// capsule step applies it when no CLI override is given.
type ImageSource struct {
	Repositories      []ImageRepositoryRef `json:"repositories"`
	LayerHashes       map[string][]string  `json:"layerHashes"`
	SelectionStrategy string               `json:"selectionStrategy,omitempty"`
}

// ConsensusNodeInputs holds user-supplied values for deploying a consensus node
// via the solo-operator's ConsensusCapsule CRD.
type ConsensusNodeInputs struct {
	Namespace string `json:"namespace"`
	OrbitName string `json:"orbitName"`
	NodeId    int64  `json:"nodeId"`
	AccountId string `json:"accountId"`
	Weight    int    `json:"weight"`

	// ProvisionerDaemonEnabled deploys the Orbit in mainnet mode: the UC sidecar
	// runs UC_MODE=mainnet and defers the execute phase to a host-level
	// solo-provisioner-daemon. Default false = cluster-only (the in-pod UC runs
	// execute). Sets Orbit.spec.consensus.provisionerDaemonEnabled.
	ProvisionerDaemonEnabled bool `json:"provisionerDaemonEnabled,omitempty"`

	LedgerId string `json:"ledgerId"`
	ChainId  string `json:"chainId,omitempty"`

	ConsensusImageRepo string `json:"consensusImageRepo"`
	ConsensusImageTag  string `json:"consensusImageTag"`

	// ConsensusImageSource is the multi-registry image source for the consensus-node
	// container, resolved from the manifest by the BLL (not a CLI flag). When set,
	// the capsule step adds a SoftwareVersionSource alongside SoftwareVersion.
	ConsensusImageSource *ImageSource `json:"-"`

	// ImagePinned is set by the BLL when the user explicitly pinned the image via
	// --image-repo/--image-tag. It tells the capsule step not to preserve a
	// SoftwareVersionSource already on the live CR — an explicit pin means "run
	// exactly this single image".
	ImagePinned bool `json:"-"`

	// UC sidecar image (repository is the full "registry/path/name"). Empty falls
	// back to ConsensusDefaultUCImageRepo/Tag. The operator has no default UC image.
	UCImageRepo string `json:"ucImageRepo,omitempty"`
	UCImageTag  string `json:"ucImageTag,omitempty"`

	// ImagePullSecrets is the host-keyed selector from --image-pull-secret. It names
	// docker-registry secrets in the node's namespace. Every image (consensus, UC,
	// each candidate registry) picks its own secret by host at build time, so a new
	// image type needs no new field.
	ImagePullSecrets PullSecretSelector `json:"imagePullSecrets,omitempty"`

	// RegistrySelectionStrategy sets SoftwareVersionSource.SelectionStrategy
	// (Random or Sequential) when a multi-registry source is emitted. Empty uses the
	// weaver default (Sequential — manifest primary first).
	RegistrySelectionStrategy string `json:"registrySelectionStrategy,omitempty"`

	DeploymentPackageDir string `json:"deploymentPackageDir,omitempty"`

	GrpcTlsSecret string `json:"grpcTlsSecret,omitempty"`
	SigningSecret string `json:"signingSecret,omitempty"`

	// Consensus-node container sizing + JVM tuning. Empty values fall back to the
	// ConsensusDefault* constants (mirroring the solo-operator reference example).
	// The explicit Java heap is essential — without -Xmx the 0.74 node stalls on
	// startup with a GC death-spiral (solo-operator#1223).
	ContainerName string `json:"containerName,omitempty"`
	JavaHeapMin   string `json:"javaHeapMin,omitempty"`
	JavaHeapMax   string `json:"javaHeapMax,omitempty"`
	JavaOpts      string `json:"javaOpts,omitempty"`
	CPULimit      string `json:"cpuLimit,omitempty"`
	CPURequest    string `json:"cpuRequest,omitempty"`
	MemoryLimit   string `json:"memoryLimit,omitempty"`
	MemoryRequest string `json:"memoryRequest,omitempty"`

	// Volumes is the fully-merged per-volume backing configuration (emptyDir /
	// hostPath / PVC) with global defaults. Built by the CLI from --volumes-file plus
	// the --default-* and --volume flags. Empty = every volume emptyDir (operator
	// default). Resolve a volume's effective backing with Volumes.EffectiveSpec.
	Volumes ConsensusVolumeConfig `json:"volumes,omitempty"`

	// HostPathUID/HostPathGID own the hostPath directories created for hostpath-backed
	// volumes. Non-positive values fall back to the canonical hedera user/group
	// (config.HederaUserId/GroupId, 2000:2000), resolved by the chown step.
	HostPathUID int `json:"hostPathUid,omitempty"`
	HostPathGID int `json:"hostPathGid,omitempty"`

	// Profile is the deployment profile (local/testnet/mainnet/...) used to size
	// the host hardware floor when this install bootstraps the cluster.
	Profile string `json:"profile,omitempty"`

	// SkipHardwareChecks bypasses the preflight hardware floor during cluster bootstrap.
	SkipHardwareChecks bool `json:"-"`

	// Resolved config file contents (populated by BLL, not by CLI flags)
	ConfigLog4j2                string `json:"-"`
	ConfigSettings              string `json:"-"`
	ConfigAppProperties         string `json:"-"`
	ConfigAppOverrideProperties string `json:"-"`
	ConfigApiPermission         string `json:"-"`
	ConfigBootstrap             string `json:"-"`
	ConfigNodeProperties        string `json:"-"`
	ConfigFeeSchedules          string `json:"-"`
	ConfigSimpleFeesSchedules   string `json:"-"`
	ConfigThrottles             string `json:"-"`
	ConfigBlockNodes            string `json:"-"`

	// ConfigSources records, per config key (ConfigKey*), whether the resolved
	// content came from the deployment package or the embedded default. Populated
	// by the BLL during resolution; drives the apply policy for config CRs.
	ConfigSources map[string]string `json:"-"`
}

// Config keys identify each consensus config file across resolution, hashing,
// and CR application. They double as the ConfigSources / ConfigHashes map keys.
const (
	ConfigKeyLog4j2              = "log4j2"
	ConfigKeySettings            = "settings"
	ConfigKeyAppProperties       = "application-properties"
	ConfigKeyAppOverride         = "application-override-properties"
	ConfigKeyApiPermission       = "api-permission-properties"
	ConfigKeyBootstrap           = "bootstrap-properties"
	ConfigKeyNodeProperties      = "node-properties"
	ConfigKeyFeeSchedules        = "fee-schedules"
	ConfigKeySimpleFeesSchedules = "simple-fees-schedules"
	ConfigKeyThrottles           = "throttles"
	ConfigKeyBlockNodes          = "block-nodes"
)

// consensusConfigCRSuffix maps each config key to the canonical name suffix the
// solo-operator expects. The config CR, the ConfigMap the operator derives from
// it, and the ConsensusCapsule *Ref that points at it all share this exact name
// (verified against the solo-operator docs/example). This is the single source
// of truth so EnsureConfigCRs and the capsule refs can never drift apart — a
// mismatch leaves the capsule stuck (referenced ConfigMap "not found").
//
// Note simple-fee-schedules is intentionally singular "fee" here even though the
// ConfigKey is "simple-fees-schedules": the operator/example use the singular
// form for the resource name.
var consensusConfigCRSuffix = map[string]string{
	ConfigKeyLog4j2:              "log4j2",
	ConfigKeySettings:            "settings",
	ConfigKeyAppProperties:       "application-properties",
	ConfigKeyAppOverride:         "application-override-properties",
	ConfigKeyApiPermission:       "api-permission-properties",
	ConfigKeyBootstrap:           "bootstrap-properties",
	ConfigKeyNodeProperties:      "node-properties",
	ConfigKeyFeeSchedules:        "fee-schedules",
	ConfigKeySimpleFeesSchedules: "simple-fee-schedules",
	ConfigKeyThrottles:           "throttles",
	ConfigKeyBlockNodes:          "block-nodes",
}

// ConsensusConfigCRName returns the config CR / managed ConfigMap / capsule ref
// name for a config key on a node, e.g.
// ConsensusConfigCRName("node0", ConfigKeyAppProperties) == "node0-application-properties".
func ConsensusConfigCRName(scope, configKey string) string {
	return fmt.Sprintf("%s-%s", scope, consensusConfigCRSuffix[configKey])
}

// Config content sources, stored in ConfigSources and ConfigHashEntry.Source.
const (
	ConfigSourcePackage  = "deployment-package"
	ConfigSourceEmbedded = "embedded"
)

// ConsensusNodeScope returns the node-local scope for a consensus node (e.g.
// "node0"). Use this for names of resources that live inside the node's
// namespace (secrets, config CR refs, secret key filenames) — the namespace
// already disambiguates them across orbits, so they stay node-local.
func ConsensusNodeScope(nodeId int64) string {
	return fmt.Sprintf("node%d", nodeId)
}

// ConsensusNodeStateKey returns the orbit-qualified key for solo-weaver's local
// state and reality maps (e.g. "hiero-network-1/node0"). Because the same
// nodeId can be deployed into multiple orbits in one cluster, the local state
// key must include the namespace/orbit — a bare "node0" would collide across
// orbits and overwrite entries in state.yaml. This is NOT a cluster resource
// name; it never appears on any Kubernetes object.
func ConsensusNodeStateKey(namespace string, nodeId int64) string {
	return fmt.Sprintf("%s/%s", namespace, ConsensusNodeScope(nodeId))
}

// ConsensusCapsuleName returns the CR name for a ConsensusCapsule (e.g. "myorbit-consensus-0").
func ConsensusCapsuleName(orbitName string, nodeId int64) string {
	return fmt.Sprintf("%s-consensus-%d", orbitName, nodeId)
}

// Validate checks that required fields are present.
func (c *ConsensusNodeInputs) Validate() error {
	if c.Namespace == "" {
		return errorx.IllegalArgument.New("--namespace is required")
	}
	if c.AccountId == "" {
		return errorx.IllegalArgument.New("--account-id is required")
	}
	if c.Weight <= 0 {
		return errorx.IllegalArgument.New("--weight must be positive")
	}
	return nil
}
