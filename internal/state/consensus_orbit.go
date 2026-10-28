// SPDX-License-Identifier: Apache-2.0

package state

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

// ConsensusOrbitState is the managed shape of an Orbit, keyed by Orbit name in
// StateRecord.ConsensusOrbits. An Orbit is shared by every consensus node that
// names it and is created once (by the first install), so facts that belong to
// the Orbit live here rather than on each node's ManagedSpec. It is both the
// install-time contract (a later install must request the same Orbit) and the
// baseline for Orbit-level drift detection. Only fields that change what a node
// runs against are tracked; operator tuning (upgrade, telemetry, lifetime,
// finalizers) and the daemon-refreshed version annotations are not.
type ConsensusOrbitState struct {
	ProvisionerDaemonEnabled bool                   `yaml:"provisionerDaemonEnabled" json:"provisionerDaemonEnabled"`
	LedgerId                 string                 `yaml:"ledgerId,omitempty" json:"ledgerId,omitempty"`
	ChainId                  string                 `yaml:"chainId,omitempty" json:"chainId,omitempty"`
	RealmId                  int                    `yaml:"realmId,omitempty" json:"realmId,omitempty"`
	ShardId                  int                    `yaml:"shardId,omitempty" json:"shardId,omitempty"`
	ProvisioningModel        string                 `yaml:"provisioningModel,omitempty" json:"provisioningModel,omitempty"`
	RequireDigestOnDeploy    bool                   `yaml:"requireDigestOnDeploy,omitempty" json:"requireDigestOnDeploy,omitempty"`
	DeploymentModel          string                 `yaml:"deploymentModel,omitempty" json:"deploymentModel,omitempty"`
	ExternalAddresses        []OrbitExternalAddress `yaml:"externalAddresses,omitempty" json:"externalAddresses,omitempty"`
}

// OrbitExternalAddress identifies one external address-book entry by node id and
// a digest of its full content, so a changed certificate or endpoint is detected
// without keeping PEM material in the state file.
type OrbitExternalAddress struct {
	NodeId int64  `yaml:"nodeId" json:"nodeId"`
	Digest string `yaml:"digest" json:"digest"`
}

// OrbitFieldDiff is one Orbit field whose value differs from the expected one.
type OrbitFieldDiff struct {
	Field string
	Want  string
	Live  string
}

// ConsensusOrbitStateFromSpec reads the tracked fields out of a live Orbit's
// spec map. The operator omits zero values, so an absent key is the zero value.
func ConsensusOrbitStateFromSpec(spec map[string]interface{}) ConsensusOrbitState {
	var o ConsensusOrbitState
	o.ProvisionerDaemonEnabled, _ = spec["provisionerDaemonEnabled"].(bool)
	o.RequireDigestOnDeploy, _ = spec["requireDigestOnDeploy"].(bool)
	o.DeploymentModel, _ = spec["deploymentModel"].(string)

	consensus, _ := spec["consensus"].(map[string]interface{})
	o.ProvisioningModel, _ = consensus["provisioningModel"].(string)
	genesis, _ := consensus["genesis"].(map[string]interface{})
	ab, _ := genesis["addressBook"].(map[string]interface{})
	o.LedgerId, _ = ab["ledgerId"].(string)
	o.ChainId, _ = ab["chainId"].(string)
	o.RealmId = specInt(ab["realmId"])
	o.ShardId = specInt(ab["shardId"])

	if list, ok := ab["externalAddresses"].([]interface{}); ok {
		for _, item := range list {
			entry, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			b, err := json.Marshal(entry) // map keys are sorted: deterministic
			if err != nil {
				continue
			}
			sum := sha256.Sum256(b)
			o.ExternalAddresses = append(o.ExternalAddresses, OrbitExternalAddress{
				NodeId: specInt64(entry["nodeId"]),
				Digest: fmt.Sprintf("%x", sum),
			})
		}
	}
	return o
}

func specInt(v interface{}) int { return int(specInt64(v)) }

// specInt64 reads a JSON number that the dynamic client may decode as int64 or float64.
func specInt64(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}

// Diff reports the tracked fields where live differs from o. An empty chainId is
// the CRD's "0", so the two are equal. External addresses are compared by node id
// and digest, reporting a missing, unexpected or changed entry per node.
func (o ConsensusOrbitState) Diff(live ConsensusOrbitState) []OrbitFieldDiff {
	var diffs []OrbitFieldDiff
	add := func(field, want, have string) {
		if want != have {
			diffs = append(diffs, OrbitFieldDiff{Field: field, Want: want, Live: have})
		}
	}
	add("provisionerDaemonEnabled", strconv.FormatBool(o.ProvisionerDaemonEnabled), strconv.FormatBool(live.ProvisionerDaemonEnabled))
	add("ledgerId", o.LedgerId, live.LedgerId)
	add("chainId", normalizeChainId(o.ChainId), normalizeChainId(live.ChainId))
	add("realmId", strconv.Itoa(o.RealmId), strconv.Itoa(live.RealmId))
	add("shardId", strconv.Itoa(o.ShardId), strconv.Itoa(live.ShardId))
	add("provisioningModel", o.ProvisioningModel, live.ProvisioningModel)
	add("requireDigestOnDeploy", strconv.FormatBool(o.RequireDigestOnDeploy), strconv.FormatBool(live.RequireDigestOnDeploy))
	add("deploymentModel", o.DeploymentModel, live.DeploymentModel)

	want := externalByNode(o.ExternalAddresses)
	have := externalByNode(live.ExternalAddresses)
	ids := make([]int64, 0, len(want)+len(have))
	for id := range want {
		ids = append(ids, id)
	}
	for id := range have {
		if _, ok := want[id]; !ok {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		w, wok := want[id]
		h, hok := have[id]
		field := fmt.Sprintf("externalAddresses[%d]", id)
		switch {
		case wok && !hok:
			diffs = append(diffs, OrbitFieldDiff{Field: field, Want: "present", Live: "absent"})
		case !wok && hok:
			diffs = append(diffs, OrbitFieldDiff{Field: field, Want: "absent", Live: "present"})
		case w != h:
			diffs = append(diffs, OrbitFieldDiff{Field: field, Want: "recorded content", Live: "changed content"})
		}
	}
	return diffs
}

func normalizeChainId(v string) string {
	if v == "" {
		return "0"
	}
	return v
}

func externalByNode(list []OrbitExternalAddress) map[int64]string {
	m := make(map[int64]string, len(list))
	for _, e := range list {
		m[e.NodeId] = e.Digest
	}
	return m
}
