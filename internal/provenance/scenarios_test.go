// SPDX-License-Identifier: Apache-2.0

package provenance

import "sort"

// Scenario is a named, canned Input covering one outcome. It is test-only (this
// file is a _test.go, so it is never compiled into a shipped binary): there is
// deliberately no way to inject a canned verdict into the real gate at runtime.
type Scenario struct {
	Name string
	Desc string
	In   Input
}

// scenarios covers every rule the gate can fire for a host+api command. The
// "ok" scenarios pass; the rest stop.
var scenarios = map[string]Scenario{
	"provisioned-ok": {
		Name: "provisioned-ok",
		Desc: "healthy provisioned host, live cluster matches the recorded one (A.3)",
		In: Input{
			RecordedMachineIDPresent: true, MachineIDMatches: true,
			HasProvisionRecord: true, TargetMatchesProvisionRecord: true,
			LocalEvidencePasses: true, APIReachable: true,
			LiveClusterID: "uid-123", ProvisionedClusterID: "uid-123",
		},
	},
	"first-use-ok": {
		Name: "first-use-ok",
		Desc: "older install, nothing recorded, no external state: adopt on first use (B.3)",
		In: Input{
			RecordedMachineIDPresent: false,
			HasProvisionRecord:       false, LocalEvidencePasses: true,
			APIReachable: true, LiveClusterID: "uid-456",
		},
	},
	"copied-state": {
		Name: "copied-state",
		Desc: "state directory copied from another machine (P.1)",
		In: Input{
			RecordedMachineIDPresent: true, MachineIDMatches: false,
			HasProvisionRecord: true, TargetMatchesProvisionRecord: true,
		},
	},
	"broken-cluster": {
		Name: "broken-cluster",
		Desc: "our cluster, but kubelet is down / API unreachable; never falls back to external (A.1)",
		In: Input{
			RecordedMachineIDPresent: true, MachineIDMatches: true,
			HasProvisionRecord: true, TargetMatchesProvisionRecord: true,
			LocalEvidenceFailed: true, FailingPiece: "kubelet is installed but not running",
			APIReachable: false,
		},
	},
	"needs-sudo": {
		Name: "needs-sudo",
		Desc: "our cluster, but a local check could not be read without root (A.2)",
		In: Input{
			RecordedMachineIDPresent: true, MachineIDMatches: true,
			HasProvisionRecord: true, TargetMatchesProvisionRecord: true,
			LocalEvidenceUnreadable: true, FailingPiece: "/proc/<pid>/exe",
			APIReachable: true,
		},
	},
	"rebuilt": {
		Name: "rebuilt",
		Desc: "our cluster was rebuilt; live cluster ID differs from the recorded one, state is stale (A.4)",
		In: Input{
			RecordedMachineIDPresent: true, MachineIDMatches: true,
			HasProvisionRecord: true, TargetMatchesProvisionRecord: true,
			LocalEvidencePasses: true, APIReachable: true,
			LiveClusterID: "uid-new", ProvisionedClusterID: "uid-old",
		},
	},
	"down-first-use": {
		Name: "down-first-use",
		Desc: "older install, nothing recorded, API down so the cluster ID cannot be read (B.1)",
		In: Input{
			HasProvisionRecord: false, LocalEvidencePasses: true,
			APIReachable: false,
		},
	},
	"needs-adopt": {
		Name: "needs-adopt",
		Desc: "nothing recorded, but this cluster already has external state: adopt/discard first (B.2)",
		In: Input{
			HasProvisionRecord: false, LocalEvidencePasses: true,
			APIReachable: true, LiveClusterID: "uid-789", ExternalStateExists: true,
		},
	},
	"external": {
		Name: "external",
		Desc: "a cluster weaver did not provision, with --kubeconfig: external, so host+api install is refused (C.1 -> host+api)",
		In: Input{
			HasProvisionRecord: false, LocalEvidencePasses: false,
			KubeconfigGiven: true, APIReachable: true, LiveClusterID: "uid-ext",
		},
	},
	"nothing-here": {
		Name: "nothing-here",
		Desc: "no cluster on this host and no --kubeconfig (C.2)",
		In: Input{
			HasProvisionRecord: false, LocalEvidencePasses: false,
			KubeconfigGiven: false,
		},
	},
}

// ScenarioInput returns the canned Input for a named demo scenario and whether
// the name is known.
func ScenarioInput(name string) (Scenario, bool) {
	s, ok := scenarios[name]
	return s, ok
}

// ScenarioNames returns the demo scenario names, sorted, for help text.
func ScenarioNames() []string {
	names := make([]string, 0, len(scenarios))
	for n := range scenarios {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
