// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"context"
	"strings"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	htime "helm.sh/helm/v3/pkg/time"
)

// ConsensusKubeClient is the subset of kube.Client used by the consensus checker.
type ConsensusKubeClient interface {
	ResourceExists(ctx context.Context, apiVersion, kind, namespace, name string) (bool, error)
	GetResourceNestedString(ctx context.Context, apiVersion, kind, namespace, name string, fields ...string) (string, error)
	GetResourceNestedInt64(ctx context.Context, apiVersion, kind, namespace, name string, fields ...string) (int64, bool, error)
}

type consensusChecker struct {
	sm            state.Manager
	newKube       func() (ConsensusKubeClient, error)
	clusterExists ClusterProbe
}

// NewConsensusChecker creates a reality checker that reads Orbit and ConsensusCapsule CRs
// from the cluster to build a map of ConsensusNodeState.
func NewConsensusChecker(
	sm state.Manager,
	newKube func() (ConsensusKubeClient, error),
	clusterExists ClusterProbe,
) (Checker[map[string]state.ConsensusNodeState], error) {
	if sm == nil {
		return nil, errorx.IllegalArgument.New("state manager cannot be nil")
	}
	return &consensusChecker{sm: sm, newKube: newKube, clusterExists: clusterExists}, nil
}

func (c *consensusChecker) RefreshState(ctx context.Context) (map[string]state.ConsensusNodeState, error) {
	l := logx.As()

	exists, err := c.clusterExists()
	if err != nil || !exists {
		l.Debug().Msg("Cluster not reachable; returning persisted consensus state")
		return c.sm.State().ConsensusNodes, nil
	}

	kc, err := c.newKube()
	if err != nil {
		return nil, errorx.ExternalError.Wrap(err, "failed to create kube client for consensus check")
	}

	persisted := c.sm.State().ConsensusNodes
	if persisted == nil {
		persisted = make(map[string]state.ConsensusNodeState)
	}

	result := make(map[string]state.ConsensusNodeState, len(persisted))
	apiVersion := kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion

	for scope, ns := range persisted {
		capsuleName := models.ConsensusCapsuleName(ns.OrbitName, ns.NodeId)
		capsuleExists, err := kc.ResourceExists(ctx, apiVersion, string(kube.KindConsensusCapsule), ns.Namespace, capsuleName)
		if err != nil {
			l.Warn().Err(err).Str("scope", scope).Msg("Failed to check ConsensusCapsule existence")
			result[scope] = ns
			continue
		}

		if !capsuleExists {
			l.Info().Str("scope", scope).Msg("ConsensusCapsule not found in cluster; preserving persisted state")
			result[scope] = ns
			continue
		}

		updated := ns
		updated.LastSync = htime.Now()

		// ImageRepo is the full "registry/path/name". The operator stores it split
		// across softwareVersion.repository (registry/path) and .imageName (name), so
		// reassemble both — reading repository alone drops the name and, because
		// Reality outranks the deployment-package Config, each idempotent re-run would
		// otherwise feed a shorter path back in (gcr.io/hedera-registry/consensus-node
		// → gcr.io/hedera-registry → gcr.io), corrupting the image reference.
		repo, _ := kc.GetResourceNestedString(ctx, apiVersion, string(kube.KindConsensusCapsule),
			ns.Namespace, capsuleName,
			"spec", "podProperties", "containers", "consensusNode", "softwareVersion", "repository")
		imageName, _ := kc.GetResourceNestedString(ctx, apiVersion, string(kube.KindConsensusCapsule),
			ns.Namespace, capsuleName,
			"spec", "podProperties", "containers", "consensusNode", "softwareVersion", "imageName")
		if full := joinImageRef(repo, imageName); full != "" {
			updated.ImageRepo = full
		}
		if tag, err := kc.GetResourceNestedString(ctx, apiVersion, string(kube.KindConsensusCapsule),
			ns.Namespace, capsuleName,
			"spec", "podProperties", "containers", "consensusNode", "softwareVersion", "imageTag"); err == nil && tag != "" {
			updated.ImageTag = tag
		}
		if acct, err := kc.GetResourceNestedString(ctx, apiVersion, string(kube.KindConsensusCapsule),
			ns.Namespace, capsuleName, "spec", "accountId"); err == nil && acct != "" {
			updated.AccountId = acct
		}
		if w, found, err := kc.GetResourceNestedInt64(ctx, apiVersion, string(kube.KindConsensusCapsule),
			ns.Namespace, capsuleName, "spec", "weight"); err == nil && found {
			updated.Weight = int(w)
		}

		orbitExists, err := kc.ResourceExists(ctx, apiVersion, string(kube.KindOrbit), "", ns.OrbitName)
		if err == nil && orbitExists {
			if lid, err := kc.GetResourceNestedString(ctx, apiVersion, string(kube.KindOrbit),
				"", ns.OrbitName,
				"spec", "consensus", "genesis", "addressBook", "ledgerId"); err == nil && lid != "" {
				updated.LedgerId = lid
			}
			if cid, err := kc.GetResourceNestedString(ctx, apiVersion, string(kube.KindOrbit),
				"", ns.OrbitName,
				"spec", "consensus", "genesis", "addressBook", "chainId"); err == nil && cid != "" {
				updated.ChainId = cid
			}
		}

		// Surface out-of-band drift: compare the live capsule against the recorded
		// managed baseline and log any differences. We report but do NOT absorb the
		// managed shape (updated keeps the persisted ManagedSpec), so drift keeps
		// being reported until the operator reconciles via re-apply/reconfigure.
		// Identity fields are still reconciled into updated above (reality outranks
		// for RSL resolution) — the log makes that reconciliation visible rather
		// than silent.
		live := c.readLiveConsensusShape(ctx, kc, apiVersion, ns.Namespace, capsuleName)
		live.ImageRepo = updated.ImageRepo
		live.ImageTag = updated.ImageTag
		live.AccountId = updated.AccountId
		live.LedgerId = updated.LedgerId
		live.ChainId = updated.ChainId
		if drift := diffConsensusManaged(ns, live); len(drift) > 0 {
			l.Warn().
				Str("scope", scope).
				Str("capsule", capsuleName).
				Strs("drift", drift).
				Msg("Consensus node has drifted from its recorded managed spec")
		}

		result[scope] = updated
	}

	return result, nil
}

// readLiveConsensusShape reads the managed sizing/JVM/UC fields from the live
// capsule's consensus-node container (and UC sidecar) for drift comparison. It is
// best-effort: unreadable fields are left empty and skipped by diffConsensusManaged.
// Identity fields are filled in by the caller from the values it already read.
func (c *consensusChecker) readLiveConsensusShape(
	ctx context.Context, kc ConsensusKubeClient, apiVersion, namespace, capsuleName string,
) liveConsensusShape {
	cn := []string{"spec", "podProperties", "containers", "consensusNode"}
	get := func(fields ...string) string {
		v, _ := kc.GetResourceNestedString(ctx, apiVersion, string(kube.KindConsensusCapsule), namespace, capsuleName, fields...)
		return v
	}

	var live liveConsensusShape
	live.ContainerName = get(append(cn, "name")...)
	live.JavaHeapMin = get(append(cn, "javaHeapMin")...)
	live.JavaHeapMax = get(append(cn, "javaHeapMax")...)
	live.JavaOpts = get(append(cn, "javaOpts")...)
	live.CPULimit = get(append(cn, "resources", "limits", "cpu")...)
	live.MemoryLimit = get(append(cn, "resources", "limits", "memory")...)
	live.CPURequest = get(append(cn, "resources", "requests", "cpu")...)
	live.MemoryRequest = get(append(cn, "resources", "requests", "memory")...)

	uc := []string{"spec", "podProperties", "containers", "uc", "softwareVersion"}
	ucRepo := get(append(uc, "repository")...)
	ucName := get(append(uc, "imageName")...)
	live.UCImageRepo = joinImageRef(ucRepo, ucName)
	live.UCImageTag = get(append(uc, "imageTag")...)

	return live
}

// joinImageRef reassembles the full "registry/path/name" image reference from the
// operator's split repository (registry/path) and imageName (name) fields. It is
// the inverse of the split done in step_consensus_capsule.go's splitConsensusImage,
// so reading a capsule and re-applying it round-trips losslessly.
func joinImageRef(repository, imageName string) string {
	repository = strings.TrimSpace(repository)
	imageName = strings.TrimSpace(imageName)
	switch {
	case repository == "":
		return imageName
	case imageName == "":
		return repository
	default:
		return repository + "/" + imageName
	}
}
