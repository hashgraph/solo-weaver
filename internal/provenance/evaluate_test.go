// SPDX-License-Identifier: Apache-2.0

package provenance

import (
	"testing"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/stretchr/testify/require"
)

// TestEvaluate_Outcomes walks every rule in design section 5.2 and asserts the
// outcome: the rule id that fired, whether it stopped, and for a pass the Origin,
// for a stop the reason code an operator would see. This is the authoritative
// demonstration of what the gate blocks.
func TestEvaluate_Outcomes(t *testing.T) {
	tests := []struct {
		name       string
		in         Input
		wantRule   string
		wantStop   bool
		wantOrigin Origin // checked when not a stop
		wantReason errx.Reason
	}{
		{
			name:       "P.1 copied state dir",
			in:         Input{RecordedMachineIDPresent: true, MachineIDMatches: false, HasProvisionRecord: true, TargetMatchesProvisionRecord: true},
			wantRule:   "P.1",
			wantStop:   true,
			wantReason: reasons.PreconditionNotMet,
		},
		{
			name:       "A.1 broken local cluster, never external",
			in:         Input{RecordedMachineIDPresent: true, MachineIDMatches: true, HasProvisionRecord: true, TargetMatchesProvisionRecord: true, LocalEvidenceFailed: true, APIReachable: false},
			wantRule:   "A.1",
			wantStop:   true,
			wantReason: reasons.PreconditionNotMet,
		},
		{
			name:       "A.1 API unreachable on our cluster",
			in:         Input{RecordedMachineIDPresent: true, MachineIDMatches: true, HasProvisionRecord: true, TargetMatchesProvisionRecord: true, LocalEvidencePasses: true, APIReachable: false},
			wantRule:   "A.1",
			wantStop:   true,
			wantReason: reasons.PreconditionNotMet,
		},
		{
			name:       "A.2 needs sudo",
			in:         Input{RecordedMachineIDPresent: true, MachineIDMatches: true, HasProvisionRecord: true, TargetMatchesProvisionRecord: true, LocalEvidenceUnreadable: true, APIReachable: true},
			wantRule:   "A.2",
			wantStop:   true,
			wantReason: reasons.PermissionDenied,
		},
		{
			name:       "A.3 provisioned, ids match",
			in:         Input{RecordedMachineIDPresent: true, MachineIDMatches: true, HasProvisionRecord: true, TargetMatchesProvisionRecord: true, LocalEvidencePasses: true, APIReachable: true, LiveClusterID: "u1", ProvisionedClusterID: "u1"},
			wantRule:   "A.3",
			wantOrigin: Provisioned,
		},
		{
			name:       "A.4 rebuilt, stale state",
			in:         Input{RecordedMachineIDPresent: true, MachineIDMatches: true, HasProvisionRecord: true, TargetMatchesProvisionRecord: true, LocalEvidencePasses: true, APIReachable: true, LiveClusterID: "u-new", ProvisionedClusterID: "u-old"},
			wantRule:   "A.4",
			wantStop:   true,
			wantReason: reasons.PreconditionNotMet,
		},
		{
			name:       "A.1 wins over A.2 when both fail and unreadable",
			in:         Input{RecordedMachineIDPresent: true, MachineIDMatches: true, HasProvisionRecord: true, TargetMatchesProvisionRecord: true, LocalEvidenceFailed: true, LocalEvidenceUnreadable: true, APIReachable: true},
			wantRule:   "A.1",
			wantStop:   true,
			wantReason: reasons.PreconditionNotMet,
		},
		{
			name:       "B.1 nothing recorded, API down",
			in:         Input{HasProvisionRecord: false, LocalEvidencePasses: true, APIReachable: false},
			wantRule:   "B.1",
			wantStop:   true,
			wantReason: reasons.PreconditionNotMet,
		},
		{
			name:       "B.2 needs adopt/discard",
			in:         Input{HasProvisionRecord: false, LocalEvidencePasses: true, APIReachable: true, LiveClusterID: "u2", ExternalStateExists: true},
			wantRule:   "B.2",
			wantStop:   true,
			wantReason: reasons.PreconditionNotMet,
		},
		{
			name:       "B.3 adopt on first use",
			in:         Input{HasProvisionRecord: false, LocalEvidencePasses: true, APIReachable: true, LiveClusterID: "u3"},
			wantRule:   "B.3",
			wantOrigin: Provisioned,
		},
		{
			name:       "C.1 external with kubeconfig",
			in:         Input{HasProvisionRecord: false, LocalEvidencePasses: false, KubeconfigGiven: true},
			wantRule:   "C.1",
			wantOrigin: External,
		},
		{
			name:       "C.1 external even on a provisioned host pointed elsewhere",
			in:         Input{HasProvisionRecord: true, TargetMatchesProvisionRecord: false, KubeconfigGiven: true},
			wantRule:   "C.1",
			wantOrigin: External,
		},
		{
			name:       "C.2 nothing here, no kubeconfig",
			in:         Input{HasProvisionRecord: false, LocalEvidencePasses: false, KubeconfigGiven: false},
			wantRule:   "C.2",
			wantStop:   true,
			wantReason: reasons.NotInstalled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(tt.in)
			require.Equal(t, tt.wantRule, got.Rule, "rule")
			if tt.wantStop {
				require.True(t, got.Stopped(), "expected a stop")
				rsn, ok := errx.ReasonOf(got.Err)
				require.True(t, ok, "stop must carry a reason code")
				require.Equal(t, tt.wantReason, rsn, "reason")
				hints, ok := errx.Hints(got.Err)
				require.True(t, ok, "stop must carry operator hints")
				require.NotEmpty(t, hints)
			} else {
				require.False(t, got.Stopped(), "expected a pass")
				require.Equal(t, tt.wantOrigin, got.Origin, "origin")
			}
		})
	}
}

// TestGate_HostAPI_RefusesExternal shows a host+api command (block node install)
// turning an otherwise-valid external classification into a stop.
func TestGate_HostAPI_RefusesExternal(t *testing.T) {
	ext := Input{HasProvisionRecord: false, LocalEvidencePasses: false, KubeconfigGiven: true}

	api := Gate(NeedAPI, ext)
	require.False(t, api.Stopped(), "api accepts external")
	require.Equal(t, External, api.Origin)

	got := Gate(NeedHostAPI, ext)
	require.True(t, got.Stopped(), "host+api refuses external")
	require.Equal(t, "host+api", got.Rule)
	rsn, ok := errx.ReasonOf(got.Err)
	require.True(t, ok)
	require.Equal(t, reasons.PreconditionNotMet, rsn)
}

// TestGate_Host_OfflineOnly shows the host need ignoring a down cluster but still
// refusing a copied state dir and an external target.
func TestGate_Host_OfflineOnly(t *testing.T) {
	// Down cluster, but host work proceeds.
	down := Input{RecordedMachineIDPresent: true, MachineIDMatches: true, APIReachable: false}
	require.False(t, Gate(NeedHost, down).Stopped())

	// Copied state dir still stops.
	copied := Input{RecordedMachineIDPresent: true, MachineIDMatches: false}
	require.True(t, Gate(NeedHost, copied).Stopped())

	// External target stops a host command.
	ext := Input{KubeconfigGiven: true}
	require.True(t, Gate(NeedHost, ext).Stopped())
}

// TestScenarios_AllDecide makes sure every demo scenario maps to an outcome under
// host+api, so the table never hits an unhandled case.
func TestScenarios_AllDecide(t *testing.T) {
	for _, name := range ScenarioNames() {
		s, ok := ScenarioInput(name)
		require.True(t, ok)
		got := Gate(NeedHostAPI, s.In)
		if got.Stopped() {
			require.Error(t, got.Err, "scenario %s", name)
		}
	}
}
