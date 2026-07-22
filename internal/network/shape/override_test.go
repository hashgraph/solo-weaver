// SPDX-License-Identifier: Apache-2.0

package shape

import "testing"

func intPtr(n int) *int { return &n }

func TestValidateClassOverride(t *testing.T) {
	tests := []struct {
		name    string
		class   string
		o       ClassOverride
		wantErr bool
	}{
		{name: "valid rate/ceil/prio", class: "publisher", o: ClassOverride{Rate: "800mbit", Ceil: "1gbit", Prio: intPtr(0)}},
		{name: "valid partial (ceil only)", class: "partner", o: ClassOverride{Ceil: "700mbit"}},
		{name: "unknown class", class: "nope", o: ClassOverride{Rate: "100mbit"}, wantErr: true},
		{name: "bad rate", class: "publisher", o: ClassOverride{Rate: "fast"}, wantErr: true},
		{name: "bad ceil", class: "publisher", o: ClassOverride{Ceil: "quick"}, wantErr: true},
		{name: "ceil below rate", class: "publisher", o: ClassOverride{Rate: "1gbit", Ceil: "100mbit"}, wantErr: true},
		{name: "prio too high", class: "publisher", o: ClassOverride{Prio: intPtr(8)}, wantErr: true},
		{name: "prio negative", class: "publisher", o: ClassOverride{Prio: intPtr(-1)}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateClassOverride(tt.class, tt.o)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestApplyClassOverrides(t *testing.T) {
	classes := []*ClassConfig{
		{Name: "partner", Rate: "400mbit", Ceil: "700mbit", Prio: 0},
		{Name: "public", Rate: "300mbit", Ceil: "700mbit", Prio: 5},
	}
	overrides := map[string]ClassOverride{
		"partner": {Ceil: "1gbit", Prio: intPtr(3)}, // rate untouched
		"unknown": {Rate: "10mbit"},                 // no matching class → ignored
	}
	applyClassOverrides(classes, overrides)

	if classes[0].Rate != "400mbit" {
		t.Errorf("partner rate mutated: got %q, want unchanged 400mbit", classes[0].Rate)
	}
	if classes[0].Ceil != "1gbit" {
		t.Errorf("partner ceil not overridden: got %q, want 1gbit", classes[0].Ceil)
	}
	if classes[0].Prio != 3 {
		t.Errorf("partner prio not overridden: got %d, want 3", classes[0].Prio)
	}
	if classes[1].Ceil != "700mbit" || classes[1].Prio != 5 {
		t.Errorf("public class unexpectedly mutated: %+v", classes[1])
	}
}

func TestValidateProvisionedClasses(t *testing.T) {
	t.Run("default egress classes pass", func(t *testing.T) {
		_, classes, err := defaultEgressConfig("1gbit")
		if err != nil {
			t.Fatalf("defaultEgressConfig: %v", err)
		}
		if err := validateProvisionedClasses(classes, "1gbit"); err != nil {
			t.Fatalf("default classes should validate: %v", err)
		}
	})

	t.Run("override pushing sum over trunk fails", func(t *testing.T) {
		_, classes, err := defaultEgressConfig("1gbit")
		if err != nil {
			t.Fatalf("defaultEgressConfig: %v", err)
		}
		// Raise partner to the full trunk; with public + reserve-egress still
		// present the sum now exceeds 1gbit.
		applyClassOverrides(classes, map[string]ClassOverride{"partner": {Rate: "1gbit"}})
		if err := validateProvisionedClasses(classes, "1gbit"); err == nil {
			t.Fatalf("expected sum-exceeds-trunk error, got nil")
		}
	})
}
