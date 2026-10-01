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
	GetResourceNestedMap(ctx context.Context, apiVersion, kind, namespace, name string, fields ...string) (map[string]interface{}, bool, error)
}

// CRD field names used when reading back from the live Orbit and ConsensusCapsule.
const (
	fieldSpec                     = "spec"
	fieldPodProperties            = "podProperties"
	fieldContainers               = "containers"
	fieldConsensusNode            = "consensusNode"
	fieldUC                       = "uc"
	fieldSoftwareVersion          = "softwareVersion"
	fieldRepository               = "repository"
	fieldImageName                = "imageName"
	fieldImageTag                 = "imageTag"
	fieldImagePullSecrets         = "imagePullSecrets"
	fieldProvisionerDaemonEnabled = "provisionerDaemonEnabled"
	fieldVolumes                  = "volumes"
	fieldPersistentVolumeClaims   = "persistentVolumeClaims"
	fieldAdditionalVolumes        = "additionalVolumes"
	fieldStreams                  = "streams"
	fieldHostPath                 = "hostPath"
	fieldPath                     = "path"
	fieldResources                = "resources"
	fieldRequests                 = "requests"
	fieldStorage                  = "storage"
	fieldStorageClassName         = "storageClassName"
	fieldAccessModes              = "accessModes"
	fieldName                     = "name"
)

type consensusChecker struct {
	sm      state.Manager
	newKube func() (ConsensusKubeClient, error)
	probe   ClusterProbe
}

// NewConsensusChecker creates a reality checker that reads Orbit and ConsensusCapsule CRs
// from the cluster to build a map of ConsensusNodeState.
func NewConsensusChecker(
	sm state.Manager,
	newKube func() (ConsensusKubeClient, error),
	probe ClusterProbe,
) (Checker[map[string]state.ConsensusNodeState], error) {
	if sm == nil {
		return nil, errorx.IllegalArgument.New("state manager cannot be nil")
	}
	return &consensusChecker{sm: sm, newKube: newKube, probe: probe}, nil
}

func (c *consensusChecker) RefreshState(ctx context.Context) (map[string]state.ConsensusNodeState, error) {
	l := logx.As()

	if !c.probe().Observed() {
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
			fieldSpec, fieldPodProperties, fieldContainers, fieldConsensusNode, fieldSoftwareVersion, fieldRepository)
		imgName, _ := kc.GetResourceNestedString(ctx, apiVersion, string(kube.KindConsensusCapsule),
			ns.Namespace, capsuleName,
			fieldSpec, fieldPodProperties, fieldContainers, fieldConsensusNode, fieldSoftwareVersion, fieldImageName)
		if full := joinImageRef(repo, imgName); full != "" {
			updated.ImageRepo = full
		}
		if tag, err := kc.GetResourceNestedString(ctx, apiVersion, string(kube.KindConsensusCapsule),
			ns.Namespace, capsuleName,
			fieldSpec, fieldPodProperties, fieldContainers, fieldConsensusNode, fieldSoftwareVersion, fieldImageTag); err == nil && tag != "" {
			updated.ImageTag = tag
		}
		if acct, err := kc.GetResourceNestedString(ctx, apiVersion, string(kube.KindConsensusCapsule),
			ns.Namespace, capsuleName, fieldSpec, "accountId"); err == nil && acct != "" {
			updated.AccountId = acct
		}
		if w, found, err := kc.GetResourceNestedInt64(ctx, apiVersion, string(kube.KindConsensusCapsule),
			ns.Namespace, capsuleName, fieldSpec, "weight"); err == nil && found {
			updated.Weight = int(w)
		}

		orbitExists, err := kc.ResourceExists(ctx, apiVersion, string(kube.KindOrbit), "", ns.OrbitName)
		if err == nil && orbitExists {
			if lid, err := kc.GetResourceNestedString(ctx, apiVersion, string(kube.KindOrbit),
				"", ns.OrbitName,
				fieldSpec, "consensus", "genesis", "addressBook", "ledgerId"); err == nil && lid != "" {
				updated.LedgerId = lid
			}
			if cid, err := kc.GetResourceNestedString(ctx, apiVersion, string(kube.KindOrbit),
				"", ns.OrbitName,
				fieldSpec, "consensus", "genesis", "addressBook", "chainId"); err == nil && cid != "" {
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
		if err == nil && orbitExists {
			readProvisionerDaemonEnabled(ctx, kc, apiVersion, ns.OrbitName, &live)
		}
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
	cn := []string{fieldSpec, fieldPodProperties, fieldContainers, fieldConsensusNode}
	get := func(fields ...string) string {
		v, _ := kc.GetResourceNestedString(ctx, apiVersion, string(kube.KindConsensusCapsule), namespace, capsuleName, fields...)
		return v
	}

	var live liveConsensusShape
	live.ContainerName = get(append(cn, fieldName)...)
	live.JavaHeapMin = get(append(cn, "javaHeapMin")...)
	live.JavaHeapMax = get(append(cn, "javaHeapMax")...)
	live.JavaOpts = get(append(cn, "javaOpts")...)
	live.CPULimit = get(append(cn, fieldResources, "limits", "cpu")...)
	live.MemoryLimit = get(append(cn, fieldResources, "limits", "memory")...)
	live.CPURequest = get(append(cn, fieldResources, fieldRequests, "cpu")...)
	live.MemoryRequest = get(append(cn, fieldResources, fieldRequests, "memory")...)

	uc := []string{fieldSpec, fieldPodProperties, fieldContainers, fieldUC, fieldSoftwareVersion}
	ucRepo := get(append(uc, fieldRepository)...)
	ucName := get(append(uc, fieldImageName)...)
	live.UCImageRepo = joinImageRef(ucRepo, ucName)
	live.UCImageTag = get(append(uc, fieldImageTag)...)

	readLiveVolumes(ctx, kc, apiVersion, namespace, capsuleName, &live)
	readLiveImagePullSecrets(ctx, kc, apiVersion, namespace, capsuleName, &live)

	return live
}

// readProvisionerDaemonEnabled reads spec.provisionerDaemonEnabled from the Orbit.
func readProvisionerDaemonEnabled(
	ctx context.Context, kc ConsensusKubeClient, apiVersion, orbitName string, live *liveConsensusShape,
) {
	specMap, found, err := kc.GetResourceNestedMap(ctx, apiVersion, string(kube.KindOrbit), "", orbitName, fieldSpec)
	if err != nil || !found {
		return
	}
	if v, ok := specMap[fieldProvisionerDaemonEnabled]; ok {
		if b, ok := v.(bool); ok {
			live.ProvisionerDaemonEnabled = b
			live.ProvisionerDaemonEnabledSet = true
		}
	}
}

// readLiveVolumes reads volume backing from spec.podProperties.volumes (hostPath)
// and spec.persistentVolumeClaims (PVC), normalizing into the same
// ConsensusVolumeConfig shape that buildConsensusNodeManagedSpec persists.
// Volumes absent from both maps are emptyDir (the operator default).
func readLiveVolumes(
	ctx context.Context, kc ConsensusKubeClient, apiVersion, namespace, capsuleName string, live *liveConsensusShape,
) {
	volMap, volFound, err := kc.GetResourceNestedMap(ctx, apiVersion,
		string(kube.KindConsensusCapsule), namespace, capsuleName,
		fieldSpec, fieldPodProperties, fieldVolumes)
	if err != nil {
		return
	}
	pvcMap, pvcFound, err := kc.GetResourceNestedMap(ctx, apiVersion,
		string(kube.KindConsensusCapsule), namespace, capsuleName,
		fieldSpec, fieldPersistentVolumeClaims)
	if err != nil {
		return
	}
	if !volFound && !pvcFound {
		live.Volumes = models.ConsensusVolumeConfig{Volumes: map[string]models.ConsensusVolumeSpec{}}
		for _, name := range models.ConsensusVolumeNames() {
			live.Volumes.Volumes[name] = models.ConsensusVolumeSpec{Type: models.VolumeBackingEmptyDir}
		}
		live.VolumesSet = true
		return
	}

	vols := make(map[string]models.ConsensusVolumeSpec)

	// hostPath: each named field holds a corev1.Volume map with hostPath.path.
	hostPathFields := map[string][]string{
		models.ConsensusVolumeUpgrade: {models.ConsensusVolumeUpgrade},
		models.ConsensusVolumeLogs:    {models.ConsensusVolumeLogs},
		models.ConsensusVolumeStats:   {models.ConsensusVolumeStats},
		models.ConsensusVolumeSaved:   {models.ConsensusVolumeSaved},
		models.ConsensusVolumeBlocks:  {fieldStreams, "block"},
		models.ConsensusVolumeRecords: {fieldStreams, "record"},
		models.ConsensusVolumeEvents:  {fieldStreams, "events"},
	}
	for volName, path := range hostPathFields {
		if hp := nestedHostPath(volMap, path); hp != "" {
			vols[volName] = models.ConsensusVolumeSpec{Type: models.VolumeBackingHostPath, Path: hp}
		}
	}
	// "state" rides AdditionalVolumes (an array).
	if avRaw, ok := volMap[fieldAdditionalVolumes]; ok {
		if avSlice, ok := avRaw.([]interface{}); ok {
			for _, item := range avSlice {
				if m, ok := item.(map[string]interface{}); ok {
					if name, _ := m[fieldName].(string); name == models.ConsensusVolumeState {
						if hp := extractHostPath(m); hp != "" {
							vols[models.ConsensusVolumeState] = models.ConsensusVolumeSpec{
								Type: models.VolumeBackingHostPath, Path: hp,
							}
						}
					}
				}
			}
		}
	}

	// PVC: each named field holds a PersistentVolumeClaimSpec map.
	pvcFields := map[string][]string{
		models.ConsensusVolumeUpgrade: {models.ConsensusVolumeUpgrade},
		models.ConsensusVolumeLogs:    {models.ConsensusVolumeLogs},
		models.ConsensusVolumeStats:   {models.ConsensusVolumeStats},
		models.ConsensusVolumeSaved:   {models.ConsensusVolumeSaved},
		models.ConsensusVolumeState:   {models.ConsensusVolumeState},
		models.ConsensusVolumeBlocks:  {fieldStreams, "block"},
		models.ConsensusVolumeRecords: {fieldStreams, "record"},
		models.ConsensusVolumeEvents:  {fieldStreams, "events"},
	}
	for volName, path := range pvcFields {
		if _, already := vols[volName]; already {
			continue
		}
		if spec := nestedPVCSpec(pvcMap, path); spec != nil {
			vols[volName] = *spec
		}
	}

	// Volumes not in either map are emptyDir (operator default).
	for _, name := range models.ConsensusVolumeNames() {
		if _, ok := vols[name]; !ok {
			vols[name] = models.ConsensusVolumeSpec{Type: models.VolumeBackingEmptyDir}
		}
	}

	live.Volumes = models.ConsensusVolumeConfig{Volumes: vols}
	live.VolumesSet = true
}

// nestedHostPath drills into a nested map along path and extracts hostPath.path.
func nestedHostPath(m map[string]interface{}, path []string) string {
	cur := m
	for _, key := range path {
		next, ok := cur[key].(map[string]interface{})
		if !ok {
			return ""
		}
		cur = next
	}
	return extractHostPath(cur)
}

func extractHostPath(m map[string]interface{}) string {
	hp, ok := m[fieldHostPath].(map[string]interface{})
	if !ok {
		return ""
	}
	p, _ := hp[fieldPath].(string)
	return p
}

// nestedPVCSpec drills into a nested map along path and extracts PVC fields.
func nestedPVCSpec(m map[string]interface{}, path []string) *models.ConsensusVolumeSpec {
	cur := m
	for _, key := range path {
		next, ok := cur[key].(map[string]interface{})
		if !ok {
			return nil
		}
		cur = next
	}
	spec := models.ConsensusVolumeSpec{Type: models.VolumeBackingPVC}

	if res, ok := cur[fieldResources].(map[string]interface{}); ok {
		if req, ok := res[fieldRequests].(map[string]interface{}); ok {
			if s, ok := req[fieldStorage].(string); ok {
				spec.Size = s
			}
		}
	}
	if sc, ok := cur[fieldStorageClassName].(string); ok {
		spec.StorageClass = sc
	}
	if modes, ok := cur[fieldAccessModes].([]interface{}); ok && len(modes) > 0 {
		if am, ok := modes[0].(string); ok {
			spec.AccessMode = am
		}
	}
	return &spec
}

// readLiveImagePullSecrets reads image-pull secrets from the consensus-node and UC
// containers' SoftwareVersion.imagePullSecrets, reconstructing a PullSecretSelector.
func readLiveImagePullSecrets(
	ctx context.Context, kc ConsensusKubeClient, apiVersion, namespace, capsuleName string, live *liveConsensusShape,
) {
	cnSV, cnFound, err := kc.GetResourceNestedMap(ctx, apiVersion,
		string(kube.KindConsensusCapsule), namespace, capsuleName,
		fieldSpec, fieldPodProperties, fieldContainers, fieldConsensusNode, fieldSoftwareVersion)
	if err != nil {
		return
	}
	ucSV, ucFound, err := kc.GetResourceNestedMap(ctx, apiVersion,
		string(kube.KindConsensusCapsule), namespace, capsuleName,
		fieldSpec, fieldPodProperties, fieldContainers, fieldUC, fieldSoftwareVersion)
	if err != nil {
		return
	}
	if !cnFound && !ucFound {
		return
	}

	sel := models.PullSecretSelector{ByHost: map[string]string{}}

	addSecret := func(svMap map[string]interface{}) {
		if svMap == nil {
			return
		}
		repo, _ := svMap[fieldRepository].(string)
		imgName, _ := svMap[fieldImageName].(string)
		fullRef := joinImageRef(repo, imgName)
		if fullRef == "" {
			return
		}
		host := models.RegistryHost(fullRef)
		secretName := firstPullSecretName(svMap)
		if secretName != "" {
			sel.ByHost[host] = secretName
		}
	}

	addSecret(cnSV)
	addSecret(ucSV)

	live.ImagePullSecrets = sel
	live.ImagePullSecretsSet = true
}

func firstPullSecretName(svMap map[string]interface{}) string {
	secrets, ok := svMap[fieldImagePullSecrets].([]interface{})
	if !ok || len(secrets) == 0 {
		return ""
	}
	first, ok := secrets[0].(map[string]interface{})
	if !ok {
		return ""
	}
	name, _ := first[fieldName].(string)
	return name
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
