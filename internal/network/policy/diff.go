// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
)

// compoundSep is the separator nft prints between the two halves of a compound
// `ipv4_addr . inet_service` set element. `network policy set` renders it and
// `nft list set` prints it back, so both the desired and live element forms use
// this exact spelling.
const compoundSep = " . "

// maxPort is the highest valid inet_service port; ports outside [1, maxPort] are
// not valid set-element keys.
const maxPort = 65535

// CompoundElement converts an "ip:port" pair into the nft compound set element
// token "<ip> . <port>" used by --reply-stamp policies' sets (e.g. bn-backfill).
// It is the single conversion the apply path (`network policy set`) and the
// daemon poll loop's diff both call, so desired membership is built byte for
// byte identical to what gets written to the kernel. The set is
// `ipv4_addr . inet_service`, so the host must be IPv4 and the port in range;
// the returned token is canonical (host normalized, port stripped of any leading
// zeros) and a malformed pair is rejected here rather than poisoning a later
// batch apply.
func CompoundElement(ipPort string) (string, error) {
	host, port, err := net.SplitHostPort(ipPort)
	if err != nil {
		return "", errorx.IllegalArgument.Wrap(err, "invalid ip:port %q: compound-set entries require an ip:port pair", ipPort)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil || !addr.Is4() {
		return "", errorx.IllegalArgument.New("invalid ip:port %q: %q is not an IPv4 address", ipPort, host)
	}
	if err := sanity.ValidatePort(port); err != nil {
		return "", errorx.IllegalArgument.Wrap(err, "invalid ip:port %q", ipPort)
	}
	p, _ := strconv.Atoi(port) // safe: ValidatePort already parsed and range-checked it
	return addr.String() + compoundSep + strconv.Itoa(p), nil
}

// SetDelta is the membership change to transform a policy's live nft set into
// its desired state: elements to add and elements to delete, each canonicalized
// and numerically ordered.
type SetDelta struct {
	Adds    []string
	Deletes []string
}

// Empty reports whether the delta is a no-op (live already matches desired).
func (d SetDelta) Empty() bool {
	return len(d.Adds) == 0 && len(d.Deletes) == 0
}

// DiffElements computes the SetDelta that turns live membership into desired
// membership. Both inputs are canonicalized first, so the result is independent
// of input order or spelling: Adds are canonical elements present in desired but
// not live; Deletes are present in live but not desired.
func DiffElements(desired, live []string) SetDelta {
	desiredCanon := CanonicalizeElements(desired)
	liveCanon := CanonicalizeElements(live)

	liveSet := make(map[string]struct{}, len(liveCanon))
	for _, e := range liveCanon {
		liveSet[e] = struct{}{}
	}
	desiredSet := make(map[string]struct{}, len(desiredCanon))
	for _, e := range desiredCanon {
		desiredSet[e] = struct{}{}
	}

	var adds, deletes []string
	for _, e := range desiredCanon {
		if _, ok := liveSet[e]; !ok {
			adds = append(adds, e)
		}
	}
	for _, e := range liveCanon {
		if _, ok := desiredSet[e]; !ok {
			deletes = append(deletes, e)
		}
	}
	return SetDelta{Adds: adds, Deletes: deletes}
}

// CanonicalizeElements normalizes a list of nft set element tokens into a stable,
// deduplicated, numerically-sorted slice. It accepts both plain IPv4 elements
// (bare "10.1.0.2" or CIDR "10.4.0.0/24") and compound "<ip> . <port>" keys, and
// renders each in the form `nft list set` prints for an interval ipv4_addr set:
// a single host as a bare address (a /32 is collapsed), a wider range as
// "<network>/<bits>". Passing both the desired membership and the live
// membership through this before diffing guarantees the two agree on spelling
// and order regardless of how the caller or the kernel originally wrote them, so
// the same normalized form drives both the comparison and the apply args.
//
// Tokens that don't parse are preserved as-is and sorted after the parseable
// ones, so an unexpected kernel rendering surfaces as a diff rather than being
// silently dropped.
func CanonicalizeElements(elements []string) []string {
	keys := make([]elemKey, 0, len(elements))
	seen := make(map[string]struct{}, len(elements))
	for _, e := range elements {
		k := parseElement(e)
		if _, dup := seen[k.canon]; dup {
			continue
		}
		seen[k.canon] = struct{}{}
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].less(keys[j]) })
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = k.canon
	}
	return out
}

// elemKey is the parsed, sortable form of one nft set element.
type elemKey struct {
	canon  string     // canonical rendering used for comparison and apply
	addr   netip.Addr // zero value when unparseable
	bits   int        // prefix length; -1 for compound elements
	port   int        // port for compound elements; -1 otherwise
	parsed bool       // false when the token could not be parsed
}

// parseElement normalizes one nft set element token into an elemKey.
func parseElement(tok string) elemKey {
	tok = strings.TrimSpace(tok)

	// These are ipv4_addr(. inet_service) sets, so only IPv4 hosts and in-range
	// ports count as parsed/authoritative; anything else falls through to the
	// unparseable path (preserved as-is, sorted last) rather than being ordered
	// among valid elements.
	if host, port, ok := splitCompound(tok); ok {
		if addr, err := netip.ParseAddr(host); err == nil && addr.Is4() {
			if p, err := strconv.Atoi(port); err == nil && p >= 1 && p <= maxPort {
				return elemKey{
					canon:  addr.String() + compoundSep + strconv.Itoa(p),
					addr:   addr,
					bits:   -1,
					port:   p,
					parsed: true,
				}
			}
		}
		return elemKey{canon: tok, bits: -1, port: -1}
	}

	if strings.Contains(tok, "/") {
		if pfx, err := netip.ParsePrefix(tok); err == nil && pfx.Addr().Is4() {
			pfx = pfx.Masked()
			// A full-length prefix is a single host; nft prints an interval
			// set's /32 as the bare address, so collapse it to match.
			if pfx.Bits() == pfx.Addr().BitLen() {
				return elemKey{canon: pfx.Addr().String(), addr: pfx.Addr(), bits: pfx.Bits(), port: -1, parsed: true}
			}
			return elemKey{canon: pfx.String(), addr: pfx.Addr(), bits: pfx.Bits(), port: -1, parsed: true}
		}
		return elemKey{canon: tok, bits: -1, port: -1}
	}

	if addr, err := netip.ParseAddr(tok); err == nil && addr.Is4() {
		return elemKey{canon: addr.String(), addr: addr, bits: addr.BitLen(), port: -1, parsed: true}
	}
	return elemKey{canon: tok, bits: -1, port: -1}
}

// splitCompound splits an nft compound element "<ip> . <port>" into its ip and
// port halves. It reports ok=false for plain (non-compound) tokens.
func splitCompound(tok string) (host, port string, ok bool) {
	i := strings.Index(tok, compoundSep)
	if i < 0 {
		return "", "", false
	}
	return strings.TrimSpace(tok[:i]), strings.TrimSpace(tok[i+len(compoundSep):]), true
}

// less orders elements numerically: by address, then prefix length, then port.
// Unparseable tokens sort last and lexically among themselves.
func (k elemKey) less(o elemKey) bool {
	if k.parsed != o.parsed {
		return k.parsed
	}
	if !k.parsed {
		return k.canon < o.canon
	}
	if c := k.addr.Compare(o.addr); c != 0 {
		return c < 0
	}
	if k.bits != o.bits {
		return k.bits < o.bits
	}
	return k.port < o.port
}
