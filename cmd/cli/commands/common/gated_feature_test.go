// SPDX-License-Identifier: Apache-2.0

package common

import (
	"testing"

	"github.com/spf13/cobra"
)

// testFeature is a minimal gatedFeature used to exercise resolveFeatureGate in
// isolation from the firewall/traffic-shaping specifics.
func testFeature() gatedFeature {
	return gatedFeature{
		GateFlag:     "feature-enabled",
		Noun:         "the test feature",
		PromptTitle:  "Enable the test feature?",
		PromptDesc:   "desc",
		ContentFlags: []string{"feature-content"},
	}
}

// newGateCmd builds a command with the gate flag, one content flag, and --force
// registered. Tests pass --force so ShouldPrompt is always false: the confirm
// prompt is skipped and the seed/flag/mismatch-guard logic runs deterministically
// without a TTY.
func newGateCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "x", RunE: func(*cobra.Command, []string) error { return nil }}
	cmd.Flags().Bool("feature-enabled", false, "")
	cmd.Flags().String("feature-content", "", "")
	var force bool
	FlagForce().SetVarP(cmd, &force, false)
	return cmd
}

func TestResolveFeatureGate_SeedAndFlag(t *testing.T) {
	cases := []struct {
		name    string
		setGate string // "" = leave unset, else the value to Set
		seed    bool
		want    bool
	}{
		{"unset seeds enabled", "", true, true},
		{"unset seeds disabled", "", false, false},
		{"flag true overrides seed false", "true", false, true},
		{"flag false overrides seed true", "false", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd := newGateCmd()
			if c.setGate != "" {
				if err := cmd.Flags().Set("feature-enabled", c.setGate); err != nil {
					t.Fatalf("set gate: %v", err)
				}
			}
			got, err := resolveFeatureGate(cmd, []string{"--force"}, testFeature(), c.seed)
			if err != nil {
				t.Fatalf("resolveFeatureGate: %v", err)
			}
			if got != c.want {
				t.Errorf("enabled = %v, want %v", got, c.want)
			}
		})
	}
}

func TestResolveFeatureGate_ContentFlagWithoutGateErrors(t *testing.T) {
	cmd := newGateCmd()
	if err := cmd.Flags().Set("feature-content", "x"); err != nil {
		t.Fatalf("set content: %v", err)
	}
	// Gate left off (seed=false, flag unset): supplying a content flag must be a
	// hard error rather than silently dropping the value.
	_, err := resolveFeatureGate(cmd, []string{"--force"}, testFeature(), false)
	if err == nil {
		t.Fatal("expected an error when a content flag is set without the gate, got nil")
	}
}

func TestResolveFeatureGate_ContentFlagAllowedWhenGated(t *testing.T) {
	cmd := newGateCmd()
	if err := cmd.Flags().Set("feature-enabled", "true"); err != nil {
		t.Fatalf("set gate: %v", err)
	}
	if err := cmd.Flags().Set("feature-content", "x"); err != nil {
		t.Fatalf("set content: %v", err)
	}
	got, err := resolveFeatureGate(cmd, []string{"--force"}, testFeature(), false)
	if err != nil {
		t.Fatalf("resolveFeatureGate: %v", err)
	}
	if !got {
		t.Error("enabled = false, want true when gate flag is set")
	}
}
