// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"reflect"
	"strings"
	"testing"
)

// normalizeTcLine turns a rendered boot-script `tc ...` line into the token
// slice the tc*Args builders produce: drop the leading "tc", strip the
// best-effort redirection suffix (`2>/dev/null || true`) and the shell quotes
// the template wraps rates/device in, and collapse the template's cosmetic
// column-alignment whitespace. The device stays as the shell variable "$NIC".
func normalizeTcLine(line string) []string {
	line = strings.TrimSpace(line)
	if i := strings.Index(line, " 2>/dev/null"); i >= 0 {
		line = line[:i]
	}
	line = strings.ReplaceAll(line, "\"", "")
	line = strings.TrimPrefix(line, "tc ")
	return strings.Fields(line) // Fields collapses any run of whitespace
}

// TestBootScriptTcEncodingMatchesArgBuilders is the lockstep guard for #946
// AC#2: the tc command encoding must live in one place. The live path
// (execTCRunner) executes the tc*Args slices; the bandwidth-shaper boot script renders
// the same commands as shell text. This renders the explicit-rate boot script
// for a concrete egress config and asserts every `tc ...` line equals the
// arg-builder encoding token for token (whitespace- and quote-insensitive), so
// a change to HTB argument order or the field set on either side fails here.
func TestBootScriptTcEncodingMatchesArgBuilders(t *testing.T) {
	dev := &DeviceConfig{Dir: DirEgress, Rate: "500mbit", DefaultClass: "reserve-egress"}
	classes := []*ClassConfig{
		{Name: "partner", Rate: "200mbit", Ceil: "350mbit", Prio: 0},
		{Name: "public", Rate: "150mbit", Ceil: "350mbit", Prio: 5},
		{Name: "reserve-egress", Rate: "150mbit", Prio: 1}, // ceil defaults to rate
	}

	// The rendered tc lines reference the shell variable "$NIC" regardless of the
	// nicName passed here (which only fills the NIC="..." assignment), so the
	// expected builder encoding uses "$NIC" as the device.
	const nic = "$NIC"
	rendered, err := renderTcEgressScriptFromConfig("enp0s1", dev, classes)
	if err != nil {
		t.Fatalf("renderTcEgressScriptFromConfig: %v", err)
	}

	def, err := lookupClassInfo(dev.DefaultClass)
	if err != nil {
		t.Fatalf("lookupClassInfo(%q): %v", dev.DefaultClass, err)
	}
	expected := [][]string{
		tcQdiscDelRootArgs(nic),
		tcQdiscAddRootArgs(nic, def.Minor),
		tcClassAddRootArgs(nic, dev.Rate, dev.Rate),
	}
	for _, c := range classes {
		ci, err := lookupClassInfo(c.Name)
		if err != nil {
			t.Fatalf("lookupClassInfo(%q): %v", c.Name, err)
		}
		expected = append(expected,
			tcClassAddArgs(nic, ci.Minor, c.Rate, c.effectiveCeil(), c.Prio),
			tcQdiscAddFqCodelArgs(nic, ci.Minor, ci.Handle),
		)
	}

	var actual [][]string
	for _, ln := range strings.Split(rendered, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "tc ") {
			actual = append(actual, normalizeTcLine(ln))
		}
	}

	if len(actual) != len(expected) {
		t.Fatalf("boot script has %d tc lines, arg-builders produce %d\nscript:\n%s",
			len(actual), len(expected), rendered)
	}
	for i := range expected {
		if !reflect.DeepEqual(actual[i], expected[i]) {
			t.Errorf("tc line %d encoding drift:\n  boot script:  %v\n  arg-builders: %v",
				i, actual[i], expected[i])
		}
	}
}
