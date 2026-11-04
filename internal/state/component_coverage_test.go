// SPDX-License-Identifier: Apache-2.0

package state

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

// Fails when a field is added to StateRecord, or to MachineState, which is
// split across three files, without deciding which component file holds it:
// applyComponentSection copies fields by hand, so a field it misses would
// silently never be saved.
func TestEveryStateFieldIsInExactlyOneComponentFile(t *testing.T) {
	record := reflect.TypeFor[StateRecord]()
	for i := range record.NumField() {
		f := record.Field(i)
		if f.Name == "MachineState" {
			machine := f.Type
			for j := range machine.NumField() {
				checkCarriedOnce(t, "MachineState."+machine.Field(j).Name, func(s *State) reflect.Value {
					return reflect.ValueOf(&s.MachineState).Elem().Field(j)
				})
			}
			continue
		}
		checkCarriedOnce(t, f.Name, func(s *State) reflect.Value {
			return reflect.ValueOf(&s.StateRecord).Elem().Field(i)
		})
	}
}

// checkCarriedOnce sets the field field() selects in an otherwise empty State
// and requires exactly one component's projection to keep it.
func checkCarriedOnce(t *testing.T, name string, field func(*State) reflect.Value) {
	t.Helper()
	var full State
	fillNonZero(field(&full), 0)
	require.Falsef(t, field(&full).IsZero(), "could not give %s a non-zero value", name)

	var carriers []ComponentID
	for _, id := range AllComponentIDs {
		projected := projectComponentSection(full, id)
		if !field(&projected).IsZero() {
			carriers = append(carriers, id)
		}
	}

	require.Lenf(t, carriers, 1,
		"%s must be written by exactly one component file (see applyComponentSection); written by %v",
		name, carriers)
}
