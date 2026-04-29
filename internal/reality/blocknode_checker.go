// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"context"
	"fmt"
	"strings"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
	htime "helm.sh/helm/v3/pkg/time"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// blockNodeChecker probes the BlockNode Helm release and associated Kubernetes
// PersistentVolumes to build a BlockNodeState.
// It depends only on injectable helm/kube factories and a cluster probe.
type blockNodeChecker struct {
	sm            state.Manager
	newHelm       func() (HelmManager, error)
	newKube       func() (KubeClient, error)
	clusterExists ClusterProbe
}

// NewBlockNodeChecker constructs a blockNodeChecker.
// In production pass helm2.NewManager, kube.NewClient and kube.ClusterExists.
// In tests swap them for fakes.
func NewBlockNodeChecker(
	sm state.Manager,
	newHelm func() (HelmManager, error),
	newKube func() (KubeClient, error),
	clusterExists ClusterProbe,
) (Checker[state.BlockNodeState], error) {
	return &blockNodeChecker{
		sm:            sm,
		newHelm:       newHelm,
		newKube:       newKube,
		clusterExists: clusterExists,
	}, nil
}

func (b *blockNodeChecker) RefreshState(ctx context.Context) (state.BlockNodeState, error) {
	now := htime.Now()
	bn := b.sm.State().BlockNodeState

	exists, err := b.clusterExists()
	if !exists {
		logx.As().Debug().Err(err).Msg("Kubernetes cluster does not exist, skipping BlockNodeState check")
		return bn, nil
	}

	re, err := b.findBlockNodeHelmRelease()
	if err != nil {
		return bn, err
	}

	if bn.ReleaseInfo.Status == release.StatusDeployed && re == nil {
		logx.As().Debug().Msg("BlockNode Helm release was previously deployed but is now missing; treating as deleted or unknown")
		return state.NewBlockNodeState(), nil
	}

	if re == nil {
		logx.As().Debug().Msg("No BlockNode Helm release found; returning default BlockNodeState")
		return state.NewBlockNodeState(), nil
	}

	logx.As().Debug().Msg("Found BlockNode Helm release; building BlockNodeState")
	bn = state.BlockNodeState{
		ReleaseInfo: state.HelmReleaseInfo{
			Name:          re.Name,
			ChartVersion:  re.Chart.Metadata.Version,
			AppVersion:    re.Chart.Metadata.AppVersion,
			Namespace:     re.Namespace,
			ChartRef:      "", // it is not stored in Helm Release, so caller need to inject it
			ChartName:     re.Chart.ChartFullPath(),
			FirstDeployed: re.Info.FirstDeployed,
			LastDeployed:  re.Info.LastDeployed,
			DeletedAt:     re.Info.Deleted,
			Status:        re.Info.Status,
		},
		Storage:  models.BlockNodeStorage{},
		LastSync: now,
	}

	b.populateRetentionFromHelmValues(re.Config, &bn)

	// PersistentVolumes are cluster-scoped; pass empty namespace.
	k8s, err := b.newKube()
	if err != nil {
		return bn, err
	}

	pvs, err := k8s.List(ctx, kube.KindPV, "", kube.WaitOptions{})
	if err != nil {
		return bn, err
	}

	if err := b.populateStorageFromPVs(pvs, re.Namespace, &bn.Storage); err != nil {
		return bn, err
	}

	return bn, nil
}

// findBlockNodeHelmRelease iterates all Helm releases and returns the first one
// whose manifest contains a StatefulSet labelled app.kubernetes.io/instance=block-node.
// Returns (nil, nil) when no matching release is found.
func (b *blockNodeChecker) findBlockNodeHelmRelease() (*release.Release, error) {
	helm, err := b.newHelm()
	if err != nil {
		return nil, err
	}

	releases, err := helm.ListAll()
	if err != nil {
		return nil, err
	}

	for _, re := range releases {
		logx.As().Debug().Str("release", re.Name).Any("info", re.Info).Msg("Inspecting Helm release")

		manifests, err := UnmarshalManifest(re.Manifest)
		if err != nil {
			return nil, err
		}

		for _, manifest := range manifests {
			if manifest.GetKind() != "StatefulSet" {
				continue
			}
			if manifest.GetLabels()["app.kubernetes.io/instance"] == "block-node" {
				logx.As().Debug().
					Str("releaseName", re.Name).
					Str("namespace", manifest.GetNamespace()).
					Str("name", manifest.GetName()).
					Any("labels", manifest.GetLabels()).
					Msg("Found BlockNode StatefulSet in Helm release")
				return re, nil
			}
		}
	}

	return nil, nil // no matching release
}

// populateStorageFromPVs extracts hostPath and capacity from PVs bound to the
// given namespace and fills in the appropriate fields of storage.
// PVs whose claimRef namespace does not match are skipped silently.
// Returns an error if a matching PV is missing required fields.
func (b *blockNodeChecker) populateStorageFromPVs(
	pvs *unstructured.UnstructuredList,
	namespace string,
	storage *models.BlockNodeStorage,
) error {
	if pvs == nil || storage == nil {
		return nil
	}

	for _, pv := range pvs.Items {
		logx.As().Debug().Str("pv", pv.GetName()).Msg("Checking PV for BlockNode storage")

		claimRef, found, err := unstructured.NestedMap(pv.Object, "spec", "claimRef")
		if err != nil || !found {
			continue
		}

		claimNamespace, _, _ := unstructured.NestedString(claimRef, "namespace")
		claimName, _, _ := unstructured.NestedString(claimRef, "name")

		if claimNamespace != namespace {
			continue
		}

		size, found, err := unstructured.NestedString(pv.Object, "spec", "capacity", "storage")
		if err != nil || !found {
			return errorx.IllegalState.New("PV %s does not have a storage capacity field", pv.GetName())
		}

		hostpath, found, err := unstructured.NestedString(pv.Object, "spec", "hostPath", "path")
		if err != nil || !found {
			return errorx.IllegalState.New("PV %s does not have a hostPath field", pv.GetName())
		}

		logx.As().Debug().
			Str("pv", pv.GetName()).
			Str("pvc", claimName).
			Str("namespace", claimNamespace).
			Str("size", size).
			Str("hostPath", hostpath).
			Msg("Found BlockNode storage PV/PVC")

		switch {
		case strings.Contains(claimName, "live"):
			storage.LivePath = hostpath
			storage.LiveSize = size
		case strings.Contains(claimName, "archive"):
			storage.ArchivePath = hostpath
			storage.ArchiveSize = size
		case strings.Contains(claimName, "log"):
			storage.LogPath = hostpath
			storage.LogSize = size
		case strings.Contains(claimName, "verification"):
			storage.VerificationPath = hostpath
			storage.VerificationSize = size
		case strings.Contains(claimName, "plugins"):
			storage.PluginsPath = hostpath
			storage.PluginsSize = size
		}

	}

	return nil
}

// populateRetentionFromHelmValues extracts FILES_HISTORIC_BLOCK_RETENTION_THRESHOLD
// and FILES_RECENT_BLOCK_RETENTION_THRESHOLD from the Helm release's computed
// values (blockNode.config.*) and writes them into the BlockNodeState.
// Values may be stored as strings or numeric types depending on how the YAML was
// originally specified; both forms are accepted and converted to string.
// Missing or empty values are silently ignored.
func (b *blockNodeChecker) populateRetentionFromHelmValues(config map[string]interface{}, bn *state.BlockNodeState) {
	if config == nil || bn == nil {
		return
	}

	blockNode, ok := config["blockNode"].(map[string]interface{})
	if !ok {
		return
	}

	chartConfig, ok := blockNode["config"].(map[string]interface{})
	if !ok {
		return
	}

	if v := helmValueToString(chartConfig["FILES_HISTORIC_BLOCK_RETENTION_THRESHOLD"]); v != "" {
		bn.HistoricRetention = v
		logx.As().Debug().Str("historicRetention", v).Msg("Extracted historic block retention threshold from Helm values")
	}

	if v := helmValueToString(chartConfig["FILES_RECENT_BLOCK_RETENTION_THRESHOLD"]); v != "" {
		bn.RecentRetention = v
		logx.As().Debug().Str("recentRetention", v).Msg("Extracted recent block retention threshold from Helm values")
	}
}

// helmValueToString converts a Helm config value to its string representation.
// Helm's release.Config commonly stores YAML numeric scalars as float64 or
// int/int64, so this helper accepts those types in addition to plain strings.
// Non-whole float64 values are treated as invalid (retention thresholds must be
// non-negative integers) and return "" with a warning log.
// Returns "" for nil or unsupported types.
func helmValueToString(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case int:
		return fmt.Sprintf("%d", val)
	case int64:
		return fmt.Sprintf("%d", val)
	case float64:
		// YAML numeric scalars are often decoded as float64;
		// only accept whole numbers — fractional values are invalid for
		// retention thresholds and would fail downstream validation.
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		logx.As().Warn().
			Float64("value", val).
			Msg("Ignoring non-integer retention threshold from Helm values; expected a whole number")
		return ""
	default:
		return ""
	}
}
