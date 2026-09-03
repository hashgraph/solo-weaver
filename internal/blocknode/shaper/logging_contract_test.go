// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package shaper

import (
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPackageEmitsNoLogs guards the contract that makes `reconcile-shaper
// --check --output json` machine-readable: this package must not log.
//
// Under --output json the root command routes every log line to stdout as
// NDJSON, and stdout is where the digest document is written and where
// privexec.ReconcileShaperCheck parses it back. One log line anywhere on the
// Check path therefore makes the daemon's json.Unmarshal fail, which faults the
// statusz poll loop into a retry-forever backoff — the exact failure the
// resolution work exists to remove. The worker still exits 0, so nothing else
// catches it: every hand-run of the verb looks fine, and only the daemon leg
// shows the fault.
//
// The rule is "no logging at all" rather than "no logging on the Check path"
// because the two paths share this package, and which helpers Check reaches is
// not something a reader can verify at the point they add a log line. Report
// diagnostics by returning them (see hostResolution.unresolved, surfaced through
// CheckResult.Unresolved) and let the command layer decide where they go.
func TestPackageEmitsNoLogs(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ImportsOnly)
	require.NoError(t, err)

	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			for _, imp := range file.Imports {
				require.NotContains(t, imp.Path.Value, "logx",
					"%s imports a logger: this package must not log, or --check --output json "+
						"stops being parseable by the daemon. Return the diagnostic instead.", path)
			}
		}
	}
}
