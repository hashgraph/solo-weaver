// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package prompt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/blocknode"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/spf13/cobra"
)

func TestWizard_RunNoGroupsIsNoop(t *testing.T) {
	// A wizard with no accumulated pages must not run a form — it returns nil,
	// mirroring the per-stage no-op behaviour when every prompt is skipped.
	if err := NewWizard().Run(); err != nil {
		t.Fatalf("expected nil error for empty wizard, got: %v", err)
	}
}

func TestAddSelectPrompts_AccumulatesGroupAndRecordsValue(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	var target string
	cmd.Flags().StringVar(&target, "profile", "", "deployment profile")

	cv := NewChosenValues()
	w := NewWizard()
	AddSelectPrompts(w, cmd, []SelectPrompt{{
		FlagName:       "profile",
		Title:          "Profile",
		Options:        []string{"local", "mainnet"},
		EffectiveValue: "local",
		Target:         &target,
	}}, cv)

	if len(w.groups) != 1 || len(w.afterRun) != 1 {
		t.Fatalf("expected 1 group and 1 afterRun, got %d groups / %d afterRun", len(w.groups), len(w.afterRun))
	}
	// EffectiveValue must be pre-seeded so pressing Enter accepts it.
	if target != "local" {
		t.Fatalf("expected target pre-filled to 'local', got %q", target)
	}

	// Firing the afterRun callback records the final value into cv.
	w.afterRun[0]()
	if len(cv.pairs) != 1 || cv.pairs[0].title != "Profile" || cv.pairs[0].value != "local" {
		t.Fatalf("expected cv to record Profile=local, got %+v", cv.pairs)
	}
}

func TestAddSelectPrompts_SkipsSetFlag(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	var target string
	cmd.Flags().StringVar(&target, "profile", "", "deployment profile")
	_ = cmd.Flags().Set("profile", "mainnet")

	w := NewWizard()
	AddSelectPrompts(w, cmd, []SelectPrompt{{
		FlagName:       "profile",
		Title:          "Profile",
		Options:        []string{"local", "mainnet"},
		EffectiveValue: "local",
		Target:         &target,
	}}, NewChosenValues())

	if len(w.groups) != 0 || len(w.afterRun) != 0 {
		t.Fatalf("expected nothing accumulated when flag already set, got %d groups / %d afterRun", len(w.groups), len(w.afterRun))
	}
	// The effective value must not overwrite the user's CLI value.
	if target != "mainnet" {
		t.Fatalf("expected target to remain 'mainnet', got %q", target)
	}
}

func TestAddInputPrompts_SkipsUnregisteredFlag(t *testing.T) {
	// A flag not registered on the command must not be added to the wizard.
	cmd := &cobra.Command{Use: "reconfigure"}
	var target string

	w := NewWizard()
	AddInputPrompts(w, cmd, []InputPrompt{{
		FlagName:       "chart-version",
		Title:          "Chart Version",
		EffectiveValue: "0.35.1",
		Target:         &target,
	}}, NewChosenValues())

	if len(w.groups) != 0 {
		t.Fatalf("expected no groups for unregistered flag, got %d", len(w.groups))
	}
	if target != "" {
		t.Fatalf("expected target untouched for skipped flag, got %q", target)
	}
}

func storageTargets(
	basePath, archivePath, livePath, logPath, verificationPath, pluginsPath, applicationStatePath *string,
) StoragePathTargets {
	return StoragePathTargets{
		BasePath:             basePath,
		ArchivePath:          archivePath,
		LivePath:             livePath,
		LogPath:              logPath,
		VerificationPath:     verificationPath,
		PluginsPath:          pluginsPath,
		ApplicationStatePath: applicationStatePath,
	}
}

func TestAddStoragePathPrompts_BaseModeNormalisesTargets(t *testing.T) {
	cmd := &cobra.Command{Use: "install"}

	var basePath, archivePath, livePath, logPath, verificationPath, pluginsPath, applicationStatePath string
	targets := storageTargets(&basePath, &archivePath, &livePath, &logPath, &verificationPath, &pluginsPath, &applicationStatePath)

	// A persisted base path makes single-base-path the default mode.
	var defaults state.PromptDefaults
	defaults.BlockNode.Storage.BasePath = "/data"

	chartVersion := "0.35.1"
	cv := NewChosenValues()
	w := NewWizard()
	AddStoragePathPrompts(w, cmd, defaults, &chartVersion, targets, cv)

	// mode group + base-path group + one individual-paths group (core + optional
	// paths share a single page via hidableField).
	if len(w.groups) != 3 || len(w.afterRun) != 1 {
		t.Fatalf("expected 3 groups and 1 afterRun, got %d groups / %d afterRun", len(w.groups), len(w.afterRun))
	}

	// Firing afterRun with the default (base) mode must clear the individual
	// targets and retain the base path so downstream mode inference is unambiguous.
	w.afterRun[0]()
	if basePath != "/data" {
		t.Fatalf("expected base path retained as /data, got %q", basePath)
	}
	for name, v := range map[string]string{
		"archive": archivePath, "live": livePath, "log": logPath,
		"verification": verificationPath, "plugins": pluginsPath, "application-state": applicationStatePath,
	} {
		if v != "" {
			t.Fatalf("expected %s path cleared in base mode, got %q", name, v)
		}
	}
	if len(cv.pairs) != 2 || cv.pairs[0].value != storagePathModeBasePath {
		t.Fatalf("expected cv to record base mode + base path, got %+v", cv.pairs)
	}
}

func TestAddStoragePathPrompts_IndividualModeClearsBasePath(t *testing.T) {
	cmd := &cobra.Command{Use: "install"}

	var basePath, archivePath, livePath, logPath, verificationPath, pluginsPath, applicationStatePath string
	targets := storageTargets(&basePath, &archivePath, &livePath, &logPath, &verificationPath, &pluginsPath, &applicationStatePath)

	// No persisted base path → individual paths is the default mode.
	var defaults state.PromptDefaults

	chartVersion := "0.35.1"
	cv := NewChosenValues()
	w := NewWizard()
	AddStoragePathPrompts(w, cmd, defaults, &chartVersion, targets, cv)

	if len(w.groups) != 3 || len(w.afterRun) != 1 {
		t.Fatalf("expected 3 groups and 1 afterRun, got %d groups / %d afterRun", len(w.groups), len(w.afterRun))
	}

	// Individual mode must clear the base path and keep the required per-path
	// defaults (archive/live/log), which are pre-filled from the effective base.
	w.afterRun[0]()
	if basePath != "" {
		t.Fatalf("expected base path cleared in individual mode, got %q", basePath)
	}
	if archivePath == "" || livePath == "" || logPath == "" {
		t.Fatalf("expected archive/live/log pre-filled in individual mode, got %q / %q / %q", archivePath, livePath, logPath)
	}
	if len(cv.pairs) == 0 || cv.pairs[0].value != storagePathModeIndividual {
		t.Fatalf("expected first cv pair to be individual mode, got %+v", cv.pairs)
	}
}

// TestAddStoragePathPrompts_IndividualModeTracksChartVersionEdit is the #826
// regression guard: a single wizard built while the chart version is pre-0.37.0
// must reflect a later edit to a >=0.37.0 version, dropping the retired
// verification path and keeping application-state — because the builders read the
// chart-version pointer live rather than snapshotting it at construction.
func TestAddStoragePathPrompts_IndividualModeTracksChartVersionEdit(t *testing.T) {
	cmd := &cobra.Command{Use: "install"}

	var basePath, archivePath, livePath, logPath, verificationPath, pluginsPath, applicationStatePath string
	targets := storageTargets(&basePath, &archivePath, &livePath, &logPath, &verificationPath, &pluginsPath, &applicationStatePath)

	// No persisted base path → individual mode is the default.
	var defaults state.PromptDefaults

	// Build the wizard on a pre-0.37.0 version, then simulate the operator editing
	// the chart-version input to a >=0.37.0 version before the wizard completes.
	chartVersion := "0.35.1"
	cv := NewChosenValues()
	w := NewWizard()
	AddStoragePathPrompts(w, cmd, defaults, &chartVersion, targets, cv)

	chartVersion = "0.37.1"
	w.afterRun[0]()

	// application-state applies to >=0.37.0 and must be retained; verification is
	// retired at 0.37.0 and must be cleared; plugins applies across the range.
	if applicationStatePath == "" {
		t.Fatalf("expected application-state path retained for chart 0.37.1, got empty")
	}
	if verificationPath != "" {
		t.Fatalf("expected verification path cleared for chart 0.37.1, got %q", verificationPath)
	}
	if pluginsPath == "" {
		t.Fatalf("expected plugins path retained for chart 0.37.1, got empty")
	}

	recorded := make(map[string]bool)
	for _, p := range cv.pairs {
		recorded[p.title] = true
	}
	if recorded["Verification Storage Path"] {
		t.Fatalf("did not expect verification recorded for chart 0.37.1, got %+v", cv.pairs)
	}
	if !recorded["Application-State Storage Path"] {
		t.Fatalf("expected application-state recorded for chart 0.37.1, got %+v", cv.pairs)
	}
}

func TestAddPluginPresetPrompts_NonCustomClearsPlugins(t *testing.T) {
	cmd := &cobra.Command{Use: "install"}
	var preset, plugins string

	chartVersion := "0.35.1"
	w := NewWizard()
	AddPluginPresetPrompts(w, cmd, state.PromptDefaults{}, &preset, &plugins, &chartVersion, "", NewChosenValues())

	// preset group + conditional custom group.
	if len(w.groups) != 2 || len(w.afterRun) != 1 {
		t.Fatalf("expected 2 groups and 1 afterRun, got %d groups / %d afterRun", len(w.groups), len(w.afterRun))
	}
	if preset == blocknode.PresetCustom {
		t.Fatalf("expected a non-custom default preset, got %q", preset)
	}

	// A stale --plugins value must be cleared when the preset is not Custom.
	plugins = "should-be-cleared"
	w.afterRun[0]()
	if plugins != "" {
		t.Fatalf("expected plugins cleared for non-custom preset, got %q", plugins)
	}
}

func TestAddPluginPresetPrompts_CustomJoinsSelection(t *testing.T) {
	valid := blocknode.PluginsForVersion("0.35.1")
	if len(valid) == 0 {
		t.Skip("no plugins available for chart version 0.35.1")
	}

	cmd := &cobra.Command{Use: "install"}
	var preset, plugins string

	// Custom preset + a saved plugin list pre-selects the saved plugins.
	var defaults state.PromptDefaults
	defaults.BlockNode.PluginPreset = blocknode.PresetCustom
	defaults.BlockNode.PluginList = valid[0]

	chartVersion := "0.35.1"
	cv := NewChosenValues()
	w := NewWizard()
	AddPluginPresetPrompts(w, cmd, defaults, &preset, &plugins, &chartVersion, "", cv)

	if preset != blocknode.PresetCustom {
		t.Fatalf("expected Custom preset pre-selected, got %q", preset)
	}

	// afterRun joins the pre-selected plugins into --plugins.
	w.afterRun[0]()
	if plugins != valid[0] {
		t.Fatalf("expected plugins=%q from custom selection, got %q", valid[0], plugins)
	}
	if len(cv.pairs) != 2 {
		t.Fatalf("expected cv to record preset + custom plugins, got %+v", cv.pairs)
	}
}

func TestAddPluginPresetPrompts_SmartDefaultsToNoneWhenValuesFileDefinesPlugins(t *testing.T) {
	valuesFile := filepath.Join(t.TempDir(), "values.yaml")
	if err := os.WriteFile(valuesFile, []byte("plugins:\n  names: \"health,verification\"\n"), 0o600); err != nil {
		t.Fatalf("failed to write values file: %v", err)
	}

	cmd := &cobra.Command{Use: "install"}
	var preset, plugins string

	chartVersion := "0.35.1"
	w := NewWizard()
	AddPluginPresetPrompts(w, cmd, state.PromptDefaults{}, &preset, &plugins, &chartVersion, valuesFile, NewChosenValues())

	if preset != blocknode.PresetNone {
		t.Fatalf("expected the values file to smart-default the preset to PresetNone, got %q", preset)
	}
}

func TestAddPluginPresetPrompts_DefaultsToTier1LFHWhenNoValuesFile(t *testing.T) {
	cmd := &cobra.Command{Use: "install"}
	var preset, plugins string

	chartVersion := "0.35.1"
	w := NewWizard()
	AddPluginPresetPrompts(w, cmd, state.PromptDefaults{}, &preset, &plugins, &chartVersion, "", NewChosenValues())

	if preset != blocknode.PresetTier1LFH {
		t.Fatalf("expected the default tier1-lfh preset when no --values file is given, got %q", preset)
	}
}

func TestAddPluginPresetPrompts_SkipsWhenPresetFlagSet(t *testing.T) {
	cmd := &cobra.Command{Use: "install"}
	var preset, plugins string
	cmd.Flags().StringVar(&preset, "plugin-preset", "", "")
	cmd.Flags().StringVar(&plugins, "plugins", "", "")
	_ = cmd.Flags().Set("plugin-preset", "tier1-lfh")

	chartVersion := "0.35.1"
	w := NewWizard()
	AddPluginPresetPrompts(w, cmd, state.PromptDefaults{}, &preset, &plugins, &chartVersion, "", NewChosenValues())

	if len(w.groups) != 0 {
		t.Fatalf("expected no groups when --plugin-preset already set, got %d", len(w.groups))
	}
}
