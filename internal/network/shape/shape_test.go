// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderTcEgressScript_NICInterpolated(t *testing.T) {
	dir := t.TempDir()

	const testNIC = "eth0"
	testPath := filepath.Join(dir, "solo-provisioner-tc-egress.sh")

	rendered, err := renderScript(testNIC)
	if err != nil {
		t.Fatalf("renderScript: %v", err)
	}

	// The NIC name must appear in the NIC= assignment line.
	if !strings.Contains(rendered, "NIC="+testNIC) {
		t.Errorf("rendered script does not contain NIC assignment %q:\n%s", "NIC="+testNIC, rendered)
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

// renderScript is a test-only helper that calls templates.Render directly so
// tests don't need to patch the global TcEgressScriptPath constant.
func renderScript(nicName string) (string, error) {
	return renderTcEgressScript(nicName, 0)
}

// --- DetectEgressInterface tests (cross-platform via detectEgressInterfaceFrom) ---

func TestDetectEgressInterfaceFrom_DefaultRoute(t *testing.T) {
	// Minimal /proc/net/route with one default-route row and one subnet row.
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
	// Default route on ens3, another interface present.
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
	// No row with RTF_GATEWAY set.
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
