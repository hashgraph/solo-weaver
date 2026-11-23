// SPDX-License-Identifier: Apache-2.0

package common

import (
	"testing"

	"github.com/hashgraph/solo-weaver/internal/ui"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// newGuardedCommand returns a subcommand under a root carrying --force, with
// --override-drift registered when overridable.
func newGuardedCommand(t *testing.T, overridable bool) (root, cmd *cobra.Command) {
	t.Helper()
	var force, override bool
	root = &cobra.Command{Use: "root"}
	FlagForce().SetVarP(root, &force, false)
	cmd = &cobra.Command{Use: "uninstall"}
	if overridable {
		FlagOverrideDrift().SetVar(cmd, &override, false)
	}
	root.AddCommand(cmd)
	return root, cmd
}

func TestExtractDriftGuardFlags(t *testing.T) {
	cases := []struct {
		name           string
		overridable    bool
		force          bool
		override       bool
		nonInteractive bool
		want           models.CommonInputs
	}{
		{"nothing set", true, false, false, false, models.CommonInputs{}},
		{"--override-drift", true, false, true, false, models.CommonInputs{OverrideDrift: true}},
		{"--force", true, true, false, false, models.CommonInputs{Force: true}},
		{"non-interactive run", true, false, false, true, models.CommonInputs{NonInteractive: true}},
		{"command without --override-drift", false, false, false, false, models.CommonInputs{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prev := ui.NonInteractive
			ui.NonInteractive = tc.nonInteractive
			t.Cleanup(func() { ui.NonInteractive = prev })
			root, cmd := newGuardedCommand(t, tc.overridable)
			if tc.force {
				require.NoError(t, root.PersistentFlags().Set(FlagForce().Name, "true"))
			}
			if tc.override {
				require.NoError(t, cmd.Flags().Set(FlagOverrideDrift().Name, "true"))
			}

			var got models.CommonInputs
			require.NoError(t, ExtractDriftGuardFlags(cmd, nil, &got))

			require.Equal(t, tc.want, got)
		})
	}
}
