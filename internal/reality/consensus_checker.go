// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"context"

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

		if repo, err := kc.GetResourceNestedString(ctx, apiVersion, string(kube.KindConsensusCapsule),
			ns.Namespace, capsuleName,
			"spec", "podProperties", "containers", "consensusNode", "softwareVersion", "repository"); err == nil && repo != "" {
			updated.ImageRepo = repo
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

		result[scope] = updated
	}

	return result, nil
}
