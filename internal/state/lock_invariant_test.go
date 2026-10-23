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
	"RecordStartupPass", "AcquireComponentLocks", "AcquireProvisionerLock",
}

// fileCalls write, remove or move a file. They count only in the files that
// handle the state directory directly, since elsewhere they touch everything
// else: the state package and the uninstall cleanup. (Other packages have
// their own atomicWriteFile.)
var fileCalls = []string{"atomicWriteFile", "WriteFile", "Remove", "RemoveAll", "Rename"}

func handlesStateDir(rel string) bool {
	return (strings.HasPrefix(rel, "internal/state/") && !strings.Contains(strings.TrimPrefix(rel, "internal/state/"), "/")) ||
		rel == "internal/workflows/steps/step_cluster_uninstall.go"
}

const exemptStartupMigration = "exempt: a startup migration writes under .startup-migrations.lock, " +
	"which its invocation takes before any command locks a component"

const inStartupPass = "inside RecordStartupPass, which records the pass and compares the files the migration " +
	"declares: TestStateMigrations_TouchOnlyTheirDeclaredFiles"

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

	// The startup pass: one migrate/system entry for all its migrations' writes.
	"cmd/cli/commands/common/run.go:RunStartupMigrations": {
		lock: "takes .startup-migrations.lock when migrations are pending: TestRunStartupMigrations_ConcurrentInvocationsSplitStateOnce; " +
			"a stamp-only pass takes only the provisioner lock, inside PersistProvisionerVersion",
		history: "TestRunStartupMigrations_RecordsThePass",
	},
	"internal/state/startup_pass.go:appendEntry": {
		lock: "appends to action_history.yaml only, which no lock guards (one append per batch); RecordStartupPass's caller " +
			"holds .startup-migrations.lock when migrations are pending",
		history: "TestRecordStartupPass_RecordsOnePassEntry",
	},
	"internal/state/migration_per_component_state.go:Execute":                            {lock: exemptStartupMigration, history: inStartupPass},
	"internal/state/migration_per_component_state.go:Rollback":                           {lock: exemptStartupMigration, history: inStartupPass},
	"internal/state/migration_per_component_state.go:recomposeLegacyStateFromComponents": {lock: exemptStartupMigration, history: "called by Rollback: " + inStartupPass},
	"internal/state/migration_unified_state.go:Execute":                                  {lock: exemptStartupMigration, history: inStartupPass},
	"internal/state/migration_unified_state.go:Rollback":                                 {lock: exemptStartupMigration, history: inStartupPass},
	"internal/state/migration_helm_release_schema_v2.go:Execute":                         {lock: exemptStartupMigration, history: inStartupPass},
	"internal/state/migration_helm_release_schema_v2.go:Rollback":                        {lock: exemptStartupMigration, history: inStartupPass},
	"internal/state/migration_mgmt_ports_v1.go:Execute":                                  {lock: exemptStartupMigration, history: inStartupPass},
	"internal/state/migration_mgmt_ports_v1.go:Rollback":                                 {lock: exemptStartupMigration, history: inStartupPass},

	// Helpers: their callers have rows.
	"internal/state/atomic_write.go:atomicWriteFile":       {lock: "helper", history: "helper"},
	"internal/state/provisioner.go:writeProvisionerRecord": {lock: "helper", history: "helper"},
	"internal/state/state_manager.go:commitComponentFlush": {lock: "inside Commit and writeUnrecorded", history: "inside Commit and writeUnrecorded"},

	// kube cluster uninstall.
	"internal/workflows/steps/step_cluster_uninstall.go:CleanupWeaverFiles": {
		lock:    "kube cluster uninstall holds every state lock: TestLockAllState_FailsFastOnEveryStateLock",
		history: "none yet: it deletes action_history.yaml with the rest of the state dir",
	},
	"internal/workflows/steps/step_cluster_uninstall.go:RemoveSystemdServiceFiles": {lock: "not state: systemd unit files", history: "not state"},
	"internal/workflows/steps/step_cluster_uninstall.go:RemoveConfigDirectories":   {lock: "not state: configuration directories", history: "not state"},
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
			rel = filepath.ToSlash(rel)
			calls := writerCalls
			if handlesStateDir(rel) {
				calls = append(slices.Clone(writerCalls), fileCalls...)
			}
			for _, decl := range writerSitesIn(t, path, calls) {
				found[rel+":"+decl] = true
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
// writerCalls (the names given): a function or method name, or a variable's name for a call made
// inside its initializer.
func writerSitesIn(t *testing.T, path string, writerCalls []string) []string {
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

// TestStartupMigrationsOutsideStateWriteNoState: the startup pass compares only
// the files a migration declares, and only migrations in this package can
// declare them. A host migration that wrote state would go unrecorded.
func TestStartupMigrationsOutsideStateWriteNoState(t *testing.T) {
	root := repoRoot(t)
	paths, err := filepath.Glob(filepath.Join(root, "internal", "workflows", "migration_*.go"))
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		rel, err := filepath.Rel(root, path)
		require.NoError(t, err)
		require.Emptyf(t, writerSitesIn(t, path, writerCalls), "%s writes state; startup migrations that write state belong in internal/state", rel)
		require.Falsef(t, usesIdent(t, path, "StateDir"), "%s touches the state directory; startup migrations that write state belong in internal/state", rel)
	}
}

// usesIdent reports whether the Go file at path names the identifier name,
// on its own or as a selector, so a comment or a string mentioning it doesn't
// count.
func usesIdent(t *testing.T, path, name string) bool {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	require.NoError(t, err)
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			found = true
		}
		return !found
	})
	return found
}
