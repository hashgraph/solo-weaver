// SPDX-License-Identifier: Apache-2.0

package provenance

// Rule identifies which decision rule produced an Outcome.
//
// The section 5.2 values ("P.1", "A.1", ...) are stable ids referenced in
// docs/dev/bring-your-own-cluster.md — do not change them. The gate-policy tags
// ("none", "host", ...) label outcomes Gate returns outside the outcome table
// (section 5.3). Keeping them as typed constants stops the string literals from
// drifting across evaluate.go, gate.go and the tests.
//
// Allocation discipline: a rule id is a frozen label (it appears in logs, hints
// and the design doc). The numeric suffix is allocation order, not evaluation
// order — evaluation order lives in evaluate.go. A new rule gets the next free
// number in its branch (A.5, B.4, ...), is appended to allRules below, and is
// placed in its evaluation position in evaluate.go. Never renumber, insert
// between, or reuse an existing id; the table rows may end up non-sequential and
// that is fine.
type Rule string

// Section 5.2 outcome table. The comment on each is the situation it marks.
const (
	RuleMachineMismatch    Rule = "P.1" // recorded machine ID does not match this machine
	RuleEvidenceFailed     Rule = "A.1" // our cluster, but local evidence failed or the API is down
	RuleEvidenceUnreadable Rule = "A.2" // our cluster, but a check could not be read (needs root)
	RuleProvisionedMatch   Rule = "A.3" // our cluster, live ID matches the provision record
	RuleRebuilt            Rule = "A.4" // our cluster was rebuilt; the recorded state is stale
	RuleAPIUnreachable     Rule = "B.1" // nothing recorded, API down, cluster ID unreadable
	RuleNeedsAdoption      Rule = "B.2" // nothing recorded, but external state already exists
	RuleFirstUse           Rule = "B.3" // nothing recorded; adopt on first use
	RuleExternal           Rule = "C.1" // not our cluster, --kubeconfig given
	RuleNoCluster          Rule = "C.2" // not our cluster and no --kubeconfig
)

// Gate-policy tags (section 5.3), not section 5.2 ids.
const (
	RuleNeedNone       Rule = "none"     // NeedNone: nothing to check
	RuleNeedHost       Rule = "host"     // NeedHost: offline host check
	RuleNeedHostAPIExt Rule = "host+api" // NeedHostAPI refusing an external target
	RuleInternal       Rule = "internal" // unknown need (bug)
)

// String returns the raw rule id.
func (r Rule) String() string { return string(r) }

// allRules lists every Rule, section 5.2 ids first. Keep it complete when adding
// a rule: the uniqueness guard in rules_test.go iterates it, so a duplicated or
// empty id fails the build.
var allRules = []Rule{
	RuleMachineMismatch,
	RuleEvidenceFailed,
	RuleEvidenceUnreadable,
	RuleProvisionedMatch,
	RuleRebuilt,
	RuleAPIUnreachable,
	RuleNeedsAdoption,
	RuleFirstUse,
	RuleExternal,
	RuleNoCluster,
	RuleNeedNone,
	RuleNeedHost,
	RuleNeedHostAPIExt,
	RuleInternal,
}
