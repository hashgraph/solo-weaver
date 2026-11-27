// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

var updateGolden = flag.Bool("update-golden", false, "update golden files")

// visibleSurface renders the user-facing command/flag surface of root: every
// non-hidden command (top level and one level down) and every non-hidden root
// persistent flag, sorted and deterministic. It deliberately excludes help prose,
// version and build info — only the surface an end user can discover in --help.
func visibleSurface(root *cobra.Command) string {
	var b strings.Builder
	b.WriteString("# visible commands (top level + one level down)\n")

	tops := make([]*cobra.Command, 0, len(root.Commands()))
	for _, c := range root.Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		tops = append(tops, c)
	}
	sort.Slice(tops, func(i, j int) bool { return tops[i].Name() < tops[j].Name() })
	for _, c := range tops {
		b.WriteString(c.Name() + "\n")
		subs := make([]string, 0, len(c.Commands()))
		for _, s := range c.Commands() {
			if s.Hidden || s.Name() == "help" {
				continue
			}
			subs = append(subs, s.Name())
		}
		sort.Strings(subs)
		for _, s := range subs {
			b.WriteString("  " + s + "\n")
		}
	}

	b.WriteString("\n# visible root persistent flags\n")
	flags := make([]string, 0)
	root.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		flags = append(flags, "--"+f.Name)
	})
	sort.Strings(flags)
	for _, f := range flags {
		b.WriteString(f + "\n")
	}
	return b.String()
}

// TestRootHelpSurface_Golden pins the visible --help surface so that exposing a
// new command or flag (e.g. forgetting to gate an alpha feature) shows up as a
// reviewable diff rather than a surprise in production. When the change is
// intended (a feature graduated), regenerate with -update-golden.
func TestRootHelpSurface_Golden(t *testing.T) {
	got := visibleSurface(rootCmd)
	golden := filepath.Join("testdata", "help-surface.golden")

	if *updateGolden {
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile(golden, []byte(got), 0o644))
	}

	want, err := os.ReadFile(golden)
	require.NoError(t, err,
		"missing golden file; run: go test ./cmd/cli/commands -run TestRootHelpSurface_Golden -update-golden")
	require.Equal(t, string(want), got,
		"the visible --help surface changed. If intended (a feature graduated), re-run with -update-golden; "+
			"otherwise a command/flag may be exposed that should be gated.")
}
