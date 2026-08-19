// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// prevPath returns the retained-generation path beside a manager's config.
func prevPath(m *Manager) string { return m.configPath + HostConfigPrevSuffix }

// readFile is a require-wrapped ReadFile for the assertions below.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func TestRetainPreviousConfig(t *testing.T) {
	t.Run("first apply retains nothing", func(t *testing.T) {
		r := &fakeRunner{}
		applies := 0
		m, _ := newTestManager(t, r, &applies)

		require.NoError(t, m.Apply(context.Background(), sampleTable()))

		// There was no previous generation, and inventing one would misrepresent
		// the host's history.
		require.NoFileExists(t, prevPath(m))
		require.FileExists(t, m.configPath)
	})

	t.Run("second apply retains generation one byte-for-byte", func(t *testing.T) {
		r := &fakeRunner{}
		applies := 0
		m, _ := newTestManager(t, r, &applies)

		require.NoError(t, m.Apply(context.Background(), sampleTable()))
		gen1 := readFile(t, m.configPath)

		require.NoError(t, m.Apply(context.Background(), allowTable()))
		gen2 := readFile(t, m.configPath)

		require.Equal(t, gen1, readFile(t, prevPath(m)),
			"the retained copy must be the exact bytes of the config it replaced")
		require.NotEqual(t, gen1, gen2, "the second apply should have changed the config")
	})

	t.Run("retained copy is one generation deep", func(t *testing.T) {
		r := &fakeRunner{}
		applies := 0
		m, _ := newTestManager(t, r, &applies)

		require.NoError(t, m.Apply(context.Background(), sampleTable()))
		require.NoError(t, m.Apply(context.Background(), dualStackTable()))
		gen2 := readFile(t, m.configPath)
		require.NoError(t, m.Apply(context.Background(), allowTable()))

		// Not a version history: generation 1 is gone, replaced by generation 2.
		require.Equal(t, gen2, readFile(t, prevPath(m)))
	})

	t.Run("retained copy always parses back to a table", func(t *testing.T) {
		r := &fakeRunner{}
		applies := 0
		m, _ := newTestManager(t, r, &applies)

		require.NoError(t, m.Apply(context.Background(), allowTable()))
		require.NoError(t, m.Apply(context.Background(), sampleTable()))

		cfg, err := ParseConfig([]byte(readFile(t, prevPath(m))))
		require.NoError(t, err, "the retained generation must be loadable, or it is useless in a recovery")
		tbl, err := cfg.Table()
		require.NoError(t, err)
		// The named allow rules are what the nft-reparse fallback loses, so they
		// are the reason this artifact exists.
		require.ElementsMatch(t,
			[]string{"k8s-node", "cilium-vxlan", "admin"},
			allowRuleNames(tbl))
	})

	t.Run("a corrupt current config is not promoted over a good retained copy", func(t *testing.T) {
		r := &fakeRunner{}
		applies := 0
		m, _ := newTestManager(t, r, &applies)

		require.NoError(t, m.Apply(context.Background(), allowTable()))
		require.NoError(t, m.Apply(context.Background(), sampleTable()))
		good := readFile(t, prevPath(m))

		// Truncation is the failure this artifact exists for.
		require.NoError(t, os.WriteFile(m.configPath, []byte("mgmt:\n  cidrs:\n"), 0o600))
		require.NoError(t, m.Apply(context.Background(), dualStackTable()))

		require.Equal(t, good, readFile(t, prevPath(m)),
			"recovering from the retained copy must not consume it by promoting the corrupt config")
	})

	t.Run("nothing is written when the dry run rejects the ruleset", func(t *testing.T) {
		r := &fakeRunner{}
		applies := 0
		m, _ := newTestManager(t, r, &applies)

		require.NoError(t, m.Apply(context.Background(), sampleTable()))
		gen1 := readFile(t, m.configPath)

		r.checkErr = os.ErrInvalid
		require.Error(t, m.Apply(context.Background(), allowTable()))

		// Retention sits after the dry run, so a rejected ruleset must not have
		// rotated the generations either.
		require.Equal(t, gen1, readFile(t, m.configPath))
		require.NoFileExists(t, prevPath(m))
	})

	t.Run("delete removes the retained copy", func(t *testing.T) {
		r := &fakeRunner{}
		applies := 0
		m, nftPath := newTestManager(t, r, &applies)

		require.NoError(t, m.Apply(context.Background(), sampleTable()))
		require.NoError(t, m.Apply(context.Background(), allowTable()))
		require.FileExists(t, prevPath(m))

		require.NoError(t, m.Delete(context.Background()))

		// Left behind, a later create on this host would inherit a "previous"
		// config belonging to a table that no longer exists.
		require.NoFileExists(t, prevPath(m))
		require.NoFileExists(t, m.configPath)
		require.NoFileExists(t, nftPath)
	})
}

func TestReapply(t *testing.T) {
	t.Run("re-applies the persisted table unchanged", func(t *testing.T) {
		r := &fakeRunner{}
		applies := 0
		m, nftPath := newTestManager(t, r, &applies)

		require.NoError(t, m.Apply(context.Background(), allowTable()))
		cfgBefore := readFile(t, m.configPath)
		nftBefore := readFile(t, nftPath)
		require.Equal(t, 1, applies)

		require.NoError(t, m.Reapply(context.Background()))

		require.Equal(t, cfgBefore, readFile(t, m.configPath), "reapply must not change the persisted intent")
		require.Equal(t, nftBefore, readFile(t, nftPath), "reapply must render the same ruleset")
		require.Equal(t, 2, applies, "reapply must actually reload the kernel, not short-circuit")
	})

	t.Run("scopes its flush to the weaver table", func(t *testing.T) {
		r := &fakeRunner{}
		applies := 0
		m, _ := newTestManager(t, r, &applies)

		require.NoError(t, m.Apply(context.Background(), sampleTable()))
		require.NoError(t, m.Reapply(context.Background()))

		// The scoped-replace prefix is what keeps a third-party ruleset on the
		// host alive across a re-apply.
		doc := r.checked[len(r.checked)-1]
		require.Contains(t, doc, "delete table "+TableName)
		require.NotContains(t, doc, "flush ruleset")
	})

	t.Run("errors when nothing is persisted", func(t *testing.T) {
		r := &fakeRunner{}
		applies := 0
		m, _ := newTestManager(t, r, &applies)

		err := m.Reapply(context.Background())

		// Falling through to a default table would apply a default-drop policy
		// with an empty management allowlist, i.e. a lock-out.
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
		require.Equal(t, 0, applies)
	})

	t.Run("retains the generation it replaces", func(t *testing.T) {
		r := &fakeRunner{}
		applies := 0
		m, _ := newTestManager(t, r, &applies)

		require.NoError(t, m.Apply(context.Background(), allowTable()))
		cfg := readFile(t, m.configPath)

		require.NoError(t, m.Reapply(context.Background()))

		// Identical content, but it went through applyAndPersist like any other
		// verb, so the generation rotated.
		require.Equal(t, cfg, readFile(t, prevPath(m)))
	})

	t.Run("recovery from the retained copy restores named allow rules", func(t *testing.T) {
		r := &fakeRunner{}
		applies := 0
		m, _ := newTestManager(t, r, &applies)

		require.NoError(t, m.Apply(context.Background(), allowTable()))
		require.NoError(t, m.Apply(context.Background(), sampleTable()))

		// The documented recovery: promote the retained copy, then reapply.
		require.NoError(t, os.WriteFile(m.configPath, []byte(readFile(t, prevPath(m))), 0o600))
		require.NoError(t, m.Reapply(context.Background()))

		tbl, err := m.Table(context.Background())
		require.NoError(t, err)
		require.ElementsMatch(t,
			[]string{"k8s-node", "cilium-vxlan", "admin"},
			allowRuleNames(tbl))
	})
}

// TestPrevConfigPathDerivation pins the production path so a rename of either
// constant cannot silently point the retained copy at another directory.
func TestPrevConfigPathDerivation(t *testing.T) {
	require.Equal(t, HostConfigPath+".prev", HostConfigPrevPath)
	require.Equal(t, filepath.Dir(HostConfigPath), filepath.Dir(HostConfigPrevPath))

	m := NewManager()
	require.Equal(t, HostConfigPrevPath, m.prevConfigPath)
}

// allowRuleNames returns the names of a table's named allow rules.
func allowRuleNames(t *Table) []string {
	names := make([]string, 0, len(t.Allow))
	for _, r := range t.Allow {
		names = append(names, r.Name)
	}
	return names
}
