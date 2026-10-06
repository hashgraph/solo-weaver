// SPDX-License-Identifier: Apache-2.0

package drift

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"k8s.io/apimachinery/pkg/api/resource"
)

const consensusComponent = "consensus"

// ConsensusNode reports consensus-node fields that differ between the persisted
// state.yaml baseline and the live cluster. It covers two kinds of drift:
//
//   - Identity fields (imageRepo, imageTag, accountId, …) are compared as
//     baseline vs live, because the reality checker absorbs the live value into
//     ConsensusNodeState — identical to the Teleport pattern.
//   - Managed-spec fields (sizing, JVM, UC image, volumes, pull secrets, daemon
//     flag) are compared as ManagedSpec vs ObservedShape. ManagedSpec records
//     what weaver deliberately set at install and is never refreshed from the
//     cluster; ObservedShape is the transient readback the checker populates
//     during RefreshState.
func ConsensusNode(baseline, live state.State) []Change {
	var changes []Change
	for scope, liveNS := range live.ConsensusNodes {
		component := fmt.Sprintf("%s[%s]", consensusComponent, scope)
		baseNS, inBaseline := baseline.ConsensusNodes[scope]

		// Identity drift: baseline vs live (only for nodes that existed before).
		if inBaseline {
			changes = append(changes, compareConsensusIdentity(component, baseNS, liveNS)...)
		}

		// Managed-spec drift: ManagedSpec vs ObservedShape.
		if liveNS.ManagedSpec != nil && liveNS.ObservedShape != nil {
			changes = append(changes, compareConsensusManagedSpec(component, liveNS.ManagedSpec, liveNS.ObservedShape)...)
		}
	}
	return changes
}

// compareConsensusIdentity reports identity fields that differ between the
// persisted baseline and the reality-refreshed live state.
func compareConsensusIdentity(component string, baseline, live state.ConsensusNodeState) []Change {
	var changes []Change
	add := func(field, persisted, observed string) {
		if observed != "" && observed != persisted {
			changes = append(changes, Change{Component: component, Field: field, Persisted: persisted, Live: observed})
		}
	}
	add("imageRepo", baseline.ImageRepo, live.ImageRepo)
	add("imageTag", baseline.ImageTag, live.ImageTag)
	add("accountId", baseline.AccountId, live.AccountId)
	add("ledgerId", baseline.LedgerId, live.LedgerId)
	add("chainId", baseline.ChainId, live.ChainId)
	if live.Weight != baseline.Weight {
		changes = append(changes, Change{
			Component: component, Field: "weight",
			Persisted: strconv.Itoa(baseline.Weight), Live: strconv.Itoa(live.Weight),
		})
	}
	return changes
}

// compareConsensusManagedSpec reports managed-shape fields that differ between
// the install-time ManagedSpec and the live ObservedShape. Fields the checker
// could not read (empty string / *Set = false) are skipped.
func compareConsensusManagedSpec(component string, m *state.ConsensusNodeManagedSpec, o *state.ConsensusNodeObservedShape) []Change {
	var changes []Change
	add := func(field, persisted, observed string) {
		if observed != "" && observed != persisted {
			changes = append(changes, Change{Component: component, Field: field, Persisted: persisted, Live: observed})
		}
	}
	addQuantity := func(field, persisted, observed string) {
		if observed != "" && quantityDiffers(persisted, observed) {
			changes = append(changes, Change{Component: component, Field: field, Persisted: persisted, Live: observed})
		}
	}

	add("containerName", m.ContainerName, o.ContainerName)
	addQuantity("cpuLimit", m.CPULimit, o.CPULimit)
	addQuantity("cpuRequest", m.CPURequest, o.CPURequest)
	addQuantity("memoryLimit", m.MemoryLimit, o.MemoryLimit)
	addQuantity("memoryRequest", m.MemoryRequest, o.MemoryRequest)
	add("javaHeapMin", m.JavaHeapMin, o.JavaHeapMin)
	add("javaHeapMax", m.JavaHeapMax, o.JavaHeapMax)
	add("javaOpts", m.JavaOpts, o.JavaOpts)
	add("ucImageRepo", m.UCImageRepo, o.UCImageRepo)
	add("ucImageTag", m.UCImageTag, o.UCImageTag)

	if o.ProvisionerDaemonEnabledSet && o.ProvisionerDaemonEnabled != m.ProvisionerDaemonEnabled {
		changes = append(changes, Change{
			Component: component, Field: "provisionerDaemonEnabled",
			Persisted: strconv.FormatBool(m.ProvisionerDaemonEnabled),
			Live:      strconv.FormatBool(o.ProvisionerDaemonEnabled),
		})
	}

	if o.ImagePullSecretsSet {
		changes = append(changes, diffPullSecrets(component, m.ImagePullSecrets, o.ImagePullSecrets)...)
	}

	if o.VolumesSet {
		changes = append(changes, diffVolumes(component, m.Volumes, o.Volumes)...)
	}

	return changes
}

// diffPullSecrets compares persisted vs live pull-secret selectors.
func diffPullSecrets(component string, persisted, live models.PullSecretSelector) []Change {
	var changes []Change
	if persisted.Default != live.Default {
		changes = append(changes, Change{
			Component: component, Field: "imagePullSecrets.default",
			Persisted: persisted.Default, Live: live.Default,
		})
	}

	allHosts := make(map[string]bool)
	for h := range persisted.ByHost {
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
		sv := persisted.ByHost[h]
		lv := live.ByHost[h]
		if sv != lv {
			changes = append(changes, Change{
				Component: component, Field: fmt.Sprintf("imagePullSecrets[%s]", h),
				Persisted: sv, Live: lv,
			})
		}
	}
	return changes
}

// diffVolumes compares persisted vs live volume configurations.
func diffVolumes(component string, persisted, live models.ConsensusVolumeConfig) []Change {
	var changes []Change
	for _, name := range models.ConsensusVolumeNames() {
		sv := persisted.Volumes[name]
		lv, liveHas := live.Volumes[name]
		if !liveHas {
			continue
		}
		if sv.Type != lv.Type {
			changes = append(changes, Change{
				Component: component, Field: fmt.Sprintf("volumes[%s].type", name),
				Persisted: string(sv.Type), Live: string(lv.Type),
			})
		}
		if sv.Path != lv.Path {
			changes = append(changes, Change{
				Component: component, Field: fmt.Sprintf("volumes[%s].path", name),
				Persisted: sv.Path, Live: lv.Path,
			})
		}
		if quantityDiffers(sv.Size, lv.Size) {
			changes = append(changes, Change{
				Component: component, Field: fmt.Sprintf("volumes[%s].size", name),
				Persisted: sv.Size, Live: lv.Size,
			})
		}
		if sv.StorageClass != lv.StorageClass {
			changes = append(changes, Change{
				Component: component, Field: fmt.Sprintf("volumes[%s].storageClass", name),
				Persisted: sv.StorageClass, Live: lv.StorageClass,
			})
		}
		if sv.AccessMode != lv.AccessMode {
			changes = append(changes, Change{
				Component: component, Field: fmt.Sprintf("volumes[%s].accessMode", name),
				Persisted: sv.AccessMode, Live: lv.AccessMode,
			})
		}
	}
	return changes
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
