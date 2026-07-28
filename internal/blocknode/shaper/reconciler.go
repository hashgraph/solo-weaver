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

// setApplier writes desired per-policy nft set state to the kernel — CIDR
// membership sets and managed listener-port sets — under a single lock
// acquisition, so a reconcile tick is atomic (both dimensions or neither).
// Satisfied by *policy.Manager; a fake is injected in tests. The bool return
// distinguishes "applied" from "skipped because the operator apply lock was held"
// (see policy.Manager.ApplySets).
type setApplier interface {
	ApplySets(ctx context.Context, membership, ports map[string][]string) (bool, error)
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
	applier setApplier
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

// CheckResult is the unprivileged detect path's output: the sha256 digest of the
// desired policy state, the canonical desired CIDR membership (policy name ->
// nft-rendered elements), and the desired per-policy listener ports derived from
// statusz local.port, so `--check --output json` is useful for daemon-side
// introspection and debugging, not just change detection. The digest covers BOTH
// membership and ports, so a ports-only change is still detected.
type CheckResult struct {
	Digest       string              `json:"desired-digest"`
	Desired      map[string][]string `json:"desired"`
	DesiredPorts map[string][]string `json:"desired-ports"`
}

// Check fetches both statusz endpoints, buckets them into the desired
// per-category membership, derives the desired per-policy listener ports from
// the inbound local.port values, and returns the sha256 digest over both. It
// reads no nft state and requires no privilege — it is the unprivileged detect
// path.
func (r *Reconciler) Check(ctx context.Context) (CheckResult, error) {
	ce, inbound, err := r.fetchEndpoints(ctx)
	if err != nil {
		return CheckResult{}, err
	}
	canon, err := canonicalDesiredMembership(ce)
	if err != nil {
		return CheckResult{}, err
	}
	portsDesired := desiredPorts(inbound)
	return CheckResult{
		Digest:       membershipDigest(combinedCanonical(canon, portsDesired)),
		Desired:      canon,
		DesiredPorts: portsDesired,
	}, nil
}

// Apply fetches both statusz endpoints, derives the desired CIDR membership and
// listener ports, reads the live nft sets to find which owned sets actually
// changed, and rewrites only those. Sets already in the desired state are not
// touched. Both dimensions' changes are applied together under a single lock
// acquisition (see setApplier.ApplySets), so a tick is atomic — either both the
// changed membership sets and the changed listener-port sets are written, or the
// whole tick is skipped because an operator holds the lock. The returned Result
// folds both into its applied/skipped/unchanged lists — a membership set reported
// by its policy name (`bn-publisher`), a listener-port set by its nft set name
// (`bn-publisher_ports`) — alongside the digest, which covers both dimensions.
func (r *Reconciler) Apply(ctx context.Context) (Result, error) {
	ce, inbound, err := r.fetchEndpoints(ctx)
	if err != nil {
		return Result{}, err
	}
	desired := desiredMembership(ce)
	canon, err := canonicalDesiredMembership(ce)
	if err != nil {
		return Result{}, err
	}
	portsDesired := desiredPorts(inbound)
	digest := membershipDigest(combinedCanonical(canon, portsDesired))

	memDeltas, err := computePolicyDeltas(ctx, r.lister, ce)
	if err != nil {
		return Result{}, err
	}
	portDeltas, err := computePortDeltas(ctx, r.lister, portsDesired)
	if err != nil {
		return Result{}, err
	}

	// Membership sets are keyed and reported by policy name; listener-port sets
	// are keyed by policy name for the apply batch but reported by their
	// `<name>_ports` nft set name.
	changedMem := make(map[string][]string, len(memDeltas))
	changedPorts := make(map[string][]string, len(portDeltas))
	changed := make([]string, 0, len(memDeltas)+len(portDeltas))
	for _, d := range memDeltas {
		changedMem[d.Policy] = desired[d.Policy]
		changed = append(changed, d.Policy)
	}
	for _, d := range portDeltas {
		changedPorts[d.Policy] = portsDesired[d.Policy]
		changed = append(changed, policy.PortsSetName(d.Policy))
	}
	sort.Strings(changed)

	res := Result{Digest: digest, Unchanged: unchangedSetNames(changed)}
	if len(changedMem) == 0 && len(changedPorts) == 0 {
		return res, nil
	}

	// One lock acquisition for both dimensions: the tick is atomic.
	applied, err := r.applier.ApplySets(ctx, changedMem, changedPorts)
	if err != nil {
		return Result{}, errorx.ExternalError.Wrap(err, "apply reconciled traffic-shaper sets")
	}
	// The operator apply lock being held (applied == false) means nothing was
	// written, so the changed sets are still out of sync — report them as skipped
	// rather than dropping them from both Applied and Unchanged.
	if applied {
		res.Applied = changed
	} else {
		res.Skipped = changed
	}
	return res, nil
}

// fetchEndpoints reads both statusz endpoints, buckets them into the desired
// per-category membership view, and returns the raw inbound NetworkData too:
// listener-port derivation reads local.port straight off the inbound endpoints
// (which the membership bucketize discards), so the port pass needs the raw
// inbound payload rather than the bucketized view.
func (r *Reconciler) fetchEndpoints(ctx context.Context) (categoryEndpoints, NetworkData, error) {
	inbound, err := r.fetcher.InboundClients(ctx)
	if err != nil {
		return nil, NetworkData{}, err
	}
	outbound, err := r.fetcher.OutboundClients(ctx)
	if err != nil {
		return nil, NetworkData{}, err
	}
	return bucketizeEndpoints(inbound, outbound), inbound, nil
}

// bucketizeEndpoints folds one statusz snapshot into the desired membership,
// keyed by (direction, category). Inbound endpoints are matched against the
// inbound bindings (publisher/partner/restricted) and contribute their remote
// host/CIDR; outbound endpoints are matched against the outbound bindings. The
// compound bn-backfill set (outbound partner) contributes compound
// "remote.Address:remote.Port" pairs, skipping any endpoint whose port is empty
// or "*" (a wildcard port cannot key a compound set).
//
// Every owned binding is seeded present with an empty slice, so a category the
// BN no longer reports collapses to an empty membership that clears its set
// rather than leaving stale members behind — each owned set is fully reconciled
// every tick. Endpoints whose (direction, category) is not an owned binding
// (e.g. the public category, or operator-curated mgmt sets) are ignored.
func bucketizeEndpoints(inbound, outbound NetworkData) categoryEndpoints {
	ce := make(categoryEndpoints, len(categoryBindings))
	for k := range categoryBindings {
		ce[k] = []string{}
	}

	bucketize(ce, Inbound, inbound)
	bucketize(ce, Outbound, outbound)

	return ce
}

// bucketize appends one direction's endpoints into ce under their owned
// bindings, rendering compound "ip:port" tokens for compound sets and plain
// host/CIDR otherwise. Endpoints with no owned binding for (dir, category) are
// dropped; a compound endpoint with an empty or wildcard port is skipped.
func bucketize(ce categoryEndpoints, dir Direction, data NetworkData) {
	for _, conn := range data.ActiveEndpoints {
		k := bindingKey{dir: dir, cat: Category(conn.Category)}
		b, ok := categoryBindings[k]
		if !ok {
			continue
		}
		if !b.compound {
			ce[k] = append(ce[k], conn.Remote.Address)
			continue
		}
		if conn.Remote.Port == "" || conn.Remote.Port == "*" {
			continue
		}
		ce[k] = append(ce[k], conn.Remote.Address+":"+conn.Remote.Port)
	}
}

// desiredMembership maps each owned key present in ce to its nft policy name,
// carrying the raw statusz endpoints through unchanged (the policy Manager and
// the diff engine canonicalize them on the way to the kernel). Keys with no
// policy binding are dropped.
func desiredMembership(ce categoryEndpoints) map[string][]string {
	m := make(map[string][]string, len(ce))
	for k, endpoints := range ce {
		b, ok := categoryBindings[k]
		if !ok {
			continue
		}
		m[b.policyName] = endpoints
	}
	return m
}

// desiredPorts derives each managed-ports policy's desired listener ports from
// the inbound statusz local.port values, per the portBindings table. It is the
// port-dimension counterpart to desiredMembership, and carries the same
// present/absent semantics: every policy in portBindings is seeded present (with
// an empty, de-duplicated slice), so a category the BN stops reporting collapses
// its ports set to empty — clearing it — rather than leaving stale ports behind.
//
// Only local.port is read (the BN's own listener port); an endpoint whose
// local.port is empty or "*" (an unspecified/wildcard port) is skipped, since a
// wildcard cannot key an inet_service set. Outbound endpoints are never
// consulted: an outbound connection originates from an ephemeral local port and
// is not a listener.
func desiredPorts(inbound NetworkData) map[string][]string {
	perPolicy := make(map[string]map[string]struct{})
	for _, names := range portBindings {
		for _, name := range names {
			if _, ok := perPolicy[name]; !ok {
				perPolicy[name] = make(map[string]struct{})
			}
		}
	}

	for _, conn := range inbound.ActiveEndpoints {
		names, ok := portBindings[Category(conn.Category)]
		if !ok {
			continue
		}
		port := conn.Local.Port
		if port == "" || port == "*" {
			continue
		}
		for _, name := range names {
			perPolicy[name][port] = struct{}{}
		}
	}

	out := make(map[string][]string, len(perPolicy))
	for name, set := range perPolicy {
		ports := make([]string, 0, len(set))
		for p := range set {
			ports = append(ports, p)
		}
		out[name] = sortedUnique(ports)
	}
	return out
}

// combinedCanonical merges the canonical desired CIDR membership (keyed by policy
// name) with the desired listener ports (keyed by `<name>_ports`, the actual nft
// set name) into one map, canonicalizing the ports the same way membership is.
// Digesting this combined view means the digest changes when EITHER membership or
// ports change, so the daemon's digest-based change detection never misses a
// ports-only update. Keying ports under the `_ports` set name keeps them from
// colliding with a policy's membership entry.
func combinedCanonical(canon, portsDesired map[string][]string) map[string][]string {
	combined := make(map[string][]string, len(canon)+len(portsDesired))
	for name, elems := range canon {
		combined[name] = elems
	}
	for name, ports := range portsDesired {
		combined[policy.PortsSetName(name)] = policy.CanonicalizeElements(ports)
	}
	return combined
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

// ownedSetNames returns every nft set the traffic-shaper reconciles, sorted: the
// CIDR membership sets (ownedPolicyNames, keyed by policy name) plus the managed
// listener-port sets (`<name>_ports` for each managedPortsPolicyNames entry). It
// is the denominator for the Apply Result's unchanged accounting across both
// dimensions.
func ownedSetNames() []string {
	names := ownedPolicyNames()
	for _, p := range managedPortsPolicyNames() {
		names = append(names, policy.PortsSetName(p))
	}
	sort.Strings(names)
	return names
}

// unchangedSetNames returns the owned set names (membership + listener-port sets)
// not present in changed, sorted.
func unchangedSetNames(changed []string) []string {
	changedSet := make(map[string]struct{}, len(changed))
	for _, c := range changed {
		changedSet[c] = struct{}{}
	}
	var out []string
	for _, name := range ownedSetNames() {
		if _, ok := changedSet[name]; !ok {
			out = append(out, name)
		}
	}
	return out
}
