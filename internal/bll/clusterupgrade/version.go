// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/joomcode/errorx"
)

// version is a release version, major.minor.patch. Pre-releases are not
// versions the ladder ships, so they do not parse.
type version struct{ major, minor, patch int }

// minorVersion is a Kubernetes or Cilium minor, such as 1.34.
type minorVersion struct{ major, minor int }

func parseVersion(s string) (version, error) {
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) == 3 {
		n, ok := parseNumbers(parts)
		if ok {
			return version{n[0], n[1], n[2]}, nil
		}
	}
	return version{}, errorx.IllegalFormat.New("%q is not a release version such as 1.34.13", s)
}

func parseMinor(s string) (minorVersion, error) {
	parts := strings.Split(s, ".")
	if len(parts) == 2 {
		n, ok := parseNumbers(parts)
		if ok {
			return minorVersion{n[0], n[1]}, nil
		}
	}
	return minorVersion{}, errorx.IllegalFormat.New("%q is not a minor version such as 1.34", s)
}

func parseNumbers(parts []string) ([]int, bool) {
	out := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p != strconv.Itoa(n) {
			return nil, false
		}
		out[i] = n
	}
	return out, true
}

func (v version) String() string      { return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch) }
func (m minorVersion) String() string { return fmt.Sprintf("%d.%d", m.major, m.minor) }

func (v version) minorOf() minorVersion { return minorVersion{v.major, v.minor} }

func (v version) compare(o version) int {
	if c := compareInts(v.major, o.major); c != 0 {
		return c
	}
	if c := compareInts(v.minor, o.minor); c != 0 {
		return c
	}
	return compareInts(v.patch, o.patch)
}

func (v version) less(o version) bool { return v.compare(o) < 0 }

func (m minorVersion) compare(o minorVersion) int {
	if c := compareInts(m.major, o.major); c != 0 {
		return c
	}
	return compareInts(m.minor, o.minor)
}

// next is the following minor of the same major.
func (m minorVersion) next() minorVersion { return minorVersion{m.major, m.minor + 1} }

func compareInts(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
