// SPDX-License-Identifier: Apache-2.0

package featuregate

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
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
	// Repo root, independent of the test's working directory.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
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
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if e.IsDir() {
				if filepath.Clean(path) == selfPkg { // the package that defines the constants
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
			for _, m := range matchers {
				if !seen[m.name] && m.re.Match(src) {
					seen[m.name] = true
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}

	for _, m := range matchers {
		if !seen[m.name] {
			t.Errorf("feature %q is catalogued but has no reader in production code "+
				"(no gate.Enabled / GateFeature / GateFeatureFlag reference) — if it is a stable "+
				"capability, remove the gate (the contract step)", m.name)
		}
	}
}
