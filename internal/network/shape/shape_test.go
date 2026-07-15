// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Existing render tests (default/legacy SPEED-based rendering) ---

func TestRenderTcEgressScript_NICInterpolated(t *testing.T) {
	dir := t.TempDir()

	const testNIC = "eth0"
	testPath := filepath.Join(dir, "solo-provisioner-tc-egress.sh")

	rendered, err := renderScript(testNIC)
	if err != nil {
		t.Fatalf("renderScript: %v", err)
	}

	// The NIC name must appear in the quoted NIC= assignment line.
	if !strings.Contains(rendered, `NIC="`+testNIC+`"`) {
		t.Errorf("rendered script does not contain NIC assignment %q:\n%s", `NIC="`+testNIC+`"`, rendered)
	}
	// Must NOT contain the raw template placeholder.
	if strings.Contains(rendered, "{{.NIC}}") {
		t.Errorf("rendered script still contains template placeholder {{.NIC}}:\n%s", rendered)
	}

	// Golden: all expected tc commands are present (the script uses the shell
	// variable "$NIC", not the literal NIC name inline in tc commands).
	for _, want := range []string{
		`SPEED=$(cat /sys/class/net/"$NIC"/speed 2>/dev/null || echo -1)`,
		`[ "${SPEED:-0}" -gt 0 ] 2>/dev/null || SPEED=1000`,
		`tc qdisc del dev "$NIC" root 2>/dev/null || true`,
		`tc qdisc add dev "$NIC" root handle 1: htb default 60`,
		`tc class  add dev "$NIC" parent 1:   classid 1:1  htb rate "${SPEED}mbit" ceil "${SPEED}mbit"`,
		`tc class  add dev "$NIC" parent 1:1  classid 1:40 htb rate "$(( SPEED * 40 / 100 ))mbit"`,
		`tc class  add dev "$NIC" parent 1:1  classid 1:50 htb rate "$(( SPEED * 30 / 100 ))mbit"`,
		`tc class  add dev "$NIC" parent 1:1  classid 1:60 htb rate "$(( SPEED * 30 / 100 ))mbit"`,
		`tc qdisc  add dev "$NIC" parent 1:40 handle 140: fq_codel`,
		`tc qdisc  add dev "$NIC" parent 1:50 handle 150: fq_codel`,
		`tc qdisc  add dev "$NIC" parent 1:60 handle 160: fq_codel`,
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered script missing expected line:\n  want: %s\n  got:\n%s", want, rendered)
		}
	}

	// Write to temp path and confirm the file is executable.
	if err := atomicWriteFile(testPath, rendered, 0o755); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}
	info, err := os.Stat(testPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode()&0o100 == 0 {
		t.Errorf("script not executable: mode %v", info.Mode())
	}
}

func TestRenderTcEgressScript_EmptyNIC(t *testing.T) {
	// The NIC-name check lives in the render funnel, so an empty (or invalid)
	// NIC is rejected on the live path, not just in a dedicated wrapper.
	if _, err := renderTcEgressScript(""); err == nil {
		t.Error("expected error for empty NIC name, got nil")
	}
	if _, err := renderTcEgressScript(`eth0";reboot;#`); err == nil {
		t.Error("expected error for injection-style NIC name, got nil")
	}
}

func TestAtomicWriteFile_SecondWritePreservesContent(t *testing.T) {
	dir := t.TempDir()
	const testNIC = "ens3"
	path := filepath.Join(dir, "tc-egress.sh")

	rendered, err := renderScript(testNIC)
	if err != nil {
		t.Fatalf("renderScript: %v", err)
	}
	if err := atomicWriteFile(path, rendered, 0o755); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := atomicWriteFile(path, rendered, 0o755); err != nil {
		t.Fatalf("second write: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != rendered {
		t.Error("content changed on second write")
	}
}

// renderScript is a test-only helper that calls renderTcEgressScript so tests
// don't need to patch the global TcEgressScriptPath constant.
func renderScript(nicName string) (string, error) {
	return renderTcEgressScript(nicName)
}

// --- New tests: renderTcEgressScriptFromConfig ---

func TestRenderTcEgressScriptFromConfig_ExplicitRates(t *testing.T) {
	dev := &DeviceConfig{
		Dir:          DirEgress,
		Rate:         "1gbit",
		DefaultClass: "reserve-egress",
	}
	classes := []*ClassConfig{
		{Name: "partner", Rate: "400mbit", Ceil: "700mbit", Prio: 0},
		{Name: "public", Rate: "300mbit", Ceil: "700mbit", Prio: 5},
		{Name: "reserve-egress", Rate: "300mbit", Prio: 1},
	}
	rendered, err := renderTcEgressScriptFromConfig("ens3", dev, classes)
	if err != nil {
		t.Fatalf("renderTcEgressScriptFromConfig: %v", err)
	}

	if !strings.Contains(rendered, `NIC="ens3"`) {
		t.Errorf("expected NIC=\"ens3\":\n%s", rendered)
	}
	if !strings.Contains(rendered, "htb default 60") {
		t.Errorf("expected htb default 60 (reserve-egress minor):\n%s", rendered)
	}
	if !strings.Contains(rendered, `rate "1gbit" ceil "1gbit"`) {
		t.Errorf("expected root rate 1gbit:\n%s", rendered)
	}
	if !strings.Contains(rendered, `classid 1:40 htb rate "400mbit" ceil "700mbit" prio 0`) {
		t.Errorf("partner class line wrong:\n%s", rendered)
	}
	if !strings.Contains(rendered, `classid 1:50 htb rate "300mbit" ceil "700mbit" prio 5`) {
		t.Errorf("public class line wrong:\n%s", rendered)
	}
	// reserve-egress: ceil defaults to rate when unset.
	if !strings.Contains(rendered, `classid 1:60 htb rate "300mbit" ceil "300mbit" prio 1`) {
		t.Errorf("reserve-egress class line wrong:\n%s", rendered)
	}
	for _, want := range []string{
		`parent 1:40 handle 140: fq_codel`,
		`parent 1:50 handle 150: fq_codel`,
		`parent 1:60 handle 160: fq_codel`,
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("missing fq_codel line %q:\n%s", want, rendered)
		}
	}
	// SPEED variable must not appear: all rates are explicit, sysfs detection is dead code.
	if strings.Contains(rendered, "SPEED") {
		t.Errorf("rendered script must not contain SPEED when rates are explicit:\n%s", rendered)
	}
}

func TestRenderTcEgressScriptFromConfig_UnknownClass(t *testing.T) {
	dev := &DeviceConfig{Dir: DirEgress, Rate: "1gbit", DefaultClass: "reserve-egress"}
	_, err := renderTcEgressScriptFromConfig("ens3", dev, []*ClassConfig{{Name: "no-such-class", Rate: "100mbit"}})
	if err == nil {
		t.Error("expected error for unknown class, got nil")
	}
}

func TestRenderTcEgressScriptFromConfig_IngressClasses(t *testing.T) {
	dev := &DeviceConfig{Dir: DirIngress, Rate: "1gbit", DefaultClass: "reserve-ingress"}
	classes := []*ClassConfig{
		{Name: "publisher", Rate: "400mbit", Ceil: "700mbit", Prio: 0},
		{Name: "reserve-ingress", Rate: "300mbit", Prio: 1},
	}
	rendered, err := renderTcEgressScriptFromConfig("veth0", dev, classes)
	if err != nil {
		t.Fatalf("renderTcEgressScriptFromConfig: %v", err)
	}
	// Default minor for reserve-ingress is "30".
	if !strings.Contains(rendered, "htb default 30") {
		t.Errorf("expected htb default 30:\n%s", rendered)
	}
	if !strings.Contains(rendered, `classid 1:10 htb rate "400mbit"`) {
		t.Errorf("publisher class (1:10) missing:\n%s", rendered)
	}
}

// --- DetectEgressInterface tests (cross-platform via detectEgressInterfaceFrom) ---

func TestDetectEgressInterfaceFrom_DefaultRoute(t *testing.T) {
	content := "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n" +
		"eth0\t00000000\t0101A8C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n" +
		"eth0\tC0A80100\t00000000\t0001\t0\t0\t0\tFFFFFF00\t0\t0\t0\n"

	path := writeTempRoute(t, content)
	iface, err := detectEgressInterfaceFrom(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if iface != "eth0" {
		t.Errorf("expected eth0, got %q", iface)
	}
}

func TestDetectEgressInterfaceFrom_MultipleInterfaces(t *testing.T) {
	content := "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n" +
		"lo\t00000000\t00000000\t0001\t0\t0\t0\t000000FF\t0\t0\t0\n" +
		"ens3\t00000000\t0101A8C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n"

	path := writeTempRoute(t, content)
	iface, err := detectEgressInterfaceFrom(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if iface != "ens3" {
		t.Errorf("expected ens3, got %q", iface)
	}
}

func TestDetectEgressInterfaceFrom_NoDefaultRoute(t *testing.T) {
	content := "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n" +
		"eth0\tC0A80100\t00000000\t0001\t0\t0\t0\tFFFFFF00\t0\t0\t0\n"

	path := writeTempRoute(t, content)
	_, err := detectEgressInterfaceFrom(path)
	if err == nil {
		t.Error("expected error when no default route found, got nil")
	}
}

func TestDetectEgressInterfaceFrom_EmptyTable(t *testing.T) {
	content := "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n"
	path := writeTempRoute(t, content)
	_, err := detectEgressInterfaceFrom(path)
	if err == nil {
		t.Error("expected error for empty routing table, got nil")
	}
}

func writeTempRoute(t *testing.T, content string) string {
	t.Helper()
	tmp, err := os.CreateTemp(t.TempDir(), "proc-net-route-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, err := tmp.WriteString(content); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	_ = tmp.Close()
	return tmp.Name()
}

// --- Speed hint tests (cross-platform via readLinkSpeedMbitFrom) ---

func TestReadLinkSpeedMbitFrom_ValidSpeed(t *testing.T) {
	for _, tc := range []struct {
		content string
		want    int
	}{
		{"1000\n", 1000},
		{"10000\n", 10000},
		{"100\n", 100},
		{"1000", 1000}, // no trailing newline
	} {
		path := writeTempSpeed(t, tc.content)
		got, ok := readLinkSpeedMbitFrom(path)
		if !ok || got != tc.want {
			t.Errorf("readLinkSpeedMbitFrom(%q) = (%d, %v), want (%d, true)", tc.content, got, ok, tc.want)
		}
	}
}

func TestReadLinkSpeedMbitFrom_NegativeValue(t *testing.T) {
	path := writeTempSpeed(t, "-1\n")
	_, ok := readLinkSpeedMbitFrom(path)
	if ok {
		t.Error("expected (0, false) for -1, got ok=true")
	}
}

func TestReadLinkSpeedMbitFrom_ZeroValue(t *testing.T) {
	path := writeTempSpeed(t, "0\n")
	_, ok := readLinkSpeedMbitFrom(path)
	if ok {
		t.Error("expected (0, false) for 0, got ok=true")
	}
}

func TestReadLinkSpeedMbitFrom_NonNumeric(t *testing.T) {
	path := writeTempSpeed(t, "unknown\n")
	_, ok := readLinkSpeedMbitFrom(path)
	if ok {
		t.Error("expected (0, false) for non-numeric content, got ok=true")
	}
}

func TestReadLinkSpeedMbitFrom_MissingFile(t *testing.T) {
	_, ok := readLinkSpeedMbitFrom("/nonexistent/path/speed")
	if ok {
		t.Error("expected (0, false) for missing file, got ok=true")
	}
}

func TestReadLinkSpeedMbit_PathTraversal(t *testing.T) {
	_, ok := ReadLinkSpeedMbit("../../../etc/passwd")
	if ok {
		t.Error("expected (0, false) for NIC name containing path separator, got ok=true")
	}
}

func writeTempSpeed(t *testing.T, content string) string {
	t.Helper()
	tmp, err := os.CreateTemp(t.TempDir(), "sysfs-speed-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, err := tmp.WriteString(content); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	_ = tmp.Close()
	return tmp.Name()
}

// --- FormatSpeedHint tests ---

func TestFormatSpeedHint(t *testing.T) {
	cases := []struct {
		mbit int
		want string
	}{
		{1000, "1gbit"},
		{10000, "10gbit"},
		{100, "100mbit"},
		{500, "500mbit"},
		{2000, "2gbit"},
		{1500, "1500mbit"},
	}
	for _, c := range cases {
		got := FormatSpeedHint(c.mbit)
		if got != c.want {
			t.Errorf("FormatSpeedHint(%d) = %q, want %q", c.mbit, got, c.want)
		}
	}
}

func TestParseSpeedMbit(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  int
		ok    bool
	}{
		{"1gbit", 1000, true},
		{"10gbit", 10000, true},
		{"100mbit", 100, true},
		{"500mbit", 500, true},
		{"1GBIT", 1000, true}, // case-insensitive
		{"", 0, false},
		{"auto", 0, false},
		{"0gbit", 0, false},
		{"0mbit", 0, false},
		{"-1mbit", 0, false},
	} {
		got, ok := ParseSpeedMbit(tc.input)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ParseSpeedMbit(%q) = (%d, %v), want (%d, %v)", tc.input, got, ok, tc.want, tc.ok)
		}
	}
}

func TestRenderTcEgressScript_AutoDetectSpeed(t *testing.T) {
	rendered, err := renderTcEgressScript("eth0")
	if err != nil {
		t.Fatalf("renderTcEgressScript: %v", err)
	}
	if !strings.Contains(rendered, `cat /sys/class/net/"$NIC"/speed`) {
		t.Errorf("rendered script missing sysfs detection:\n%s", rendered)
	}
}

func renderDefaultEgressScript(t *testing.T, trunkRate string) string {
	t.Helper()
	dev, classes, err := defaultEgressConfig(trunkRate)
	if err != nil {
		t.Fatalf("defaultEgressConfig(%q): %v", trunkRate, err)
	}
	rendered, err := renderTcEgressScriptFromConfig("eth0", dev, classes)
	if err != nil {
		t.Fatalf("renderTcEgressScriptFromConfig: %v", err)
	}
	return rendered
}

func TestDefaultEgressConfig_1gbit_ProportionalRates(t *testing.T) {
	rendered := renderDefaultEgressScript(t, "1gbit")

	// Explicit rates: SPEED variable must be absent.
	if strings.Contains(rendered, "SPEED") {
		t.Errorf("rendered script must not contain SPEED when rates are explicit:\n%s", rendered)
	}
	// Trunk at 1gbit.
	if !strings.Contains(rendered, `htb rate "1gbit" ceil "1gbit"`) {
		t.Errorf("trunk class missing 1gbit rate:\n%s", rendered)
	}
	// partner: 40%/70% of 1gbit = 400mbit/700mbit
	if !strings.Contains(rendered, `rate "400mbit" ceil "700mbit"`) {
		t.Errorf("partner class missing 400mbit/700mbit:\n%s", rendered)
	}
	// public: 30%/70% of 1gbit = 300mbit/700mbit
	if !strings.Contains(rendered, `rate "300mbit" ceil "700mbit"`) {
		t.Errorf("public class missing 300mbit/700mbit:\n%s", rendered)
	}
	// reserve-egress: 30%/100% = 300mbit / trunk (1gbit)
	if !strings.Contains(rendered, `rate "300mbit" ceil "1gbit"`) {
		t.Errorf("reserve-egress class missing 300mbit/1gbit:\n%s", rendered)
	}
}

func TestDefaultEgressConfig_100mbit_ProportionalRates(t *testing.T) {
	rendered := renderDefaultEgressScript(t, "100mbit")

	if strings.Contains(rendered, "SPEED") {
		t.Errorf("rendered script must not contain SPEED:\n%s", rendered)
	}
	// partner: 40mbit/70mbit; public: 30mbit/70mbit; reserve-egress: 30mbit/100mbit trunk
	if !strings.Contains(rendered, `rate "40mbit" ceil "70mbit"`) {
		t.Errorf("partner class missing 40mbit/70mbit:\n%s", rendered)
	}
	if !strings.Contains(rendered, `rate "30mbit" ceil "70mbit"`) {
		t.Errorf("public class missing 30mbit/70mbit:\n%s", rendered)
	}
	if !strings.Contains(rendered, `rate "30mbit" ceil "100mbit"`) {
		t.Errorf("reserve-egress class missing 30mbit/100mbit:\n%s", rendered)
	}
}

// --- Validation tests ---

func TestValidateDir(t *testing.T) {
	if err := validateDir(DirEgress); err != nil {
		t.Errorf("validateDir(egress): %v", err)
	}
	if err := validateDir(DirIngress); err != nil {
		t.Errorf("validateDir(ingress): %v", err)
	}
	if err := validateDir("sideways"); err == nil {
		t.Error("expected error for invalid direction, got nil")
	}
}

func TestValidatePrio(t *testing.T) {
	for _, p := range []int{0, 3, 7} {
		if err := validatePrio(p); err != nil {
			t.Errorf("validatePrio(%d): %v", p, err)
		}
	}
	for _, p := range []int{-1, 8, 100} {
		if err := validatePrio(p); err == nil {
			t.Errorf("expected error for prio %d, got nil", p)
		}
	}
}

func TestParseBandwidthBps(t *testing.T) {
	cases := []struct {
		in      string
		wantBps int64
	}{
		{"1gbit", 1_000_000_000},
		{"100mbit", 100_000_000},
		{"500kbit", 500_000},
		{"1000bit", 1_000},
		{"1GBIT", 1_000_000_000},
	}
	for _, c := range cases {
		got, err := parseBandwidthBps(c.in)
		if err != nil {
			t.Errorf("parseBandwidthBps(%q): %v", c.in, err)
			continue
		}
		if got != c.wantBps {
			t.Errorf("parseBandwidthBps(%q) = %d, want %d", c.in, got, c.wantBps)
		}
	}
	for _, bad := range []string{"", "100", "fast", "$(( SPEED * 40 / 100 ))mbit", "${SPEED}mbit"} {
		if _, err := parseBandwidthBps(bad); err == nil {
			t.Errorf("expected error for %q, got nil", bad)
		}
	}
}

func TestValidateSumRates(t *testing.T) {
	existing := []*ClassConfig{
		{Name: "partner", Rate: "400mbit"},
		{Name: "public", Rate: "300mbit"},
	}
	// 400+300+300 = 1000mbit = 1gbit: exactly at limit, should pass.
	cfg := &ClassConfig{Name: "reserve-egress", Rate: "300mbit"}
	if err := validateSumRates(existing, cfg, "1gbit"); err != nil {
		t.Errorf("unexpected error at exact limit: %v", err)
	}
	// 400+300+400 = 1100mbit > 1gbit: should fail.
	cfg2 := &ClassConfig{Name: "reserve-egress", Rate: "400mbit"}
	if err := validateSumRates(existing, cfg2, "1gbit"); err == nil {
		t.Error("expected sum-rate error, got nil")
	}
	// Replacing partner (400→500): 500+300=800mbit ≤ 1gbit: should pass.
	cfg3 := &ClassConfig{Name: "partner", Rate: "500mbit"}
	if err := validateSumRates(existing, cfg3, "1gbit"); err != nil {
		t.Errorf("unexpected error when replacing within limit: %v", err)
	}
	// Unparseable device rate: skip validation (legacy).
	if err := validateSumRates(existing, cfg2, "${SPEED}mbit"); err != nil {
		t.Errorf("unexpected error for unparseable device rate: %v", err)
	}
}

func TestValidateDefaultClass(t *testing.T) {
	if err := validateDefaultClass("reserve-egress", DirEgress); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if err := validateDefaultClass("reserve-ingress", DirIngress); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if err := validateDefaultClass("partner", DirIngress); err == nil {
		t.Error("expected error: partner is egress, not ingress")
	}
	if err := validateDefaultClass("no-such-class", DirEgress); err == nil {
		t.Error("expected error for unknown class")
	}
}

func TestValidateCeilGeRate(t *testing.T) {
	if err := validateCeilGeRate("700mbit", "400mbit"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if err := validateCeilGeRate("400mbit", "400mbit"); err != nil {
		t.Errorf("equal ceil/rate should be valid: %v", err)
	}
	if err := validateCeilGeRate("300mbit", "400mbit"); err == nil {
		t.Error("expected error: ceil < rate")
	}
	// Unparseable ceil is an error (user-supplied; shell expressions are not accepted).
	if err := validateCeilGeRate("${SPEED}mbit", "100mbit"); err == nil {
		t.Error("expected error for unparseable ceil")
	}
	// Unparseable rate (legacy shell expression in defaultScriptData): skip the comparison.
	if err := validateCeilGeRate("100mbit", "$(( SPEED * 40 / 100 ))mbit"); err != nil {
		t.Errorf("unexpected error when rate is a legacy shell expression: %v", err)
	}
}

func TestClassInfoMap_AllClassesPresent(t *testing.T) {
	expected := []struct {
		name   string
		minor  string
		handle string
		dir    string
	}{
		{"publisher", "10", "110", DirIngress},
		{"backfill-response", "20", "120", DirIngress},
		{"reserve-ingress", "30", "130", DirIngress},
		{"partner", "40", "140", DirEgress},
		{"public", "50", "150", DirEgress},
		{"reserve-egress", "60", "160", DirEgress},
	}
	for _, e := range expected {
		ci, err := lookupClassInfo(e.name)
		if err != nil {
			t.Errorf("lookupClassInfo(%q): %v", e.name, err)
			continue
		}
		if ci.Minor != e.minor {
			t.Errorf("%s: Minor=%q, want %q", e.name, ci.Minor, e.minor)
		}
		if ci.Handle != e.handle {
			t.Errorf("%s: Handle=%q, want %q", e.name, ci.Handle, e.handle)
		}
		if ci.Dir != e.dir {
			t.Errorf("%s: Dir=%q, want %q", e.name, ci.Dir, e.dir)
		}
	}
}

func TestLookupClassInfo_UnknownClass(t *testing.T) {
	_, err := lookupClassInfo("no-such-class")
	if err == nil {
		t.Error("expected error for unknown class, got nil")
	}
}

func TestEffectiveCeil_DefaultsToRate(t *testing.T) {
	cls := &ClassConfig{Name: "partner", Rate: "400mbit", Ceil: ""}
	if cls.effectiveCeil() != "400mbit" {
		t.Errorf("effectiveCeil() = %q, want %q", cls.effectiveCeil(), "400mbit")
	}
	cls.Ceil = "700mbit"
	if cls.effectiveCeil() != "700mbit" {
		t.Errorf("effectiveCeil() = %q, want %q", cls.effectiveCeil(), "700mbit")
	}
}

// --- Device-only rendering tests ---
//
// When a device root is configured but no class configs exist yet,
// renderAndApplyScript branches on defaultEgressConfig(dev.Rate): an explicit
// rate yields the three default egress classes at proportional explicit rates
// (no SPEED variable); "auto" or any unparseable rate makes defaultEgressConfig
// fail and the render falls back to sysfs auto-detect. These tests pin that
// branch condition and the two rendered outcomes.

func TestDefaultEgressConfig_AutoRateFallsBack(t *testing.T) {
	// "auto" is not a parseable bandwidth, so defaultEgressConfig returns an
	// error — the signal renderAndApplyScript uses to fall back to sysfs detect.
	if _, _, err := defaultEgressConfig("auto"); err == nil {
		t.Error("expected defaultEgressConfig(\"auto\") to error, got nil")
	}
	// An explicit rate must succeed (→ explicit device-only render path).
	if _, _, err := defaultEgressConfig("500mbit"); err != nil {
		t.Errorf("defaultEgressConfig(\"500mbit\"): unexpected error: %v", err)
	}
}

func TestDeviceOnly_ExplicitRate_NoSpeed(t *testing.T) {
	// Device-only explicit render reuses the same path as renderAndApplyScript:
	// defaultEgressConfig(rate) → renderTcEgressScriptFromConfig(dev, classes).
	dev := &DeviceConfig{Dir: DirEgress, Rate: "500mbit", DefaultClass: "reserve-egress"}
	_, classes, err := defaultEgressConfig(dev.Rate)
	if err != nil {
		t.Fatalf("defaultEgressConfig: %v", err)
	}
	rendered, err := renderTcEgressScriptFromConfig("enp0s1", dev, classes)
	if err != nil {
		t.Fatalf("renderTcEgressScriptFromConfig: %v", err)
	}
	// No SPEED variable and no sysfs detection when the rate is explicit.
	if strings.Contains(rendered, "SPEED") {
		t.Errorf("device-only explicit render must not contain SPEED:\n%s", rendered)
	}
	// 500mbit → partner 200mbit/350mbit, public 150mbit/350mbit, reserve 150mbit/500mbit.
	if !strings.Contains(rendered, `htb rate "500mbit" ceil "500mbit"`) {
		t.Errorf("trunk 500mbit missing:\n%s", rendered)
	}
	if !strings.Contains(rendered, `rate "200mbit" ceil "350mbit"`) {
		t.Errorf("partner 200mbit/350mbit missing:\n%s", rendered)
	}
}

// --- resolveAutoRate tests ---

func newTestManager(nic string, mbit int, speedOK bool) *Manager {
	return NewManagerWithConfig(Config{
		NICDetect:   func() (string, error) { return nic, nil },
		SpeedDetect: func(string) (int, bool) { return mbit, speedOK },
	})
}

func TestResolveAutoRate_ResolvesToDetectedSpeed(t *testing.T) {
	m := newTestManager("eth0", 1000, true)
	dev := &DeviceConfig{Dir: DirEgress, Rate: "auto", DefaultClass: "reserve-egress"}
	m.resolveAutoRate(dev)
	if dev.Rate != "1gbit" {
		t.Errorf("expected auto resolved to 1gbit, got %q", dev.Rate)
	}
}

func TestResolveAutoRate_FallbackToDefaultWhenUnreadable(t *testing.T) {
	m := newTestManager("eth0", 0, false)
	dev := &DeviceConfig{Dir: DirEgress, Rate: "auto", DefaultClass: "reserve-egress"}
	m.resolveAutoRate(dev)
	want := FormatSpeedHint(DefaultLinkSpeedMbit)
	if dev.Rate != want {
		t.Errorf("expected unreadable sysfs to bake default %q, got %q", want, dev.Rate)
	}
	// Must not stay dynamic — the whole point is an explicit, SPEED-free rate.
	if dev.Rate == "auto" {
		t.Error("rate must not remain \"auto\" when sysfs is unreadable")
	}
}

func TestResolveAutoRate_IngressUntouched(t *testing.T) {
	m := newTestManager("eth0", 1000, true)
	dev := &DeviceConfig{Dir: DirIngress, Rate: "auto", DefaultClass: "reserve-ingress"}
	m.resolveAutoRate(dev)
	if dev.Rate != "auto" {
		t.Errorf("ingress \"auto\" must not be sysfs-resolved (per-veth), got %q", dev.Rate)
	}
}

func TestResolveAutoRate_ExplicitRateUntouched(t *testing.T) {
	m := newTestManager("eth0", 1000, true)
	dev := &DeviceConfig{Dir: DirEgress, Rate: "500mbit", DefaultClass: "reserve-egress"}
	m.resolveAutoRate(dev)
	if dev.Rate != "500mbit" {
		t.Errorf("explicit rate must be left unchanged, got %q", dev.Rate)
	}
}

func TestPersistEgressScript_NoServiceRestart(t *testing.T) {
	// `set` persists the boot script for reboot but must NOT restart the service:
	// its live `tc class change` already updated the kernel, and a restart would
	// tear down and rebuild the root qdisc. persistEgressScript writes; only
	// renderAndApplyScript restarts.
	scriptPath := filepath.Join(t.TempDir(), "tc-egress.sh")
	applied := false
	m := NewManagerWithConfig(Config{
		ScriptPath:  scriptPath,
		NICDetect:   func() (string, error) { return "eth0", nil },
		ApplyEgress: func(context.Context) error { applied = true; return nil },
	})

	if err := m.persistEgressScript("eth0"); err != nil {
		t.Fatalf("persistEgressScript: %v", err)
	}
	if applied {
		t.Error("persistEgressScript must not restart the service (no qdisc churn)")
	}
	if _, err := os.Stat(scriptPath); err != nil {
		t.Errorf("expected boot script persisted to disk: %v", err)
	}

	// Contrast: renderAndApplyScript does restart the service.
	applied = false
	if err := m.renderAndApplyScript(context.Background(), "eth0"); err != nil {
		t.Fatalf("renderAndApplyScript: %v", err)
	}
	if !applied {
		t.Error("renderAndApplyScript must restart the service")
	}
}

func TestParseBandwidthBps_RejectsZeroAndFractional(t *testing.T) {
	for _, bad := range []string{"0mbit", "0gbit", "1.5gbit", "0.5mbit", "1e3mbit", "-5mbit"} {
		if _, err := parseBandwidthBps(bad); err == nil {
			t.Errorf("parseBandwidthBps(%q): expected error, got nil", bad)
		}
	}
	// Positive integers still parse.
	if bps, err := parseBandwidthBps("1gbit"); err != nil || bps != 1_000_000_000 {
		t.Errorf("parseBandwidthBps(1gbit) = (%d, %v), want (1000000000, nil)", bps, err)
	}
}

func TestResolveAutoRateString(t *testing.T) {
	// "auto" with a readable speed resolves to the detected rate.
	if got := newTestManager("eth0", 1000, true).resolveAutoRateString("auto"); got != "1gbit" {
		t.Errorf("resolveAutoRateString(auto, readable) = %q, want 1gbit", got)
	}
	// case-insensitive.
	if got := newTestManager("eth0", 100, true).resolveAutoRateString("AUTO"); got != "100mbit" {
		t.Errorf("resolveAutoRateString(AUTO) = %q, want 100mbit", got)
	}
	// unreadable → default fallback.
	want := FormatSpeedHint(DefaultLinkSpeedMbit)
	if got := newTestManager("eth0", 0, false).resolveAutoRateString("auto"); got != want {
		t.Errorf("resolveAutoRateString(auto, unreadable) = %q, want %q", got, want)
	}
	// non-"auto" values pass through unchanged.
	if got := newTestManager("eth0", 1000, true).resolveAutoRateString("400mbit"); got != "400mbit" {
		t.Errorf("resolveAutoRateString(400mbit) = %q, want 400mbit (unchanged)", got)
	}
}

func TestValidateSumRates_AutoDeviceRate(t *testing.T) {
	// "auto" is not a parseable bandwidth; validateSumRates must skip the check.
	cfg := &ClassConfig{Name: "partner", Rate: "400mbit"}
	if err := validateSumRates(nil, cfg, "auto"); err != nil {
		t.Errorf("validateSumRates with device rate \"auto\" must skip: %v", err)
	}
}
