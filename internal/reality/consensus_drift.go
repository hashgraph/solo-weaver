// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"fmt"
	"sort"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"k8s.io/apimachinery/pkg/api/resource"
)

// liveConsensusShape holds the managed-shape fields the reality checker reads back
// from a live ConsensusCapsule (and the Orbit). Every field here is one the capsule
// step always sets explicitly (never left to an operator-side default), so a
// non-empty live value is authoritative and an empty one means "not read" —
// compared only when non-empty to avoid false drift.
type liveConsensusShape struct {
	ImageRepo string
	ImageTag  string
	AccountId string
	Weight    int
	WeightSet bool
	LedgerId  string
	ChainId   string

	ContainerName string
	CPULimit      string
	CPURequest    string
	MemoryLimit   string
	MemoryRequest string
	JavaHeapMin   string
	JavaHeapMax   string
	JavaOpts      string
	UCImageRepo   string
	UCImageTag    string

	ProvisionerDaemonEnabled    bool
	ProvisionerDaemonEnabledSet bool
	Volumes                     models.ConsensusVolumeConfig
	VolumesSet                  bool
	ImagePullSecrets            models.PullSecretSelector
	ImagePullSecretsSet         bool
}

// diffConsensusManaged compares the persisted baseline (what weaver recorded at
// install) against the live shape read from the cluster, returning one
// human-readable entry per field that drifted. Identity fields are always
// compared; the managed sizing/JVM/UC fields are compared only when a managed
// spec was recorded (nil = a pre-#1187 state file with no baseline to diff).
// An empty live value is skipped (not read, not "changed to empty"). The result
// is for surfacing (logging / status), so the persisted baseline is never
// mutated — drift keeps being reported until the operator reconciles.
func diffConsensusManaged(ns state.ConsensusNodeState, live liveConsensusShape) []string {
	var drift []string
	add := func(field, stateVal, liveVal string) {
		if liveVal != "" && liveVal != stateVal {
			drift = append(drift, fmt.Sprintf("%s: state=%q live=%q", field, stateVal, liveVal))
		}
	}
	addQuantity := func(field, stateVal, liveVal string) {
		if liveVal != "" && quantityDiffers(stateVal, liveVal) {
			drift = append(drift, fmt.Sprintf("%s: state=%q live=%q", field, stateVal, liveVal))
		}
	}

	add("imageRepo", ns.ImageRepo, live.ImageRepo)
	add("imageTag", ns.ImageTag, live.ImageTag)
	add("accountId", ns.AccountId, live.AccountId)
	add("ledgerId", ns.LedgerId, live.LedgerId)
	add("chainId", ns.ChainId, live.ChainId)
	if live.WeightSet && live.Weight != ns.Weight {
		drift = append(drift, fmt.Sprintf("weight: state=%d live=%d", ns.Weight, live.Weight))
	}

	if ns.ManagedSpec == nil {
		return drift
	}
	m := ns.ManagedSpec
	add("containerName", m.ContainerName, live.ContainerName)
	addQuantity("cpuLimit", m.CPULimit, live.CPULimit)
	addQuantity("cpuRequest", m.CPURequest, live.CPURequest)
	addQuantity("memoryLimit", m.MemoryLimit, live.MemoryLimit)
	addQuantity("memoryRequest", m.MemoryRequest, live.MemoryRequest)
	add("javaHeapMin", m.JavaHeapMin, live.JavaHeapMin)
	add("javaHeapMax", m.JavaHeapMax, live.JavaHeapMax)
	add("javaOpts", m.JavaOpts, live.JavaOpts)
	add("ucImageRepo", m.UCImageRepo, live.UCImageRepo)
	add("ucImageTag", m.UCImageTag, live.UCImageTag)

	if live.ProvisionerDaemonEnabledSet && live.ProvisionerDaemonEnabled != m.ProvisionerDaemonEnabled {
		drift = append(drift, fmt.Sprintf("provisionerDaemonEnabled: state=%v live=%v",
			m.ProvisionerDaemonEnabled, live.ProvisionerDaemonEnabled))
	}

	if live.ImagePullSecretsSet {
		drift = append(drift, diffPullSecrets(m.ImagePullSecrets, live.ImagePullSecrets)...)
	}

	if live.VolumesSet {
		drift = append(drift, diffVolumes(m.Volumes, live.Volumes)...)
	}

	return drift
}

// diffPullSecrets compares persisted vs live pull-secret selectors.
func diffPullSecrets(state, live models.PullSecretSelector) []string {
	var drift []string
	if state.Default != live.Default {
		drift = append(drift, fmt.Sprintf("imagePullSecrets.default: state=%q live=%q",
			state.Default, live.Default))
	}

	allHosts := make(map[string]bool)
	for h := range state.ByHost {
		allHosts[h] = true
	}
	for h := range live.ByHost {
		allHosts[h] = true
	}
	hosts := make([]string, 0, len(allHosts))
	for h := range allHosts {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)

	for _, h := range hosts {
		sv := state.ByHost[h]
		lv := live.ByHost[h]
		if sv != lv {
			drift = append(drift, fmt.Sprintf("imagePullSecrets[%s]: state=%q live=%q", h, sv, lv))
		}
	}
	return drift
}

// diffVolumes compares persisted vs live volume configurations.
func diffVolumes(state, live models.ConsensusVolumeConfig) []string {
	var drift []string
	for _, name := range models.ConsensusVolumeNames() {
		sv := state.Volumes[name]
		lv, liveHas := live.Volumes[name]
		if !liveHas {
			continue
		}
		if sv.Type != lv.Type {
			drift = append(drift, fmt.Sprintf("volumes[%s].type: state=%q live=%q", name, sv.Type, lv.Type))
		}
		if sv.Path != lv.Path {
			drift = append(drift, fmt.Sprintf("volumes[%s].path: state=%q live=%q", name, sv.Path, lv.Path))
		}
		if quantityDiffers(sv.Size, lv.Size) {
			drift = append(drift, fmt.Sprintf("volumes[%s].size: state=%q live=%q", name, sv.Size, lv.Size))
		}
		if sv.StorageClass != lv.StorageClass {
			drift = append(drift, fmt.Sprintf("volumes[%s].storageClass: state=%q live=%q", name, sv.StorageClass, lv.StorageClass))
		}
		if sv.AccessMode != lv.AccessMode {
			drift = append(drift, fmt.Sprintf("volumes[%s].accessMode: state=%q live=%q", name, sv.AccessMode, lv.AccessMode))
		}
	}
	return drift
}

// quantityDiffers reports whether two Kubernetes quantity strings represent
// different amounts. It compares semantically when both parse (so "2" and
// "2000m", or "16Gi" canonicalised by the API server, are not spurious drift),
// falling back to a string compare when either side is not a valid quantity.
func quantityDiffers(stateVal, liveVal string) bool {
	qs, errS := resource.ParseQuantity(stateVal)
	ql, errL := resource.ParseQuantity(liveVal)
	if errS != nil || errL != nil {
		return stateVal != liveVal
	}
	return qs.Cmp(ql) != 0
}
