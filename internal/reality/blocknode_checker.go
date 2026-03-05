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
	"helm.sh/helm/v3/pkg/release"
	htime "helm.sh/helm/v3/pkg/time"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// blockNodeChecker probes the BlockNode Helm release and associated Kubernetes
// PersistentVolumes to build a BlockNodeState.
// It depends only on injectable helm/kube factories and a cluster probe.
type blockNodeChecker struct {
	Base
	cfg           models.Config
	newHelm       func() (HelmManager, error)
	newKube       func() (KubeClient, error)
	clusterExists ClusterProbe
}

// NewBlockNodeChecker constructs a blockNodeChecker.
// In production pass helm2.NewManager, kube.NewClient and kube.ClusterExists.
// In tests swap them for fakes.
func NewBlockNodeChecker(
	cfg models.Config,
	sm state.Manager,
	newHelm func() (HelmManager, error),
	newKube func() (KubeClient, error),
	clusterExists ClusterProbe,
) Checker[state.BlockNodeState] {
	return &blockNodeChecker{
		Base: Base{
			sm: sm,
		},
		cfg:           cfg,
		newHelm:       newHelm,
		newKube:       newKube,
		clusterExists: clusterExists,
	}
}

func (b *blockNodeChecker) FlushState(st state.BlockNodeState) error {
	if err := b.sm.Refresh(); err != nil && !errorx.IsOfType(err, state.NotFoundError) {
		return ErrFlushError.Wrap(err, "failed to refresh state")
	}
	fullState := b.sm.State()
	fullState.BlockNodeState = st
	if err := b.sm.Set(fullState).FlushState(); err != nil {
		return ErrFlushError.Wrap(err, "failed to persist state with refreshed BlockNodeState")
	}

	return nil
}

func (b *blockNodeChecker) RefreshState(ctx context.Context) (state.BlockNodeState, error) {
	now := htime.Now()
	bn := b.sm.State().BlockNodeState
	chartRef := b.cfg.BlockNode.Chart // chart ref/repo from config
	if bn.ReleaseInfo.ChartRef != "" {
		chartRef = bn.ReleaseInfo.ChartRef // inherit
	}

	exists, err := b.clusterExists()
	if !exists {
		logx.As().Debug().Err(err).Msg("Kubernetes cluster does not exist, skipping BlockNodeState check")
		return bn, nil
	}

	re, err := b.findBlockNodeHelmRelease()
	if err != nil {
		return bn, err
	}
	if re == nil {
		return bn, nil // no BlockNode release found
	}

	bn = state.BlockNodeState{
		ReleaseInfo: state.HelmReleaseInfo{
			Name:          re.Name,
			Version:       re.Chart.Metadata.AppVersion,
			Namespace:     re.Namespace,
			ChartRef:      chartRef,
			ChartName:     re.Chart.ChartFullPath(),
			ChartVersion:  re.Chart.Metadata.Version,
			FirstDeployed: re.Info.FirstDeployed,
			LastDeployed:  re.Info.LastDeployed,
			Deleted:       re.Info.Deleted,
			Status:        re.Info.Status,
		},
		Storage:  models.BlockNodeStorage{},
		LastSync: now,
	}

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

	if err = b.FlushState(bn); err != nil {
		return bn, err
	}

	logx.As().Debug().Any("blocknodeState", bn).Msg("Refreshed blocknode state")

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
		}
	}

	return nil
}
