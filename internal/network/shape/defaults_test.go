// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"strings"
	"testing"
	"time"
)

// installedAt is a fixed timestamp standing in for the moment `block node
// install` first wrote the registry, so tests can assert it is carried over
// rather than reset to the current run's clock.
var installedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// classByName finds a class in a slice, failing the test when absent.
func classByName(t *testing.T, classes []*ClassConfig, name string) *ClassConfig {
	t.Helper()
	for _, c := range classes {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("class %q not present in %d classes", name, len(classes))
	return nil
}

// egressRegistry returns a device + class set standing in for what is on disk
// after an install at trunkRate, with the classes at the egress profile's
// default proportions.
func egressRegistry(t *testing.T, trunkRate string) (*DeviceConfig, []*ClassConfig) {
	t.Helper()
	dev, classes, err := defaultEgressConfig(trunkRate)
	if err != nil {
		t.Fatalf("defaultEgressConfig(%q): %v", trunkRate, err)
	}
	dev.CreatedAt = installedAt
	for _, c := range classes {
		c.CreatedAt = installedAt
	}
	return dev, classes
}

func TestSameBandwidth(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"1gbit", "1gbit", true},
		{"1gbit", "1000mbit", true},
		{"1000mbit", "1gbit", true},
		{"400mbit", "400000kbit", true},
		{"1gbit", "500mbit", false},
		{"400mbit", "350mbit", false},
		// Unparseable values fall back to a verbatim comparison, so a legacy
		// shell expression in a hand-edited device file is not mistaken for a
		// changed trunk on every re-provision.
		{"${SPEED}mbit", "${SPEED}mbit", true},
		{"${SPEED}mbit", "1gbit", false},
		{"", "", true},
	}
	for _, tt := range tests {
		if got := sameBandwidth(tt.a, tt.b); got != tt.want {
			t.Errorf("sameBandwidth(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

// TestMergeExistingConfig_FreshInstall verifies that with nothing in the
// registry the computed defaults are left exactly as built — the install path
// must keep provisioning all three classes at profile proportions.
func TestMergeExistingConfig_FreshInstall(t *testing.T) {
	dev, classes, err := defaultEgressConfig("1gbit")
	if err != nil {
		t.Fatalf("defaultEgressConfig: %v", err)
	}

	mergeExistingConfig(dev, classes, nil, nil)

	if dev.Rate != "1gbit" {
		t.Errorf("device rate = %q, want 1gbit", dev.Rate)
	}
	if got := classByName(t, classes, "partner").Rate; got != "400mbit" {
		t.Errorf("partner rate = %q, want 400mbit (40%% of trunk)", got)
	}
	if classByName(t, classes, "partner").CreatedAt.IsZero() {
		t.Error("partner created_at is zero on a fresh install")
	}
	if dev.DefaultClass != "reserve-egress" {
		t.Errorf("device default class = %q, want the profile reserve-egress", dev.DefaultClass)
	}
}

// TestMergeExistingConfig_UnchangedTrunkPreservesTuning is the #1037 regression
// case: an operator tuned partner down with `network shape set`, then ran a bare
// `block node reconfigure`, which resolves the trunk rate back from persisted
// state and therefore re-provisions at the same rate. The tuned value must
// survive, along with the record's original created_at.
func TestMergeExistingConfig_UnchangedTrunkPreservesTuning(t *testing.T) {
	existingDev, existingClasses := egressRegistry(t, "1gbit")
	// `network shape set --class partner --rate 350mbit --prio 3`
	tuned := classByName(t, existingClasses, "partner")
	tuned.Rate = "350mbit"
	tuned.Ceil = "600mbit"
	tuned.Prio = 3

	dev, classes, err := defaultEgressConfig("1gbit")
	if err != nil {
		t.Fatalf("defaultEgressConfig: %v", err)
	}
	mergeExistingConfig(dev, classes, existingDev, existingClasses)

	partner := classByName(t, classes, "partner")
	if partner.Rate != "350mbit" {
		t.Errorf("partner rate = %q, want the tuned 350mbit", partner.Rate)
	}
	if partner.Ceil != "600mbit" {
		t.Errorf("partner ceil = %q, want the tuned 600mbit", partner.Ceil)
	}
	if partner.Prio != 3 {
		t.Errorf("partner prio = %d, want the tuned 3", partner.Prio)
	}
	if !partner.CreatedAt.Equal(installedAt) {
		t.Errorf("partner created_at = %v, want the original %v (record updated, not recreated)",
			partner.CreatedAt, installedAt)
	}
	if !dev.CreatedAt.Equal(installedAt) {
		t.Errorf("device created_at = %v, want the original %v", dev.CreatedAt, installedAt)
	}
	// Untouched classes keep their recorded values too.
	if got := classByName(t, classes, "public").Rate; got != "300mbit" {
		t.Errorf("public rate = %q, want the recorded 300mbit", got)
	}
}

// TestMergeExistingConfig_EquivalentTrunkSpelling covers a re-provision whose
// resolved trunk rate is the same bandwidth spelled differently (state carries
// "1gbit"; an "auto" resolution yields "1000mbit"). That is not a trunk change,
// and the recorded spelling is kept so the rendered boot script does not churn.
func TestMergeExistingConfig_EquivalentTrunkSpelling(t *testing.T) {
	existingDev, existingClasses := egressRegistry(t, "1gbit")
	classByName(t, existingClasses, "partner").Rate = "350mbit"

	dev, classes, err := defaultEgressConfig("1000mbit")
	if err != nil {
		t.Fatalf("defaultEgressConfig: %v", err)
	}
	mergeExistingConfig(dev, classes, existingDev, existingClasses)

	if dev.Rate != "1gbit" {
		t.Errorf("device rate = %q, want the recorded spelling 1gbit", dev.Rate)
	}
	if got := classByName(t, classes, "partner").Rate; got != "350mbit" {
		t.Errorf("partner rate = %q, want the tuned 350mbit", got)
	}
}

// TestMergeExistingConfig_ChangedTrunkRebalances covers the intentional
// `--link-rate` path: a genuinely different trunk rate resets every class to the
// profile proportions of the new trunk. created_at is still carried over — the
// records are updated, not recreated.
func TestMergeExistingConfig_ChangedTrunkRebalances(t *testing.T) {
	existingDev, existingClasses := egressRegistry(t, "1gbit")
	classByName(t, existingClasses, "partner").Rate = "350mbit"

	dev, classes, err := defaultEgressConfig("500mbit")
	if err != nil {
		t.Fatalf("defaultEgressConfig: %v", err)
	}
	mergeExistingConfig(dev, classes, existingDev, existingClasses)

	if dev.Rate != "500mbit" {
		t.Errorf("device rate = %q, want the new 500mbit", dev.Rate)
	}
	for name, want := range map[string]string{
		"partner":        "200mbit", // 40% of 500
		"public":         "150mbit", // 30% of 500
		"reserve-egress": "150mbit", // 30% of 500
	} {
		if got := classByName(t, classes, name).Rate; got != want {
			t.Errorf("%s rate = %q, want %q after the trunk change", name, got, want)
		}
	}
	if !classByName(t, classes, "partner").CreatedAt.Equal(installedAt) {
		t.Error("partner created_at was reset by a trunk change")
	}
}

// TestMergeExistingConfig_ClassMissingFromRegistry covers a class that exists in
// the profile but not on disk — e.g. one added in a later version. It must keep
// its computed default so a re-provision still materialises it.
func TestMergeExistingConfig_ClassMissingFromRegistry(t *testing.T) {
	existingDev, existingClasses := egressRegistry(t, "1gbit")
	kept := make([]*ClassConfig, 0, 2)
	for _, c := range existingClasses {
		if c.Name != "reserve-egress" {
			kept = append(kept, c)
		}
	}

	dev, classes, err := defaultEgressConfig("1gbit")
	if err != nil {
		t.Fatalf("defaultEgressConfig: %v", err)
	}
	mergeExistingConfig(dev, classes, existingDev, kept)

	reserve := classByName(t, classes, "reserve-egress")
	if reserve.Rate != "300mbit" {
		t.Errorf("reserve-egress rate = %q, want the computed default 300mbit", reserve.Rate)
	}
	if reserve.CreatedAt.IsZero() {
		t.Error("reserve-egress created_at is zero; a newly materialised class needs one")
	}
}

// TestMergeExistingConfig_OverrideWinsOverPreserved mirrors provisionDefaults'
// order (merge, then apply this run's --shape) to confirm an explicit override
// still beats a preserved value, while classes the operator did not name on this
// run keep their tuning.
func TestMergeExistingConfig_OverrideWinsOverPreserved(t *testing.T) {
	existingDev, existingClasses := egressRegistry(t, "1gbit")
	classByName(t, existingClasses, "partner").Rate = "350mbit"
	classByName(t, existingClasses, "public").Rate = "250mbit"

	dev, classes, err := defaultEgressConfig("1gbit")
	if err != nil {
		t.Fatalf("defaultEgressConfig: %v", err)
	}
	mergeExistingConfig(dev, classes, existingDev, existingClasses)
	applyClassOverrides(classes, map[string]ClassOverride{
		"partner": {Rate: "300mbit"},
	})

	if got := classByName(t, classes, "partner").Rate; got != "300mbit" {
		t.Errorf("partner rate = %q, want the --shape value 300mbit", got)
	}
	if got := classByName(t, classes, "public").Rate; got != "250mbit" {
		t.Errorf("public rate = %q, want the preserved 250mbit", got)
	}
	if err := validateProvisionedClasses(classes, dev.Rate); err != nil {
		t.Errorf("merged class set failed validation: %v", err)
	}
}

// TestMergeExistingConfig_BootScriptKeepsTuning closes the loop on #1037's
// boot-persistence half: the script rendered from the merged config must carry
// the tuned rate, so a reboot after a reconfigure does not revert it.
func TestMergeExistingConfig_BootScriptKeepsTuning(t *testing.T) {
	existingDev, existingClasses := egressRegistry(t, "1gbit")
	classByName(t, existingClasses, "partner").Rate = "350mbit"

	dev, classes, err := defaultEgressConfig("1gbit")
	if err != nil {
		t.Fatalf("defaultEgressConfig: %v", err)
	}
	mergeExistingConfig(dev, classes, existingDev, existingClasses)

	rendered, err := renderTcEgressScriptFromConfig("enp0s1", dev, classes)
	if err != nil {
		t.Fatalf("renderTcEgressScriptFromConfig: %v", err)
	}
	// classid 1:40 is partner.
	if !strings.Contains(rendered, `classid 1:40 htb rate "350mbit"`) {
		t.Errorf("boot script does not carry the tuned partner rate:\n%s", rendered)
	}
	if strings.Contains(rendered, `classid 1:40 htb rate "400mbit"`) {
		t.Errorf("boot script reverted partner to the profile default:\n%s", rendered)
	}
}

// TestMergeExistingConfig_IngressPreservesTuning verifies the ingress direction
// gets the same protection. TcIngressRecord has no emptiness heuristic at all —
// it always re-provisions — so before #1037 an ingress class tuned with
// `network shape set` was reset on every reconfigure/upgrade.
func TestMergeExistingConfig_IngressPreservesTuning(t *testing.T) {
	existingDev, existingClasses, err := defaultIngressConfig("1gbit")
	if err != nil {
		t.Fatalf("defaultIngressConfig: %v", err)
	}
	existingDev.CreatedAt = installedAt
	for _, c := range existingClasses {
		c.CreatedAt = installedAt
	}
	classByName(t, existingClasses, "publisher").Rate = "700mbit"

	dev, classes, err := defaultIngressConfig("1gbit")
	if err != nil {
		t.Fatalf("defaultIngressConfig: %v", err)
	}
	mergeExistingConfig(dev, classes, existingDev, existingClasses)

	if got := classByName(t, classes, "publisher").Rate; got != "700mbit" {
		t.Errorf("publisher rate = %q, want the tuned 700mbit", got)
	}
}

// TestMergeExistingConfig_PreservesDefaultClass: an operator pointed unmatched
// egress traffic at a non-standard class, then ran a bare reconfigure. The
// recorded default must survive rather than revert to reserve-egress.
func TestMergeExistingConfig_PreservesDefaultClass(t *testing.T) {
	existingDev, existingClasses := egressRegistry(t, "1gbit")
	existingDev.DefaultClass = "public"

	dev, classes, err := defaultEgressConfig("1gbit")
	if err != nil {
		t.Fatalf("defaultEgressConfig: %v", err)
	}
	mergeExistingConfig(dev, classes, existingDev, existingClasses)

	if dev.DefaultClass != "public" {
		t.Errorf("device default class = %q, want the recorded public", dev.DefaultClass)
	}
}

// TestMergeExistingConfig_ChangedTrunkKeepsDefaultClass: a trunk-rate change
// rebalances class bandwidth, it does not un-choose the default class — the two
// are orthogonal operator decisions.
func TestMergeExistingConfig_ChangedTrunkKeepsDefaultClass(t *testing.T) {
	existingDev, existingClasses := egressRegistry(t, "1gbit")
	existingDev.DefaultClass = "public"

	dev, classes, err := defaultEgressConfig("500mbit")
	if err != nil {
		t.Fatalf("defaultEgressConfig: %v", err)
	}
	mergeExistingConfig(dev, classes, existingDev, existingClasses)

	if dev.DefaultClass != "public" {
		t.Errorf("device default class = %q, want the recorded public despite the trunk change", dev.DefaultClass)
	}
	if got := classByName(t, classes, "partner").Rate; got != "200mbit" {
		t.Errorf("partner rate = %q, want 200mbit (40%% of the new trunk)", got)
	}
}

// TestMergeExistingConfig_InvalidDefaultClassFallsBack: a recorded default the
// provision does not write — the other direction's class, an unknown name, an
// empty legacy field — falls back to the profile value, leaving a boot script
// whose `htb default` has a class behind it.
func TestMergeExistingConfig_InvalidDefaultClassFallsBack(t *testing.T) {
	tests := []struct {
		name     string
		dir      string
		recorded string
	}{
		{"egress: other direction's class", DirEgress, "publisher"},
		{"egress: unknown class", DirEgress, "bogus"},
		{"egress: empty legacy field", DirEgress, ""},
		{"ingress: other direction's class", DirIngress, "reserve-egress"},
		{"ingress: unknown class", DirIngress, "bogus"},
		{"ingress: empty legacy field", DirIngress, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			build, want := defaultEgressConfig, "reserve-egress"
			if tt.dir == DirIngress {
				build, want = defaultIngressConfig, "reserve-ingress"
			}

			existingDev, existingClasses, err := build("1gbit")
			if err != nil {
				t.Fatalf("build existing: %v", err)
			}
			existingDev.DefaultClass = tt.recorded

			dev, classes, err := build("1gbit")
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			mergeExistingConfig(dev, classes, existingDev, existingClasses)

			if dev.DefaultClass != want {
				t.Errorf("device default class = %q, want the profile %s", dev.DefaultClass, want)
			}
			if tt.dir != DirEgress {
				return // ingress renders no boot script; the daemon replays it per pod
			}
			rendered, err := renderTcEgressScriptFromConfig("enp0s1", dev, classes)
			if err != nil {
				t.Fatalf("boot script render failed after the fallback: %v", err)
			}
			if !strings.Contains(rendered, "htb default 60") {
				t.Errorf("boot script does not fall back to the profile default:\n%s", rendered)
			}
			if strings.Contains(rendered, "htb default 10") {
				t.Errorf("boot script kept the dangling ingress minor:\n%s", rendered)
			}
		})
	}
}

// TestMergeExistingConfig_DefaultClassDroppedFromProfile pins the guard on set
// membership rather than validateDefaultClass: a class dropped from the profile
// but left in classInfoMap still validates for the direction, yet nothing
// writes it, so the boot script would carry a minor with nothing behind it.
func TestMergeExistingConfig_DefaultClassDroppedFromProfile(t *testing.T) {
	existingDev, existingClasses := egressRegistry(t, "1gbit")
	existingDev.DefaultClass = "public"

	dev, classes, err := defaultEgressConfig("1gbit")
	if err != nil {
		t.Fatalf("defaultEgressConfig: %v", err)
	}
	// Stand in for a release that drops public from egressProfile: the name is
	// still known and still an egress class, so validateDefaultClass accepts it.
	if err := validateDefaultClass("public", DirEgress); err != nil {
		t.Fatalf("premise broken, public no longer validates for egress: %v", err)
	}
	pruned := make([]*ClassConfig, 0, len(classes))
	for _, c := range classes {
		if c.Name != "public" {
			pruned = append(pruned, c)
		}
	}

	mergeExistingConfig(dev, pruned, existingDev, existingClasses)

	if dev.DefaultClass != "reserve-egress" {
		t.Errorf("device default class = %q, want the profile reserve-egress: public is no longer provisioned", dev.DefaultClass)
	}
	rendered, err := renderTcEgressScriptFromConfig("enp0s1", dev, pruned)
	if err != nil {
		t.Fatalf("renderTcEgressScriptFromConfig: %v", err)
	}
	if strings.Contains(rendered, "htb default 50") {
		t.Errorf("boot script points at a class it does not create:\n%s", rendered)
	}
}

// TestMergeExistingConfig_IngressPreservesDefaultClass verifies ingress gets the
// same protection through the shared provisionDefaults.
func TestMergeExistingConfig_IngressPreservesDefaultClass(t *testing.T) {
	existingDev, existingClasses, err := defaultIngressConfig("1gbit")
	if err != nil {
		t.Fatalf("defaultIngressConfig: %v", err)
	}
	existingDev.DefaultClass = "publisher"

	dev, classes, err := defaultIngressConfig("1gbit")
	if err != nil {
		t.Fatalf("defaultIngressConfig: %v", err)
	}
	mergeExistingConfig(dev, classes, existingDev, existingClasses)

	if dev.DefaultClass != "publisher" {
		t.Errorf("device default class = %q, want the recorded publisher", dev.DefaultClass)
	}
}

// TestMergeExistingConfig_BootScriptKeepsDefaultClass covers the boot-persistent
// half: the rendered script must point at the recorded default's minor.
func TestMergeExistingConfig_BootScriptKeepsDefaultClass(t *testing.T) {
	existingDev, existingClasses := egressRegistry(t, "1gbit")
	existingDev.DefaultClass = "public"

	dev, classes, err := defaultEgressConfig("1gbit")
	if err != nil {
		t.Fatalf("defaultEgressConfig: %v", err)
	}
	mergeExistingConfig(dev, classes, existingDev, existingClasses)

	rendered, err := renderTcEgressScriptFromConfig("enp0s1", dev, classes)
	if err != nil {
		t.Fatalf("renderTcEgressScriptFromConfig: %v", err)
	}
	// minor 50 is public; 60 is the profile default reserve-egress.
	if !strings.Contains(rendered, "htb default 50") {
		t.Errorf("boot script does not direct unmatched traffic at the recorded default:\n%s", rendered)
	}
	if strings.Contains(rendered, "htb default 60") {
		t.Errorf("boot script reverted the default class to the profile value:\n%s", rendered)
	}
}
