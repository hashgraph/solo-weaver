// SPDX-License-Identifier: Apache-2.0

package drift

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/config"
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
//   - Managed-spec fields (sizing, JVM, UC image, volumes, pull secrets) are
//     compared as ManagedSpec vs ObservedShape. ManagedSpec records what weaver
//     deliberately set at install and is never refreshed from the cluster;
//     ObservedShape is the transient readback the checker populates during
//     RefreshState.
//   - Orbit-level fields (the provisioner-daemon flag) are shared by every node
//     on the Orbit, so they are compared once per Orbit against ConsensusOrbits.
func ConsensusNode(baseline, live state.State) []Change {
	var changes []Change
	orbitsSeen := map[string]bool{}
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

		// Orbit-level drift: the Orbit is shared by its nodes, so compare it once.
		if liveNS.ObservedShape != nil && !orbitsSeen[liveNS.OrbitName] {
			orbitsSeen[liveNS.OrbitName] = true
			if want, ok := live.ConsensusOrbits[liveNS.OrbitName]; ok {
				changes = append(changes, compareConsensusOrbit(liveNS.OrbitName, want, liveNS.ObservedShape)...)
			}
		}
	}
	return changes
}

// compareConsensusOrbit reports Orbit-level fields that differ between the
// recorded ConsensusOrbitState and the live Orbit. It covers only fields weaver
// deliberately sets on the Orbit (ledgerId/chainId are already reported as node
// identity; operator defaults and the daemon-refreshed version annotations are
// not baselined). A node that could not read the Orbit (ProvisionerDaemonEnabledSet = false) is skipped.
func compareConsensusOrbit(orbit string, want state.ConsensusOrbitState, o *state.ConsensusNodeObservedShape) []Change {
	if !o.ProvisionerDaemonEnabledSet || o.ProvisionerDaemonEnabled == want.ProvisionerDaemonEnabled {
		return nil
	}
	return []Change{{
		Component: fmt.Sprintf("%s-orbit[%s]", consensusComponent, orbit),
		Field:     "provisionerDaemonEnabled",
		Persisted: strconv.FormatBool(want.ProvisionerDaemonEnabled),
		Live:      strconv.FormatBool(o.ProvisionerDaemonEnabled),
	}}
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
// the install-time ManagedSpec and the live ObservedShape. A non-nil
// ObservedShape means the checker read the capsule, so an empty scalar is a real
// "absent" value (a deleted field) and is compared. Composite fields the checker
// could not read (*Set = false) are skipped.
func compareConsensusManagedSpec(component string, m *state.ConsensusNodeManagedSpec, o *state.ConsensusNodeObservedShape) []Change {
	var changes []Change
	add := func(field, persisted, observed string) {
		if observed != persisted {
			changes = append(changes, Change{Component: component, Field: field, Persisted: persisted, Live: observed})
		}
	}
	addQuantity := func(field, persisted, observed string) {
		if quantityDiffers(persisted, observed) {
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

	if o.ImagePullSecretsSet {
		changes = append(changes, diffPullSecrets(component, m.ImagePullSecrets, o.ImagePullSecrets)...)
	}

	if o.VolumesSet {
		changes = append(changes, diffVolumes(component, m.Volumes, o.Volumes)...)
	}

	changes = append(changes, diffHostPathOwners(component, m, o.HostPathOwners)...)

	return changes
}

// diffHostPathOwners compares the uid/gid weaver chowns hostpath directories to
// (hostPathUid/hostPathGid, defaulting to the hedera user) with the on-disk owner.
func diffHostPathOwners(component string, m *state.ConsensusNodeManagedSpec, owners map[string]state.HostPathOwner) []Change {
	wantUID, _ := strconv.Atoi(config.HederaUserId())
	wantGID, _ := strconv.Atoi(config.HederaGroupId())
	if m.HostPathUID > 0 {
		wantUID = m.HostPathUID
	}
	if m.HostPathGID > 0 {
		wantGID = m.HostPathGID
	}

	names := make([]string, 0, len(owners))
	for n := range owners {
		names = append(names, n)
	}
	sort.Strings(names)

	var changes []Change
	for _, n := range names {
		o := owners[n]
		if o.UID != wantUID {
			changes = append(changes, Change{
				Component: component, Field: fmt.Sprintf("hostPathUid[%s]", n),
				Persisted: strconv.Itoa(wantUID), Live: strconv.Itoa(o.UID),
			})
		}
		if o.GID != wantGID {
			changes = append(changes, Change{
				Component: component, Field: fmt.Sprintf("hostPathGid[%s]", n),
				Persisted: strconv.Itoa(wantGID), Live: strconv.Itoa(o.GID),
			})
		}
	}
	return changes
}

// diffPullSecrets compares the secret each registry host resolves to. The live
// capsule only carries per-host secrets, so the persisted selector is resolved
// per host (ByHost entry, else Default) before comparing; comparing the raw
// selectors would flag a persisted default as drift on every run.
func diffPullSecrets(component string, persisted, live models.PullSecretSelector) []Change {
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

	var changes []Change
	for _, h := range hosts {
		want, ok := persisted.ByHost[h]
		if !ok {
			want = persisted.Default
		}
		if got := live.ByHost[h]; want != got {
			changes = append(changes, Change{
				Component: component, Field: fmt.Sprintf("imagePullSecrets[%s]", h),
				Persisted: want, Live: got,
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
