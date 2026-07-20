// SPDX-License-Identifier: Apache-2.0

package shaper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/hashgraph/solo-weaver/internal/network/policy"
	"github.com/joomcode/errorx"
)

// endpointFetcher reads the block node's inbound/outbound active endpoints from
// its statusz REST endpoints. Satisfied by *StatuszClient; a fake is injected in
// tests so the reconcile logic can be exercised without a live BN.
type endpointFetcher interface {
	InboundClients(ctx context.Context) (NetworkData, error)
	OutboundClients(ctx context.Context) (NetworkData, error)
}

// membershipApplier writes desired per-policy nft set membership to the kernel.
// Satisfied by *policy.Manager; a fake is injected in tests. The bool return
// distinguishes "applied" from "skipped because the operator apply lock was
// held" (see policy.Manager.ApplyMembership).
type membershipApplier interface {
	ApplyMembership(ctx context.Context, desired map[string][]string) (bool, error)
}

// Reconciler drives one reconcile of the block node traffic-shaper's nft policy
// set membership from statusz. It is the engine behind the
// `block node reconcile-shaper` worker: Check derives the desired membership and
// digests it (no privilege, no nft), while Apply additionally reads the live nft
// sets, diffs, and writes only the changed policies (root).
//
// The three collaborators are seams so the orchestration is testable off-host:
// fetcher reads statusz, lister reads live nft membership, applier writes it.
type Reconciler struct {
	fetcher endpointFetcher
	lister  elementLister
	applier membershipApplier
}

// NewReconciler wires the production Reconciler: statusz is read over HTTP from
// statuszURL, live nft sets are read via the exec Runner, and membership is
// written via the network policy Manager.
func NewReconciler(statuszURL string) *Reconciler {
	return &Reconciler{
		fetcher: NewStatuszClient(statuszURL),
		lister:  policy.NewExecRunner(),
		applier: policy.NewManager(),
	}
}

// Result summarizes one Apply: the owned policies whose live membership was
// rewritten, those skipped because the operator apply lock was held (still out
// of sync, not yet reconciled), those left unchanged (already in the desired
// state), and the digest of the full desired membership (identical to what
// Check reports for the same statusz snapshot).
type Result struct {
	Applied   []string `json:"applied"`
	Skipped   []string `json:"skipped"`
	Unchanged []string `json:"unchanged"`
	Digest    string   `json:"digest"`
}

// CheckResult is the unprivileged detect path's output: the sha256 digest of
// the desired policy membership, and the canonical desired membership itself
// (policy name -> nft-rendered elements) so `--check --output json` is useful
// for daemon-side introspection and debugging, not just change detection.
type CheckResult struct {
	Digest  string              `json:"desired-digest"`
	Desired map[string][]string `json:"desired"`
}

// Check fetches both statusz endpoints, buckets them into the desired
// per-category membership, and returns the sha256 digest of the canonical
// desired policy membership alongside that canonical membership. It reads no
// nft state and requires no privilege — it is the unprivileged detect path.
func (r *Reconciler) Check(ctx context.Context) (CheckResult, error) {
	ce, err := r.fetchEndpoints(ctx)
	if err != nil {
		return CheckResult{}, err
	}
	canon, err := canonicalDesiredMembership(ce)
	if err != nil {
		return CheckResult{}, err
	}
	return CheckResult{Digest: membershipDigest(canon), Desired: canon}, nil
}

// Apply fetches both statusz endpoints, buckets them into the desired
// membership, reads the live nft sets to find which owned policies actually
// changed, and rewrites only those. Policies already in the desired state are
// not touched. The returned Result records the applied/unchanged split and the
// desired-membership digest.
func (r *Reconciler) Apply(ctx context.Context) (Result, error) {
	ce, err := r.fetchEndpoints(ctx)
	if err != nil {
		return Result{}, err
	}
	desired := desiredMembership(ce)
	canon, err := canonicalDesiredMembership(ce)
	if err != nil {
		return Result{}, err
	}
	digest := membershipDigest(canon)

	deltas, err := computePolicyDeltas(ctx, r.lister, ce)
	if err != nil {
		return Result{}, err
	}

	changed := make(map[string][]string, len(deltas))
	changedNames := make([]string, 0, len(deltas))
	for _, d := range deltas {
		changed[d.Policy] = desired[d.Policy]
		changedNames = append(changedNames, d.Policy)
	}
	sort.Strings(changedNames)

	res := Result{Digest: digest, Unchanged: unchangedPolicyNames(changedNames)}
	if len(changed) == 0 {
		return res, nil
	}

	applied, err := r.applier.ApplyMembership(ctx, changed)
	if err != nil {
		return Result{}, errorx.ExternalError.Wrap(err, "apply reconciled traffic-shaper membership")
	}
	if applied {
		res.Applied = changedNames
	} else {
		// The operator apply lock was held: nothing was written, so these
		// policies are still out of sync — report them as skipped rather than
		// silently dropping them from both Applied and Unchanged.
		res.Skipped = changedNames
	}
	return res, nil
}

// fetchEndpoints reads both statusz endpoints and buckets them into the desired
// per-category membership view.
func (r *Reconciler) fetchEndpoints(ctx context.Context) (CategoryEndpoints, error) {
	inbound, err := r.fetcher.InboundClients(ctx)
	if err != nil {
		return nil, err
	}
	outbound, err := r.fetcher.OutboundClients(ctx)
	if err != nil {
		return nil, err
	}
	return bucketizeEndpoints(inbound, outbound), nil
}

// bucketizeEndpoints folds one statusz snapshot into the desired per-category
// membership. Inbound categories (publisher/partner/restricted) contribute their
// remote host/CIDR; the outbound peer_bn category contributes compound
// "remote.Address:remote.Port" pairs, skipping any endpoint whose port is empty
// or "*" (a wildcard port cannot key a compound set).
//
// Every owned category is seeded present with an empty slice, so a category the
// BN no longer reports collapses to an empty membership that clears its set
// rather than leaving stale members behind — each owned set is fully reconciled
// every tick. Categories outside categoryBindings (e.g. the public category, or
// operator-curated mgmt sets) are ignored.
func bucketizeEndpoints(inbound, outbound NetworkData) CategoryEndpoints {
	ce := make(CategoryEndpoints, len(categoryBindings))
	for c := range categoryBindings {
		ce[c] = []string{}
	}

	for _, conn := range inbound.ActiveEndpoints {
		cat := Category(conn.Category)
		b, ok := categoryBindings[cat]
		if !ok || b.compound {
			continue
		}
		ce[cat] = append(ce[cat], conn.Remote.Address)
	}

	for _, conn := range outbound.ActiveEndpoints {
		cat := Category(conn.Category)
		b, ok := categoryBindings[cat]
		if !ok || !b.compound {
			continue
		}
		if conn.Remote.Port == "" || conn.Remote.Port == "*" {
			continue
		}
		ce[cat] = append(ce[cat], conn.Remote.Address+":"+conn.Remote.Port)
	}

	return ce
}

// desiredMembership maps each owned category present in ce to its nft policy
// name, carrying the raw statusz endpoints through unchanged (the policy Manager
// and the diff engine canonicalize them on the way to the kernel). Categories
// with no policy binding are dropped.
func desiredMembership(ce CategoryEndpoints) map[string][]string {
	m := make(map[string][]string, len(ce))
	for cat, endpoints := range ce {
		b, ok := categoryBindings[cat]
		if !ok {
			continue
		}
		m[b.policyName] = endpoints
	}
	return m
}

// membershipDigest returns a sha256 hex digest over a canonical serialization of
// the desired membership: policy names sorted, each membership list sorted and
// de-duplicated, rendered as `name\n<comma-joined members>\n` per policy. The
// digest depends only on the membership content, not on map iteration order or
// endpoint spelling order, so an unchanged desired state always digests the same.
func membershipDigest(m map[string][]string) string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		b.WriteString(name)
		b.WriteByte('\n')
		b.WriteString(strings.Join(sortedUnique(m[name]), ","))
		b.WriteByte('\n')
	}

	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// sortedUnique returns the input sorted with duplicates removed. The input is not
// mutated.
func sortedUnique(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	cp := make([]string, len(in))
	copy(cp, in)
	sort.Strings(cp)
	out := cp[:0]
	var prev string
	for i, s := range cp {
		if i == 0 || s != prev {
			out = append(out, s)
		}
		prev = s
	}
	return out
}

// ownedPolicyNames returns every nft policy name the traffic-shaper owns, sorted.
func ownedPolicyNames() []string {
	names := make([]string, 0, len(categoryBindings))
	for _, b := range categoryBindings {
		names = append(names, b.policyName)
	}
	sort.Strings(names)
	return names
}

// unchangedPolicyNames returns the owned policy names not present in changed,
// sorted.
func unchangedPolicyNames(changed []string) []string {
	changedSet := make(map[string]struct{}, len(changed))
	for _, c := range changed {
		changedSet[c] = struct{}{}
	}
	var out []string
	for _, name := range ownedPolicyNames() {
		if _, ok := changedSet[name]; !ok {
			out = append(out, name)
		}
	}
	return out
}
