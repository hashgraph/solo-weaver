// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"context"
	"io"
	"strings"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/kube"
	helm2 "github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
	htime "helm.sh/helm/v3/pkg/time"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// Checker is the abstraction for accessing the current state of the system
// including cluster, machines, and blocknodes etc.
//
// It is used by the BLL to make decisions based on the actual state of the system.
// It is separate from the StateManager and BLL which manages the current state and executes intents.
//
// This separation allows for clearer distinction between current and actual states as well as intent execution.
// This is supposed to be resource heavy and may involve network calls, so it should be used judiciously.
type Checker interface {
	RefreshState(ctx context.Context, st *core.State) error

	ClusterState(ctx context.Context) (*core.ClusterState, error)

	MachineState(ctx context.Context) (*core.MachineState, error)

	BlockNodeState(ctx context.Context) (*core.BlockNodeState, error)
}

type realityChecker struct {
	current *core.State
}

func (r *realityChecker) RefreshState(ctx context.Context, st *core.State) error {
	//TODO implement me
	panic("implement me")
}

func (r *realityChecker) ClusterState(ctx context.Context) (*core.ClusterState, error) {
	//TODO implement me
	panic("implement me")
}

func (r *realityChecker) MachineState(ctx context.Context) (*core.MachineState, error) {
	//TODO implement me
	panic("implement me")
}

func (r *realityChecker) BlockNodeState(ctx context.Context) (*core.BlockNodeState, error) {
	if exists, err := kube.ClusterExists(); !exists {
		logx.As().Warn().Err(err).Msg("Kubernetes cluster does not exist, skipping BlockNode state check")
		return nil, nil // cluster does not exist, so nothing to check
	}

	re, err := r.findBlockNodeHelmRelease()
	if err != nil {
		return nil, err
	}

	if re == nil {
		return nil, nil // no BlockNode release found
	}

	now := htime.Now()
	bn := core.BlockNodeState{
		ReleaseInfo: core.HelmReleaseInfo{
			Name:          re.Name,
			Version:       re.Chart.Metadata.AppVersion,
			Namespace:     re.Namespace,
			ChartRepo:     r.current.BlockNode.ReleaseInfo.ChartRepo, // repo info not available from release, so use current state
			ChartName:     re.Chart.ChartFullPath(),
			ChartVersion:  re.Chart.Metadata.Version,
			FirstDeployed: re.Info.FirstDeployed,
			LastDeployed:  re.Info.LastDeployed,
			Deleted:       re.Info.Deleted,
			Status:        re.Info.Status,
		},
		Storage: config.BlockNodeStorage{
			BasePath:    "",
			ArchivePath: "",
			LivePath:    "",
			LogPath:     "",
			LiveSize:    "",
			ArchiveSize: "",
			LogSize:     "",
		},
		LastSync: now,
	}

	// get storage info from from PVs and PVCs associated with the BlockNode release
	k8s, err := kube.NewClient()
	if err != nil {
		return nil, err
	}

	// PersistentVolumes are cluster-scoped -> pass empty namespace
	pvs, err := k8s.List(ctx, kube.KindPV, "", kube.WaitOptions{})
	if err != nil {
		return nil, err
	}

	// loop through pvs, find those associated with the BlockNode release
	for _, pv := range pvs.Items {
		logx.As().Debug().Any("pv", pv).Msg("Checking PV for BlockNode storage")
		claimRef, found, err := unstructured.NestedMap(pv.Object, "spec", "claimRef")
		if err != nil || !found {
			continue
		}
		claimNamespace, found, err := unstructured.NestedString(claimRef, "namespace")
		if err != nil || !found {
			continue
		}
		claimName, found, err := unstructured.NestedString(claimRef, "name")
		if err != nil || !found {
			continue
		}

		// check if this PVC belongs to the BlockNode release
		if claimNamespace == re.Namespace {
			size, found, err := unstructured.NestedString(pv.Object, "spec", "capacity", "storage")
			if err != nil || !found {
				return nil, errorx.IllegalState.New("PV %s does not have a storage size", pv.GetName())
			}

			hostpath, found, err := unstructured.NestedString(pv.Object, "spec", "hostPath", "path")
			if err != nil || !found {
				return nil, errorx.IllegalState.New("PV %s does not have a hostPath", pv.GetName())
			}

			logx.As().Debug().
				Str("pv", pv.GetName()).
				Str("pvc", claimName).
				Str("namespace", claimNamespace).
				Str("size", size).
				Str("hostPath", hostpath).
				Msg("Found BlockNode storage PV/PVC")

			if strings.Contains(claimName, "live") {
				bn.Storage.LivePath = hostpath
				bn.Storage.LiveSize = size
			} else if strings.Contains(claimName, "archive") {
				bn.Storage.ArchivePath = hostpath
				bn.Storage.ArchiveSize = size
			} else if strings.Contains(claimName, "log") {
				bn.Storage.LogPath = hostpath
				bn.Storage.LogSize = size
			}
		}
	}

	return &bn, nil
}

func (r *realityChecker) findBlockNodeHelmRelease() (*release.Release, error) {
	helm, err := helm2.NewManager()
	if err != nil {
		return nil, err
	}

	releases, err := helm.ListAll()
	if err != nil {
		return nil, err
	}

	for _, re := range releases {
		logx.As().Debug().Any("release", re.Info).Msg("Found Helm release: " + re.Name)
		manifests, err := UnmarshalManifest(re.Manifest)
		if err != nil {
			return nil, err
		}
		for _, manifest := range manifests {
			if manifest.GetKind() == "StatefulSet" {
				for l, v := range manifest.GetLabels() {
					if l == "block-node.hiero.com/type" && v == "block-node" {
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
		}
	}

	return nil, nil // no BlockNode release found
}

// UnmarshalManifest parses a Helm release manifest (possibly multi-doc YAML)
// and returns a slice of Unstructured objects (one per non-empty document).
func UnmarshalManifest(manifest string) ([]*unstructured.Unstructured, error) {
	dec := yaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 4096)
	var out []*unstructured.Unstructured

	for {
		// decode into a generic map first
		var doc map[string]interface{}
		if err := dec.Decode(&doc); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		// skip empty documents (e.g. separators or whitespace)
		if len(doc) == 0 {
			continue
		}
		u := &unstructured.Unstructured{Object: doc}
		out = append(out, u)
	}
	return out, nil
}

func NewChecker(current *core.State) (Checker, error) {
	if current == nil {
		return nil, errorx.IllegalArgument.New("current state cannot be nil")
	}

	st := current.Clone()
	if st == nil {
		return nil, errorx.IllegalArgument.New("failed to clone current state")
	}

	return &realityChecker{current: st}, nil
}
