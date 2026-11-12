// SPDX-License-Identifier: Apache-2.0

package featuregate

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	gatekit "github.com/automa-saga/gatekit"
)

// updateBaseline regenerates the committed baseline snapshot instead of checking against it.
// Run it when cutting a release, so the baseline is the registry as of the last release:
//
//	go test ./internal/featuregate -run TestRegistryBaselineLint -update-baseline
var updateBaseline = flag.Bool("update-baseline", false,
	"regenerate testdata/baseline_registry.json from the current catalog (run at release)")

const baselineFile = "testdata/baseline_registry.json"

// TestRegistryRemovalLint fails the build when the catalog carries a stale spec, a feature
// past its RemoveBy, or a Deprecated feature due for removal (gatekit removal lint,
// core-spec §8). It uses the same support window W the gate enforces (SupportWindow), so the
// lint checks exactly the window the binary runs with. When it fires, delete the named spec
// and the dead code path it guarded.
func TestRegistryRemovalLint(t *testing.T) {
	violations, err := gatekit.Lint(Catalog(), SupportWindow)
	if err != nil {
		t.Fatalf("Lint: the catalog is invalid: %v", err)
	}
	for _, v := range violations {
		if v.Kind == gatekit.StaleSpec {
			t.Errorf("%s: %s (spec %d) — delete the stale spec and its dead path", v.Feature, v.Kind, v.ActiveFrom)
		} else {
			t.Errorf("%s: %s", v.Feature, v.Kind)
		}
	}
}

// TestRegistryBaselineLint fails the build when the catalog jumps the activation version by
// more than one release, or rewrites a spec at or below the last released ceiling (gatekit
// baseline lint, activation-spec §10) — both break or split a running fleet.
//
// The baseline is a committed snapshot of the catalog as of the last release. Regenerate it
// at release time with -update-baseline (see updateBaseline); a normal feature PR must NOT
// regenerate it, or the lint would compare the catalog against itself and catch nothing.
func TestRegistryBaselineLint(t *testing.T) {
	if *updateBaseline {
		data, err := gatekit.MarshalRegistry(Catalog())
		if err != nil {
			t.Fatalf("MarshalRegistry: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(baselineFile), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(baselineFile, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("regenerated %s from the current catalog", baselineFile)
		return
	}

	data, err := os.ReadFile(baselineFile)
	if err != nil {
		t.Fatalf("read baseline (regenerate with -update-baseline): %v", err)
	}
	baseline, err := gatekit.UnmarshalRegistry(data)
	if err != nil {
		t.Fatalf("parse %s: %v", baselineFile, err)
	}

	violations, err := gatekit.Baseline(Catalog(), baseline)
	if err != nil {
		t.Fatalf("Baseline: %v", err)
	}
	for _, v := range violations {
		switch v.Kind {
		case gatekit.VersionJump:
			t.Errorf("baseline: version-jump — the activation version rose by more than one release")
		default:
			t.Errorf("baseline: %s %s (activeFrom %d) — a released spec was added or changed", v.Feature, v.Kind, v.ActiveFrom)
		}
	}
}

// TestDevGateWindow WARNS (never fails) when an Alpha "development" gate has drifted below
// the support floor: its spec sits at an activation version older than min, so it has
// outlived its N/N+1 cycle without being graduated (to Beta/GA at a later version) or
// deleted. This is deliberately a warning, not a failure — a feature can legitimately slip
// when more urgent work ships first — but it should not go unnoticed.
func TestDevGateWindow(t *testing.T) {
	gate, err := NewDefault()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	min := gate.EffectiveVersion().Min
	for f, def := range Catalog() {
		for _, s := range def.Specs {
			if s.Maturity == gatekit.Alpha && s.ActiveFrom < min {
				t.Logf("WARNING: dev gate %q has an Alpha spec at activation %d, below the support floor %d — graduate or delete it",
					f, s.ActiveFrom, min)
			}
		}
	}
}
