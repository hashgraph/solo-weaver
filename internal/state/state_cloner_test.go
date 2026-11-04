// SPDX-License-Identifier: Apache-2.0

package state

import (
	"reflect"
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/require"
	htime "helm.sh/helm/v3/pkg/time"
)

// Fails when Clone drops a StateRecord field. Refresh starts from a clone, so a
// dropped field loses its in-memory value for any component with no file yet.
func TestStateClone_KeepsEveryStateRecordField(t *testing.T) {
	var original State
	fillNonZero(reflect.ValueOf(&original.StateRecord).Elem(), 0)

	clone, err := original.Clone()
	require.NoError(t, err)
	require.Equal(t, original.StateRecord, clone.StateRecord)
}

// fillNonZero sets v, and every exported field beneath it, to a non-zero value.
func fillNonZero(v reflect.Value, depth int) {
	if depth > 6 || !v.CanSet() {
		return
	}
	switch v.Type() {
	case reflect.TypeFor[htime.Time]():
		v.Set(reflect.ValueOf(htime.Now()))
		return
	case reflect.TypeFor[time.Time]():
		v.Set(reflect.ValueOf(time.Now()))
		return
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString("x")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1)
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fillNonZero(v.Elem(), depth+1)
	case reflect.Struct:
		for i := range v.NumField() {
			fillNonZero(v.Field(i), depth+1)
		}
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		key := reflect.New(v.Type().Key()).Elem()
		fillNonZero(key, depth+1)
		val := reflect.New(v.Type().Elem()).Elem()
		fillNonZero(val, depth+1)
		m.SetMapIndex(key, val)
		v.Set(m)
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 1, 1)
		fillNonZero(s.Index(0), depth+1)
		v.Set(s)
	}
}

// Fails when a consensus node's clone shares a pointer or map with the
// original, so editing the clone would also edit the manager's baseline.
func TestConsensusNodeStateClone_SharesNothing(t *testing.T) {
	var original ConsensusNodeState
	fillNonZero(reflect.ValueOf(&original).Elem(), 0)

	clone := original.Clone()
	require.Equal(t, original, *clone)

	clone.ManagedSpec.CPULimit = "changed"
	for k := range clone.ManagedSpec.ImagePullSecrets.ByHost {
		clone.ManagedSpec.ImagePullSecrets.ByHost[k] = "changed"
	}
	for k := range clone.ManagedSpec.Volumes.Volumes {
		clone.ManagedSpec.Volumes.Volumes[k] = models.ConsensusVolumeSpec{Path: "changed"}
	}
	clone.ObservedShape.CPULimit = "changed"
	clone.ObservedShape.Orbit.ExternalAddresses[0] = OrbitExternalAddress{}
	for k := range clone.ObservedShape.HostPathOwners {
		clone.ObservedShape.HostPathOwners[k] = HostPathOwner{UID: 99}
	}
	for k := range clone.ObservedShape.Volumes.Volumes {
		clone.ObservedShape.Volumes.Volumes[k] = models.ConsensusVolumeSpec{Path: "changed"}
	}
	for k := range clone.ObservedShape.ImagePullSecrets.ByHost {
		clone.ObservedShape.ImagePullSecrets.ByHost[k] = "changed"
	}

	var untouched ConsensusNodeState
	fillNonZero(reflect.ValueOf(&untouched).Elem(), 0)
	require.Equal(t, untouched.ManagedSpec, original.ManagedSpec)
	require.Equal(t, untouched.ObservedShape.CPULimit, original.ObservedShape.CPULimit)
	require.Equal(t, untouched.ObservedShape.Orbit.ExternalAddresses, original.ObservedShape.Orbit.ExternalAddresses)
	require.Equal(t, untouched.ObservedShape.HostPathOwners, original.ObservedShape.HostPathOwners)
	require.Equal(t, untouched.ObservedShape.Volumes, original.ObservedShape.Volumes)
	require.Equal(t, untouched.ObservedShape.ImagePullSecrets, original.ObservedShape.ImagePullSecrets)
}
