// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package prompt

import (
	"testing"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/ui"
	"github.com/spf13/cobra"
)

func TestShouldPrompt_ForceSkips(t *testing.T) {
	// ShouldPrompt must return false when force is true.
	if ShouldPrompt(true) {
		t.Fatal("expected ShouldPrompt(true) to return false")
	}
}

func TestShouldPrompt_NonInteractiveSkips(t *testing.T) {
	// When NonInteractive is set, prompts are suppressed.
	original := ui.NonInteractive
	ui.NonInteractive = true
	defer func() { ui.NonInteractive = original }()

	if ShouldPrompt(false) {
		t.Fatal("expected ShouldPrompt to return false when NonInteractive is set")
	}
}

func TestFlagWasSet_DetectsChanged(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	var target string
	cmd.Flags().StringVar(&target, "namespace", "", "test flag")

	// Before setting: should be false.
	if flagWasSet(cmd, "namespace") {
		t.Fatal("expected flagWasSet to return false for unset flag")
	}

	// After setting via the flag set: should be true.
	_ = cmd.Flags().Set("namespace", "my-ns")
	if !flagWasSet(cmd, "namespace") {
		t.Fatal("expected flagWasSet to return true after flag was set")
	}
}

func TestFlagWasSet_InheritedPersistent(t *testing.T) {
	parent := &cobra.Command{Use: "parent"}
	child := &cobra.Command{Use: "child", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
	parent.AddCommand(child)

	var profile string
	parent.PersistentFlags().StringVar(&profile, "profile", "", "deployment profile")

	// The child should see the inherited flag as not set initially.
	// We need to trigger flag merging by executing parse.
	_ = child.ParseFlags([]string{})
	if flagWasSet(child, "profile") {
		t.Fatal("expected flagWasSet to return false for unset inherited flag")
	}

	// Set it on the parent's persistent flags.
	_ = parent.PersistentFlags().Set("profile", "mainnet")
	if !flagWasSet(child, "profile") {
		t.Fatal("expected flagWasSet to return true for set inherited persistent flag")
	}
}

func TestFlagWasSet_UnknownFlag(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	if flagWasSet(cmd, "nonexistent") {
		t.Fatal("expected flagWasSet to return false for unknown flag")
	}
}

func TestRunSelectPrompts_SkipsAlreadySetFlags(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	var target string
	cmd.Flags().StringVar(&target, "profile", "", "deployment profile")
	_ = cmd.Flags().Set("profile", "mainnet")

	prompts := []SelectPrompt{
		{
			FlagName:       "profile",
			Title:          "Profile",
			Options:        []string{"local", "mainnet"},
			EffectiveValue: "local",
			Target:         &target,
		},
	}

	// Should return nil immediately because the flag is already set.
	err := RunSelectPrompts(cmd, prompts, NewChosenValues())
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	// Target should NOT have been overwritten by the effective value.
	if target != "mainnet" {
		t.Fatalf("expected target to remain 'mainnet', got: %q", target)
	}
}

func TestRunInputPrompts_SkipsAlreadySetFlags(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	var target string
	cmd.Flags().StringVar(&target, "namespace", "", "k8s namespace")
	_ = cmd.Flags().Set("namespace", "custom-ns")

	prompts := []InputPrompt{
		{
			FlagName:       "namespace",
			Title:          "Namespace",
			EffectiveValue: "block-node",
			Target:         &target,
		},
	}

	err := RunInputPrompts(cmd, prompts, NewChosenValues())
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	if target != "custom-ns" {
		t.Fatalf("expected target to remain 'custom-ns', got: %q", target)
	}
}

func TestRunSelectPrompts_EmptyPromptList(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	err := RunSelectPrompts(cmd, nil, NewChosenValues())
	if err != nil {
		t.Fatalf("expected nil error for empty prompt list, got: %v", err)
	}
}

func TestRunInputPrompts_EmptyPromptList(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	err := RunInputPrompts(cmd, nil, NewChosenValues())
	if err != nil {
		t.Fatalf("expected nil error for empty prompt list, got: %v", err)
	}
}

func TestBlockNodeSelectPrompts_ReturnsProfilePrompt(t *testing.T) {
	var target string
	prompts := BlockNodeSelectPrompts(state.PromptDefaults{}, &target)

	if len(prompts) != 1 {
		t.Fatalf("expected 1 select prompt, got %d", len(prompts))
	}

	p := prompts[0]
	if p.FlagName != "profile" {
		t.Fatalf("expected FlagName='profile', got %q", p.FlagName)
	}
	if len(p.Options) < 2 {
		t.Fatalf("expected at least 2 profile options, got %d", len(p.Options))
	}
	if p.EffectiveValue != "" {
		t.Fatalf("expected empty EffectiveValue for profile prompt when no state/config, got %q", p.EffectiveValue)
	}
}

func TestBlockNodeInputPrompts_ReturnsExpectedPrompts(t *testing.T) {
	var ns, rel, ver, histRet, recRet string
	prompts := BlockNodeInputPrompts(state.PromptDefaults{}, &ns, &rel, &ver, &histRet, &recRet)

	if len(prompts) != 5 {
		t.Fatalf("expected 5 input prompts, got %d", len(prompts))
	}

	names := map[string]bool{}
	for _, p := range prompts {
		names[p.FlagName] = true
	}
	for _, expected := range []string{"namespace", "release-name", "chart-version", "historic-retention", "recent-retention"} {
		if !names[expected] {
			t.Fatalf("expected prompt for flag %q, not found", expected)
		}
	}
}

func TestValidateRetentionThreshold(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"0", false},
		{"96000", false},
		{"500000", false},
		{"1", false},
		{"", true},
		{"-1", true},
		{"abc", true},
		{"96.5", true},
		{"12 34", true},
	}
	for _, tt := range tests {
		err := validateRetentionThreshold(tt.input)
		if tt.wantErr && err == nil {
			t.Errorf("validateRetentionThreshold(%q) expected error, got nil", tt.input)
		}
		if !tt.wantErr && err != nil {
			t.Errorf("validateRetentionThreshold(%q) unexpected error: %v", tt.input, err)
		}
	}
}

func TestWrapFormError_Nil(t *testing.T) {
	if err := wrapFormError(nil); err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
}

func TestResolveEffective(t *testing.T) {
	tests := []struct {
		name         string
		stateValue   string
		configValue  string
		defaultValue string
		want         string
	}{
		{
			name:         "state wins over config and default",
			stateValue:   "from-state",
			configValue:  "from-config",
			defaultValue: "from-default",
			want:         "from-state",
		},
		{
			name:         "config wins when state is empty",
			stateValue:   "",
			configValue:  "from-config",
			defaultValue: "from-default",
			want:         "from-config",
		},
		{
			name:         "default wins when state and config are empty",
			stateValue:   "",
			configValue:  "",
			defaultValue: "from-default",
			want:         "from-default",
		},
		{
			name:         "empty when all sources are empty",
			stateValue:   "",
			configValue:  "",
			defaultValue: "",
			want:         "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveEffective(tt.stateValue, tt.configValue, tt.defaultValue)
			if got != tt.want {
				t.Errorf("resolveEffective(%q, %q, %q) = %q, want %q",
					tt.stateValue, tt.configValue, tt.defaultValue, got, tt.want)
			}
		})
	}
}

// TestRunInputPrompts_SkipsUnregisteredFlag verifies that RunInputPrompts does not
// add a field to the form when the flag is not registered on the command.
// This is the guard that prevents "chart-version" from appearing in the
// interactive prompt when running "block node reconfigure".
func TestRunInputPrompts_SkipsUnregisteredFlag(t *testing.T) {
	// cmd has no flags registered.
	cmd := &cobra.Command{Use: "reconfigure"}

	var target string
	prompts := []InputPrompt{
		{
			FlagName:       "chart-version",
			Title:          "Chart Version",
			EffectiveValue: "0.30.2",
			Target:         &target,
		},
	}

	cv := NewChosenValues()
	// RunInputPrompts must return nil without touching target or cv,
	// because "chart-version" is not registered on cmd.
	if err := RunInputPrompts(cmd, prompts, cv); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cv.pairs) != 0 {
		t.Fatalf("expected 0 chosen values, got %d — chart-version should have been skipped", len(cv.pairs))
	}
	// target must not have been pre-filled with the effective value.
	if target != "" {
		t.Fatalf("expected target to be empty, got %q — effective value must not be applied for skipped flags", target)
	}
}
