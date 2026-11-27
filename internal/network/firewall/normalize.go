// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"net/netip"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Every set is `flags interval; auto-merge`, so the kernel merges overlapping
// and adjacent entries and prints them back as ranges: 22,23 reads as 22-23 and
// three /32s as a range. normalizeCIDRs and normalizePorts bring the config side
// and the live side to the same form before they are compared.

// addrRange is an inclusive range of addresses in one family.
type addrRange struct{ lo, hi netip.Addr }

// normalizeCIDRs merges entries (prefixes, bare addresses or lo-hi ranges)
// into the fewest prefixes covering the same addresses, IPv4 first, each
// family in address order. An entry it cannot parse is kept as written, sorted
// last, so it still shows as a difference.
func normalizeCIDRs(entries []string) []string {
	var v4, v6 []addrRange
	var other []string
	for _, e := range entries {
		r, ok := parseAddrRange(e)
		switch {
		case !ok:
			other = append(other, e)
		case r.lo.Is4():
			v4 = append(v4, r)
		default:
			v6 = append(v6, r)
		}
	}
	var out []string
	for _, ranges := range [][]addrRange{v4, v6} {
		for _, r := range mergeAddrRanges(ranges) {
			out = append(out, rangePrefixes(r)...)
		}
	}
	return append(out, sortedDedupe(other)...)
}

func parseAddrRange(entry string) (addrRange, bool) {
	entry = strings.TrimSpace(entry)
	if lo, hi, isRange := strings.Cut(entry, "-"); isRange {
		// Not unmapped: nft prints a range in an IPv6 set with an IPv4-mapped
		// end (::fffd:ffff:ffff-::ffff:255.255.255.255), and it stays IPv6.
		l, err1 := netip.ParseAddr(strings.TrimSpace(lo))
		h, err2 := netip.ParseAddr(strings.TrimSpace(hi))
		if err1 != nil || err2 != nil || l.BitLen() != h.BitLen() || h.Less(l) {
			return addrRange{}, false
		}
		return addrRange{l, h}, true
	}
	if strings.Contains(entry, "/") {
		p, err := netip.ParsePrefix(entry)
		if err != nil {
			return addrRange{}, false
		}
		p = p.Masked()
		return addrRange{p.Addr(), lastAddr(p)}, true
	}
	a, err := netip.ParseAddr(entry)
	if err != nil {
		return addrRange{}, false
	}
	return addrRange{a, a}, true
}

func mergeAddrRanges(ranges []addrRange) []addrRange {
	slices.SortFunc(ranges, func(a, b addrRange) int { return a.lo.Compare(b.lo) })
	var out []addrRange
	for _, r := range ranges {
		if n := len(out); n > 0 {
			last := &out[n-1]
			next := last.hi.Next()
			if !next.IsValid() || r.lo.Compare(next) <= 0 {
				if last.hi.Less(r.hi) {
					last.hi = r.hi
				}
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

// rangePrefixes splits r into the fewest prefixes, largest first.
func rangePrefixes(r addrRange) []string {
	var out []string
	lo := r.lo
	for {
		p := largestPrefixAt(lo, r.hi)
		out = append(out, p.String())
		end := lastAddr(p)
		lo = end.Next()
		if end == r.hi || !lo.IsValid() || lo.BitLen() != r.hi.BitLen() {
			return out
		}
	}
}

// largestPrefixAt returns the shortest prefix that starts at lo and ends at or
// before hi.
func largestPrefixAt(lo, hi netip.Addr) netip.Prefix {
	for bits := 0; bits < lo.BitLen(); bits++ {
		p := netip.PrefixFrom(lo, bits)
		if p.Masked().Addr() == lo && lastAddr(p).Compare(hi) <= 0 {
			return p
		}
	}
	return netip.PrefixFrom(lo, lo.BitLen())
}

// lastAddr returns the last address in p.
func lastAddr(p netip.Prefix) netip.Addr {
	b := p.Masked().Addr().AsSlice()
	for i := p.Bits(); i < len(b)*8; i++ {
		b[i/8] |= 0x80 >> (i % 8)
	}
	a, _ := netip.AddrFromSlice(b)
	return a
}

// normalizePorts merges port entries ("22" or "2379-2380") into ascending
// "a" or "a-b" ranges. An entry it cannot parse is kept as written, sorted
// last.
func normalizePorts(entries []string) []string {
	type portRange struct{ lo, hi int }
	var ranges []portRange
	var other []string
	for _, e := range entries {
		e = strings.TrimSpace(e)
		lo, hi, isRange := strings.Cut(e, "-")
		if !isRange {
			hi = lo
		}
		l, err1 := strconv.Atoi(strings.TrimSpace(lo))
		h, err2 := strconv.Atoi(strings.TrimSpace(hi))
		if err1 != nil || err2 != nil || h < l {
			other = append(other, e)
			continue
		}
		ranges = append(ranges, portRange{l, h})
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].lo < ranges[j].lo })
	var merged []portRange
	for _, r := range ranges {
		if n := len(merged); n > 0 && r.lo <= merged[n-1].hi+1 {
			merged[n-1].hi = max(merged[n-1].hi, r.hi)
			continue
		}
		merged = append(merged, r)
	}
	out := make([]string, 0, len(merged)+len(other))
	for _, r := range merged {
		if r.lo == r.hi {
			out = append(out, strconv.Itoa(r.lo))
		} else {
			out = append(out, strconv.Itoa(r.lo)+"-"+strconv.Itoa(r.hi))
		}
	}
	return append(out, sortedDedupe(other)...)
}

// setElements returns the elements of set name in an `nft list table`
// listing, or nil when the set is not declared or has no elements. nft wraps a
// long element list across lines, and an element may carry a quoted comment
// holding braces or commas; only the element itself is returned.
func setElements(listing, name string) []string {
	loc := regexp.MustCompile(`\bset ` + regexp.QuoteMeta(name) + ` \{`).FindStringIndex(listing)
	if loc == nil {
		return nil
	}
	body := enclosed(listing[loc[1]:])
	const open = "elements = {"
	i := strings.Index(body, open)
	if i == -1 {
		return nil
	}
	var out []string
	for _, e := range splitUnquoted(enclosed(body[i+len(open):]), ',') {
		if f := strings.Fields(e); len(f) > 0 {
			out = append(out, f[0])
		}
	}
	return out
}

// setDeclared reports whether the listing declares set name.
func setDeclared(listing, name string) bool {
	return regexp.MustCompile(`\bset ` + regexp.QuoteMeta(name) + ` \{`).MatchString(listing)
}

// setNamePattern matches a set declaration in an `nft list table` listing.
var setNamePattern = regexp.MustCompile(`(?m)^\s*set (\S+) \{`)

// setNames returns every set the listing declares.
func setNames(listing string) []string {
	var out []string
	for _, m := range setNamePattern.FindAllStringSubmatch(listing, -1) {
		out = append(out, m[1])
	}
	return out
}

// enclosed returns s up to the brace that closes the one just before it,
// ignoring braces inside quotes.
func enclosed(s string) string {
	depth, quoted := 1, false
	for i, c := range s {
		switch {
		case c == '"':
			quoted = !quoted
		case quoted:
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return s[:i]
			}
		}
	}
	return s
}

// splitUnquoted splits s at each sep outside quotes.
func splitUnquoted(s string, sep rune) []string {
	var out []string
	quoted, from := false, 0
	for i, c := range s {
		switch {
		case c == '"':
			quoted = !quoted
		case c == sep && !quoted:
			out = append(out, s[from:i])
			from = i + 1
		}
	}
	return append(out, s[from:])
}
