// SPDX-License-Identifier: Apache-2.0

package state

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// writerCalls are the calls that write a state file or take the lock guarding
// one. A method of any type with one of these names counts, so the scan can
// over-match but never miss a call.
var writerCalls = []string{
	"Commit", "Record", "writeUnrecorded", "RunOwning", "writeProvisionerRecord",
	"AcquireComponentLocks", "AcquireProvisionerLock",
}

const exemptStartupMigration = "exempt: a startup migration writes under .startup-migrations.lock, " +
	"which its invocation takes before any command locks a component"

// writerRow says how one writer site holds the lock for what it writes, and
// how what it writes reaches the action history. Each names the test that
// shows it, or says why there is none.
type writerRow struct{ lock, history string }

// stateWriters accounts for every non-test call of writerCalls, keyed by
// "<path>:<enclosing declaration>". It is a checklist, not a proof: the test
// fails when a call that writes state, or takes a state lock, appears or moves
// without a row saying how that site holds its lock and records its entry.
//
// Known gap: the daemon runs as the weaver user and cannot open the root-only
// 0600 lock files (lock.go), so it must not write state until that is settled.
var stateWriters = map[string]writerRow{
	// HandleIntent locks the owned set for the whole command; flushState writes it.
	"internal/bll/base_handler.go:HandleIntent": {
		lock:    "TestHandleIntent_FailsFastOnEachOwnedLock",
		history: "takes no history call: flushState records the entry",
	},
	"internal/bll/base_handler.go:flushState": {
		lock:    "TestHandleIntent_FailsFastOnEachOwnedLock",
		history: "TestFlushState_RecordsEntryOnEveryExitAfterTheWorkflow",
	},
	"internal/rsl/runtime.go:Commit": {
		lock:    "wrapper: its callers are BaseHandler.flushState and kube cluster install",
		history: "wrapper over state.Writer.Commit: TestCommit_RecordsTheFilesItChanged",
	},
	"internal/rsl/runtime.go:Record": {
		lock:    "wrapper: writes no state file",
		history: "wrapper over state.Writer.Record: TestRecord_RendersTheCauseForTheOperator",
	},

	// RunOwning locks exactly the ids it flushes.
	"internal/state/owned.go:RunOwning": {
		lock:    "TestRunOwning_FailsFastWhenLocked",
		history: "TestRunOwning_RecordsEntryOnEveryExit",
	},
	"cmd/cli/commands/network/firewall/state.go:applyHostFirewallChange": {
		lock:    "TestFirewallVerbs_FailFastBeforeApplyingWhenTheFirewallIsLocked",
		history: "TestFirewallVerbs_RecordAnEntry",
	},
	"cmd/cli/commands/kube/cluster/install.go:installCmd": {
		lock: "no unit test runs installCmd: it locks ComponentSystem for the " +
			"whole command, then flushes only that; shown by the manual UAT's kube cluster install step",
		history: "takes no history call: recordClusterInstall records the entry once the workflow started",
	},
	"cmd/cli/commands/kube/cluster/install.go:recordClusterInstall": {
		lock: "its caller, installCmd, holds ComponentSystem's lock for the whole command",
		history: "TestRecordClusterInstall_SuccessRecordsOneEntry, TestRecordClusterInstall_FailedInstallRecordsAFailedEntry, " +
			"TestRecordClusterInstall_FailedRefreshIsNotPersisted",
	},

	"cmd/cli/commands/kube/cluster/uninstall.go:lockAllState": {
		lock:    "TestLockAllState_FailsFastOnEveryStateLock",
		history: "takes locks only",
	},
	"internal/state/provisioner.go:persistProvisionerVersionIn": {
		lock:    "TestPersistProvisionerVersion_FailsWhenLockedPastTheWait",
		history: "the entry of the command whose workflow ran the version step names provisioner: TestProvisionerVersionPersisted_ReadsTheStepsMetadata",
	},

	"internal/state/migration_per_component_state.go:Execute": {
		lock:    exemptStartupMigration,
		history: "none yet: the startup pass is not recorded",
	},
	"internal/state/migration_unified_state.go:Execute": {
		lock:    exemptStartupMigration,
		history: "none yet: the startup pass is not recorded",
	},
}

// Fails when code that writes state, or takes a state lock, is added or moved
// without saying how it holds the lock for what it writes.
func TestEveryStateWriterIsAccountedFor(t *testing.T) {
	root := repoRoot(t)

	found := map[string]bool{}
	for _, dir := range []string{"cmd", "internal", "pkg"} {
		require.NoError(t, filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			for _, decl := range writerSitesIn(t, path) {
				found[filepath.ToSlash(rel)+":"+decl] = true
			}
			return nil
		}))
	}

	var unknown, stale []string
	for site := range found {
		if _, ok := stateWriters[site]; !ok {
			unknown = append(unknown, site)
		}
	}
	for site := range stateWriters {
		if !found[site] {
			stale = append(stale, site)
		}
	}
	sort.Strings(unknown)
	sort.Strings(stale)
	require.Empty(t, unknown, "new state writers: add each to stateWriters with the tests proving it holds its lock and records its entry")
	require.Empty(t, stale, "stateWriters rows with no matching call left: remove them")

	tests := testFuncNames(t, root)
	for site, row := range stateWriters {
		for _, how := range []string{row.lock, row.history} {
			require.NotEmptyf(t, how, "%s must say how it holds its lock and records its entry", site)
			for _, name := range regexp.MustCompile(`\bTest\w+`).FindAllString(how, -1) {
				require.Truef(t, tests[name], "%s names %s, which does not exist", site, name)
			}
		}
	}
}

// writerSitesIn returns the top-level declarations in path that call one of
// writerCalls: a function or method name, or a variable's name for a call made
// inside its initializer.
func writerSitesIn(t *testing.T, path string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	require.NoError(t, err)

	var sites []string
	for _, decl := range file.Decls {
		name := ""
		switch d := decl.(type) {
		case *ast.FuncDecl:
			name = d.Name.Name
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if vs, ok := spec.(*ast.ValueSpec); ok && len(vs.Names) > 0 {
					name = vs.Names[0].Name
				}
			}
		}
		calls := false
		ast.Inspect(decl, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fn := call.Fun.(type) {
			case *ast.SelectorExpr:
				calls = calls || slices.Contains(writerCalls, fn.Sel.Name)
			case *ast.Ident:
				calls = calls || slices.Contains(writerCalls, fn.Name)
			}
			return true
		})
		if calls {
			sites = append(sites, name)
		}
	}
	return sites
}

// testFuncNames returns the name of every test function in the repository.
func testFuncNames(t *testing.T, root string) map[string]bool {
	t.Helper()
	re := regexp.MustCompile(`(?m)^func (Test\w+)\(`)
	names := map[string]bool{}
	for _, dir := range []string{"cmd", "internal", "pkg"} {
		require.NoError(t, filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
				return err
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range re.FindAllStringSubmatch(string(b), -1) {
				names[m[1]] = true
			}
			return nil
		}))
	}
	return names
}

// repoRoot returns the module root, two levels above this package.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, "go.mod"))
	require.NoError(t, err, "expected the module root at %s", root)
	return root
}
