// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteChecksums verifies the in-place rewrite targets exactly the right
// (chart, version) checksum line, leaves other charts untouched, and preserves
// surrounding structure (comments, ordering, quoting).
func TestWriteChecksums(t *testing.T) {
	catalog := strings.Join([]string{
		"cluster:",
		"- name: alloy",
		"  type: classic",
		"  versions:",
		"    1.4.0:",
		"      algorithm: 'sha256'",
		"      checksum: 'OLD_ALLOY'",
		"- name: solo-operator",
		"  type: oci",
		"  # keep this comment",
		"  versions:",
		"    0.6.0:",
		"      algorithm: 'sha256'",
		"      checksum: 'OLD_OP'",
		"",
	}, "\n")

	p := filepath.Join(t.TempDir(), "catalog.yaml")
	if err := os.WriteFile(p, []byte(catalog), 0o644); err != nil {
		t.Fatal(err)
	}

	// Only solo-operator's digest changes; alloy is intentionally absent.
	digests := map[string]map[string]string{
		"solo-operator": {"0.6.0": "NEWDIGEST"},
	}
	if err := writeChecksums(p, digests); err != nil {
		t.Fatalf("writeChecksums: %v", err)
	}

	out, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)

	if !strings.Contains(s, "checksum: 'NEWDIGEST'") {
		t.Errorf("solo-operator checksum was not updated:\n%s", s)
	}
	if strings.Contains(s, "OLD_OP") {
		t.Errorf("stale solo-operator checksum still present:\n%s", s)
	}
	if !strings.Contains(s, "checksum: 'OLD_ALLOY'") {
		t.Errorf("alloy checksum (not in digests) must be untouched:\n%s", s)
	}
	if !strings.Contains(s, "# keep this comment") {
		t.Errorf("comments must be preserved:\n%s", s)
	}
}
