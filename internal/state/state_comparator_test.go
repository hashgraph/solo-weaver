// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package state

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
	htime "helm.sh/helm/v3/pkg/time"
)

// deployedBlockNode is the persisted state of a block node after a clean install.
func deployedBlockNode() BlockNodeState {
	return BlockNodeState{
		ReleaseInfo: HelmReleaseInfo{
			Name:         "block-node",
			ChartVersion: "0.40.0",
			AppVersion:   "0.40.0",
			Namespace:    "block-node",
			ChartRef:     "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server",
			ChartName:    "block-node-server",
			Status:       release.StatusDeployed,
		},
		Storage: models.BlockNodeStorage{
			LivePath:    "/mnt/fast-storage/live",
			LiveSize:    "20Gi",
			ArchivePath: "/mnt/fast-storage/archive",
			ArchiveSize: "50Gi",
		},
	}
}

// A hand-run helm upgrade moves the chart version and the deploy time, and every
// refresh stamps LastSync; only the version is drift.
func TestBlockNodeDiff_ChartUpgradedOutOfBand(t *testing.T) {
	persisted := deployedBlockNode()
	persisted.ReleaseInfo.LastDeployed = htime.Unix(1_700_000_000, 0)
	persisted.LastSync = htime.Unix(1_700_000_000, 0)
	reality := deployedBlockNode()
	reality.ReleaseInfo.ChartVersion = "0.41.0"
	reality.ReleaseInfo.LastDeployed = htime.Unix(1_700_086_400, 0)
	reality.LastSync = htime.Unix(1_700_086_400, 0)

	require.Equal(t, []FieldDiff{
		{Field: "version", Persisted: "0.40.0", Reality: "0.41.0", Observable: true},
	}, persisted.Diff(reality))
}

func TestBlockNodeDiff_ReleaseStatusCounts(t *testing.T) {
	persisted := deployedBlockNode()
	reality := deployedBlockNode()
	reality.ReleaseInfo.Status = release.StatusFailed

	require.Equal(t, []FieldDiff{
		{Field: "status", Persisted: "deployed", Reality: "failed", Observable: true},
	}, persisted.Diff(reality))
}

// Reality cannot read these back, so a refresh that rebuilds the struct without
// them differs from the persisted state, but none of the differences is drift.
func TestBlockNodeDiff_WeaverOnlyRecordsAreNotObservable(t *testing.T) {
	persisted := deployedBlockNode()
	persisted.Storage.BasePath = "/mnt/fast-storage"
	persisted.PluginPreset = "tier1-lfh"
	persisted.PluginList = "facility-messaging,stream-publisher"
	persisted.TrafficShapingDisabled = true
	reality := deployedBlockNode()
	reality.ReleaseInfo.ChartRef = ""

	require.ElementsMatch(t, []FieldDiff{
		{Field: "chartRef", Persisted: "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server", Reality: "", Observable: false},
		{Field: "storage.basePath", Persisted: "/mnt/fast-storage", Reality: "", Observable: false},
		{Field: "pluginPreset", Persisted: "tier1-lfh", Reality: "", Observable: false},
		{Field: "pluginList", Persisted: "facility-messaging,stream-publisher", Reality: "", Observable: false},
		{Field: "trafficShapingDisabled", Persisted: "true", Reality: "false", Observable: false},
	}, persisted.Diff(reality))
}

func TestBlockNodeDiff_ShapingComparesByContent(t *testing.T) {
	prio1, prio2 := 1, 2
	shaping := func() *ShapingState {
		return &ShapingState{
			EgressInterface: "eth0",
			LinkRate:        "1gbit",
			ShapeOverrides: map[string]models.ShapeOverride{
				"publisher": {Rate: "800mbit", Ceil: "1gbit", Prio: &prio1},
			},
		}
	}
	cases := []struct {
		name               string
		persisted, reality *ShapingState
		want               []FieldDiff
	}{
		{
			name:      "never set and set to nothing are the same",
			persisted: nil,
			reality:   &ShapingState{},
		},
		{
			name:      "same content behind different pointers",
			persisted: shaping(),
			reality:   shaping(),
		},
		{
			name:      "link rate changed",
			persisted: shaping(),
			reality:   func() *ShapingState { s := shaping(); s.LinkRate = "10gbit"; return s }(),
			want: []FieldDiff{
				{Field: "shaping.linkRate", Persisted: "1gbit", Reality: "10gbit"},
			},
		},
		{
			name:      "override rate and prio changed",
			persisted: shaping(),
			reality: func() *ShapingState {
				s := shaping()
				s.ShapeOverrides["publisher"] = models.ShapeOverride{Rate: "500mbit", Ceil: "1gbit", Prio: &prio2}
				return s
			}(),
			want: []FieldDiff{
				{Field: "shaping.shapeOverrides.publisher.rate", Persisted: "800mbit", Reality: "500mbit"},
				{Field: "shaping.shapeOverrides.publisher.prio", Persisted: "1", Reality: "2"},
			},
		},
		{
			name:      "shaping dropped",
			persisted: shaping(),
			reality:   nil,
			want: []FieldDiff{
				{Field: "shaping.egressInterface", Persisted: "eth0", Reality: ""},
				{Field: "shaping.linkRate", Persisted: "1gbit", Reality: ""},
				{Field: "shaping.shapeOverrides.publisher.rate", Persisted: "800mbit", Reality: ""},
				{Field: "shaping.shapeOverrides.publisher.ceil", Persisted: "1gbit", Reality: ""},
				{Field: "shaping.shapeOverrides.publisher.prio", Persisted: "1", Reality: ""},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			persisted, reality := deployedBlockNode(), deployedBlockNode()
			persisted.Shaping, reality.Shaping = tc.persisted, tc.reality

			require.ElementsMatch(t, tc.want, persisted.Diff(reality))
		})
	}
}

func TestBlockNodeDiff_LoadBalancerComparedOnlyWhenBothKnown(t *testing.T) {
	enabled, disabled := true, false
	cases := []struct {
		name               string
		persisted, reality *bool
		want               []FieldDiff
	}{
		{name: "both unknown", persisted: nil, reality: nil},
		{name: "persisted unknown", persisted: nil, reality: &enabled},
		{name: "reality unknown", persisted: &disabled, reality: nil},
		{name: "both known and the same", persisted: &enabled, reality: &enabled},
		{
			name:      "enabled out of band",
			persisted: &disabled,
			reality:   &enabled,
			want: []FieldDiff{
				{Field: "loadBalancerEnabled", Persisted: "false", Reality: "true", Observable: true},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			persisted, reality := deployedBlockNode(), deployedBlockNode()
			persisted.LoadBalancerEnabled, reality.LoadBalancerEnabled = tc.persisted, tc.reality

			require.Equal(t, tc.want, persisted.Diff(reality))
		})
	}
}

func TestBlockNodeEqual_IsAnEmptyDiff(t *testing.T) {
	cases := []struct {
		name   string
		change func(*BlockNodeState)
		equal  bool
	}{
		{"size spelled differently", func(b *BlockNodeState) { b.Storage.LiveSize = "20480Mi" }, true},
		{"deploy time and last sync moved", func(b *BlockNodeState) {
			b.ReleaseInfo.LastDeployed = htime.Unix(1_700_086_400, 0)
			b.LastSync = htime.Unix(1_700_086_400, 0)
		}, true},
		{"plugin preset differs", func(b *BlockNodeState) { b.PluginPreset = "tier1-lfh" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			persisted, other := deployedBlockNode(), deployedBlockNode()
			tc.change(&other)

			require.Equal(t, tc.equal, persisted.Equal(other))
			require.Equal(t, tc.equal, other.Equal(persisted))
		})
	}
}

// Every leaf field of BlockNodeState is compared and named by its state.yaml
// key, except the timestamps, so a field added later fails here until the diff
// covers it.
func TestBlockNodeDiff_CoversEveryField(t *testing.T) {
	for _, leaf := range leafFields(reflect.TypeFor[BlockNodeState](), "BlockNodeState", nil, nil) {
		t.Run(leaf.name, func(t *testing.T) {
			var persisted, reality BlockNodeState
			setLeaf(t, reflect.ValueOf(&persisted).Elem(), leaf.steps, 0)
			setLeaf(t, reflect.ValueOf(&reality).Elem(), leaf.steps, 1)

			diffs := persisted.Diff(reality)
			if leaf.isTime {
				require.Empty(t, diffs)
				return
			}
			require.Len(t, diffs, 1)
			require.Equal(t, strings.Join(leaf.key, "."), diffs[0].Field)
			require.Equal(t, slices.Contains(BlockNodeObservableFields(), diffs[0].Field), diffs[0].Observable,
				"BlockNodeObservableFields and the entry's Observable flag disagree")
		})
	}
}

// overrideClass is the map key setLeaf uses for a field inside a map entry.
const overrideClass = "publisher"

// leafField is a leaf of a state struct: key is its state.yaml key path, and
// steps are the struct field indexes to reach it, with -1 stepping into the
// overrideClass entry of a map.
type leafField struct {
	name   string
	key    []string
	steps  []int
	isTime bool
}

func leafFields(t reflect.Type, name string, key []string, steps []int) []leafField {
	switch {
	case t == reflect.TypeFor[htime.Time]():
		return []leafField{{name: name, key: key, steps: steps, isTime: true}}
	case t.Kind() == reflect.Struct:
		var leaves []leafField
		for i := range t.NumField() {
			f := t.Field(i)
			fieldKey := key
			if tag := f.Tag.Get("yaml"); !strings.Contains(tag, "inline") {
				yamlName, _, _ := strings.Cut(tag, ",")
				if yamlName == "" {
					yamlName = strings.ToLower(f.Name)
				}
				fieldKey = append(slices.Clone(key), yamlName)
			}
			leaves = append(leaves, leafFields(f.Type, name+"."+f.Name, fieldKey, append(slices.Clone(steps), i))...)
		}
		return leaves
	case t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Struct:
		return leafFields(t.Elem(), name, key, steps)
	case t.Kind() == reflect.Map:
		return leafFields(t.Elem(), name+"["+overrideClass+"]", append(slices.Clone(key), overrideClass), append(slices.Clone(steps), -1))
	default:
		return []leafField{{name: name, key: key, steps: steps}}
	}
}

// setLeaf gives the leaf at steps the value for side 0 or side 1, which always
// differ, allocating the pointers and map entries on the way so that neither
// side is unknown.
func setLeaf(t *testing.T, v reflect.Value, steps []int, side int) {
	t.Helper()
	switch {
	case v.Kind() == reflect.Pointer:
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		setLeaf(t, v.Elem(), steps, side)
	case v.Kind() == reflect.Map:
		if v.IsNil() {
			v.Set(reflect.MakeMap(v.Type()))
		}
		entry := reflect.New(v.Type().Elem()).Elem()
		setLeaf(t, entry, steps[1:], side)
		v.SetMapIndex(reflect.ValueOf(overrideClass), entry)
	case len(steps) > 0:
		setLeaf(t, v.Field(steps[0]), steps[1:], side)
	case v.Type() == reflect.TypeFor[htime.Time]():
		v.Set(reflect.ValueOf(htime.Unix(1_700_000_000+int64(side)*86_400, 0)))
	case v.Kind() == reflect.String:
		v.SetString([]string{"x", "y"}[side])
	case v.Kind() == reflect.Bool:
		v.SetBool(side == 1)
	case v.Kind() == reflect.Int:
		v.SetInt(int64(side) + 1)
	default:
		t.Fatalf("setLeaf: no non-zero value for a %s field", v.Type())
	}
}

func TestBlockNodeDiff_StorageSizesCompareAsQuantities(t *testing.T) {
	sizes := map[string]func(*models.BlockNodeStorage) *string{
		"storage.liveSize":             func(s *models.BlockNodeStorage) *string { return &s.LiveSize },
		"storage.archiveSize":          func(s *models.BlockNodeStorage) *string { return &s.ArchiveSize },
		"storage.logSize":              func(s *models.BlockNodeStorage) *string { return &s.LogSize },
		"storage.verificationSize":     func(s *models.BlockNodeStorage) *string { return &s.VerificationSize },
		"storage.pluginsSize":          func(s *models.BlockNodeStorage) *string { return &s.PluginsSize },
		"storage.applicationStateSize": func(s *models.BlockNodeStorage) *string { return &s.ApplicationStateSize },
	}
	cases := []struct {
		name               string
		persisted, reality string
		drift              bool
	}{
		{"same size spelled differently", "20Gi", "20480Mi", false},
		{"resized", "20Gi", "30Gi", true},
		{"volume gone", "20Gi", "", true},
		{"volume added", "", "20Gi", true},
		{"unparseable size", "20Gi", "twenty", true},
	}
	for field, size := range sizes {
		for _, tc := range cases {
			t.Run(field+"/"+tc.name, func(t *testing.T) {
				var persisted, reality BlockNodeState
				*size(&persisted.Storage) = tc.persisted
				*size(&reality.Storage) = tc.reality

				var want []FieldDiff
				if tc.drift {
					want = []FieldDiff{{Field: field, Persisted: tc.persisted, Reality: tc.reality, Observable: true}}
				}
				require.Equal(t, want, persisted.Diff(reality))
			})
		}
	}
}
