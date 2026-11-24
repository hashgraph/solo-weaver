// SPDX-License-Identifier: Apache-2.0

package featuregate

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoOrphanFeatures fails when a catalogued feature has no reader — no
// `featuregate.<Feature>` reference in production code outside this package (a
// gate.Enabled / GateFeature / GateFeatureFlag call).
//
// This is the guard that keeps the catalog honest regardless of the activation
// version (which solo freezes at 1, so the time-based lints never fire): an inert
// gate — e.g. a GA feature whose old `else` path was deleted — has no reader and must
// be removed (the contract step), not left as a permanent always-on row.
func TestNoOrphanFeatures(t *testing.T) {
	root := moduleRoot(t)
	selfPkg := filepath.Join(root, "internal", "featuregate")

	// One word-boundaried matcher per feature; the constant name equals its string value.
	type matcher struct {
		name string
		re   *regexp.Regexp
	}
	var matchers []matcher
	for f := range Catalog() {
		n := string(f)
		matchers = append(matchers, matcher{n, regexp.MustCompile(`featuregate\.` + regexp.QuoteMeta(n) + `\b`)})
	}

	seen := map[string]bool{}
	scanned := 0
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			// Skip non-source trees and the package that defines the constants.
			if n := e.Name(); n == "vendor" || n == ".git" || n == "testdata" || filepath.Clean(path) == selfPkg {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		scanned++
		for _, m := range matchers {
			if !seen[m.name] && m.re.Match(src) {
				seen[m.name] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if scanned == 0 {
		t.Fatalf("scanned no .go files under %q — repo root resolution is wrong", root)
	}

	for _, m := range matchers {
		if !seen[m.name] {
			t.Errorf("feature %q is catalogued but has no reader in production code "+
				"(no gate.Enabled / GateFeature / GateFeatureFlag reference) — if it is a stable "+
				"capability, remove the gate (the contract step)", m.name)
		}
	}
}

// moduleRoot walks up from the working directory to the directory containing go.mod.
// This is independent of build flags (unlike runtime.Caller under -trimpath).
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above the working directory")
		}
		dir = parent
	}
}
