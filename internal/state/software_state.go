// SPDX-License-Identifier: Apache-2.0

package state

import htime "helm.sh/helm/v3/pkg/time"

// GetSoftwareState returns the SoftwareState for the given component name from the state.
// Returns a zero-value SoftwareState with Name set if the component is not present.
func GetSoftwareState(s State, component string) SoftwareState {
	if s.MachineState.Software == nil {
		return SoftwareState{Name: component}
	}
	sw, ok := s.MachineState.Software[component]
	if !ok {
		return SoftwareState{Name: component}
	}
	return sw
}

// SetSoftwareState returns a copy of s with the given SoftwareState recorded under
// component. Callers must follow up with StateWriter.Set(updated).FlushState() to persist.
func SetSoftwareState(s State, component string, sw SoftwareState) State {
	if s.MachineState.Software == nil {
		s.MachineState.Software = make(map[string]SoftwareState)
	}
	sw.Name = component
	sw.LastSync = htime.Now()
	s.MachineState.Software[component] = sw
	return s
}
