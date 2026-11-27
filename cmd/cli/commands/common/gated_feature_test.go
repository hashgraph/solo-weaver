// SPDX-License-Identifier: Apache-2.0

package common

import (
	"bytes"
	"testing"

	"github.com/automa-saga/logx"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
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

// The debug log says what decided the feature, so a run that turned it on or
// off can be traced without the flags it was given.
func TestResolveFeatureGate_LogsWhatDecided(t *testing.T) {
	for name, tc := range map[string]struct {
		setGate string
		want    string
	}{
		"flag": {"false", `"feature":"the test feature","source":"flag","enabled":false`},
		"seed": {"", `"feature":"the test feature","source":"seed","enabled":true`},
	} {
		t.Run(name, func(t *testing.T) {
			buf := captureLogs(t)
			cmd := newGateCmd()
			if tc.setGate != "" {
				require.NoError(t, cmd.Flags().Set("feature-enabled", tc.setGate))
			}

			_, err := resolveFeatureGate(cmd, []string{"--force"}, testFeature(), true)
			require.NoError(t, err)

			require.Contains(t, buf.String(), tc.want)
		})
	}
}

// captureLogs swaps the global logger for a buffer at trace level until the
// test ends. Global state: callers must not run in parallel.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevLogger, prevLevel := *logx.As(), zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	logx.SetLogger(zerolog.New(&buf))
	t.Cleanup(func() {
		logx.SetLogger(prevLogger)
		zerolog.SetGlobalLevel(prevLevel)
	})
	return &buf
}
