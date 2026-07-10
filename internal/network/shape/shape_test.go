// SPDX-License-Identifier: Apache-2.0

package shape

import (
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
	if err := RenderTcEgressScript("", 0); err == nil {
		t.Error("expected error for empty NIC name, got nil")
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
	if !contentEqual(string(content), rendered) {
		t.Error("content changed on second write")
	}
}

// renderScript is a test-only helper that calls renderTcEgressScript so tests
// don't need to patch the global TcEgressScriptPath constant.
func renderScript(nicName string) (string, error) {
	return renderTcEgressScript(nicName, 0)
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

func TestRenderTcEgressScript_BakedSpeed(t *testing.T) {
	rendered, err := renderTcEgressScript("eth0", 1000)
	if err != nil {
		t.Fatalf("renderTcEgressScript: %v", err)
	}
	if !strings.Contains(rendered, "SPEED=1000") {
		t.Errorf("rendered script missing baked SPEED=1000:\n%s", rendered)
	}
	if strings.Contains(rendered, `cat /sys/class/net`) {
		t.Errorf("rendered script should not contain sysfs detection when speed is baked in:\n%s", rendered)
	}
}

func TestRenderTcEgressScript_AutoDetectSpeed(t *testing.T) {
	rendered, err := renderTcEgressScript("eth0", 0)
	if err != nil {
		t.Fatalf("renderTcEgressScript: %v", err)
	}
	if !strings.Contains(rendered, `cat /sys/class/net/"$NIC"/speed`) {
		t.Errorf("rendered script missing sysfs detection when speedMbit=0:\n%s", rendered)
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
