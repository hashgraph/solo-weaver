// SPDX-License-Identifier: Apache-2.0

package featuregate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
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

	// The constant name equals its string value.
	names := map[string]bool{}
	for f := range Catalog() {
		names[string(f)] = true
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
		refs, err := referencedFeatures(path, src, names)
		if err != nil {
			return err
		}
		for n := range refs {
			seen[n] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if scanned == 0 {
		t.Fatalf("scanned no .go files under %q — repo root resolution is wrong", root)
	}

	for n := range names {
		if !seen[n] {
			t.Errorf("feature %q is catalogued but has no reader in production code "+
				"(no gate.Enabled / GateFeature / GateFeatureFlag reference) — if it is a stable "+
				"capability, remove the gate (the contract step)", n)
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

const featuregatePkgPath = "github.com/hashgraph/solo-weaver/internal/featuregate"

// referencedFeatures returns the names in `names` that src references as
// <featuregate>.<Name>, honouring however the file imports the package (default name or
// an alias). Blank and dot imports are skipped: they cannot be read as selectors.
func referencedFeatures(filename string, src []byte, names map[string]bool) (map[string]bool, error) {
	f, err := parser.ParseFile(token.NewFileSet(), filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	local := ""
	for _, imp := range f.Imports {
		if imp.Path.Value != `"`+featuregatePkgPath+`"` {
			continue
		}
		local = "featuregate"
		if imp.Name != nil {
			local = imp.Name.Name
		}
	}
	found := map[string]bool{}
	if local == "" || local == "_" || local == "." {
		return found, nil
	}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == local && names[sel.Sel.Name] {
			found[sel.Sel.Name] = true
		}
		return true
	})
	return found, nil
}

func TestReferencedFeatures_HonoursImportAlias(t *testing.T) {
	names := map[string]bool{"Alpha": true, "Beta": true, "Gamma": true}
	const body = `package p
import %s "` + featuregatePkgPath + `"
func f() { _ = %s.Alpha; _ = other.Beta }
`
	cases := []struct {
		name, imp, use string
		want           []string
	}{
		{"default name", "", "featuregate", []string{"Alpha"}},
		{"alias", "fg", "fg", []string{"Alpha"}},
		{"blank import", "_", "featuregate", nil},
		{"unrelated selector is ignored", "fg", "featuregate", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := []byte(strings.NewReplacer("%s.Alpha", tc.use+".Alpha").Replace(
				strings.Replace(body, "import %s", "import "+tc.imp, 1)))
			got, err := referencedFeatures("x.go", src, names)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for _, w := range tc.want {
				if !got[w] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}
