// SPDX-License-Identifier: Apache-2.0

package common

import "testing"

func TestParseShapeOverrides(t *testing.T) {
	t.Run("nil for empty", func(t *testing.T) {
		got, err := ParseShapeOverrides(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Fatalf("expected nil map, got %v", got)
		}
	})

	t.Run("parses full and partial specs", func(t *testing.T) {
		got, err := ParseShapeOverrides([]string{
			"publisher=rate=800mbit,ceil=1gbit,prio=0",
			"partner=ceil=700mbit",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		pub := got["publisher"]
		if pub.Rate != "800mbit" || pub.Ceil != "1gbit" || pub.Prio == nil || *pub.Prio != 0 {
			t.Errorf("publisher parsed wrong: %+v", pub)
		}
		part := got["partner"]
		if part.Rate != "" || part.Ceil != "700mbit" || part.Prio != nil {
			t.Errorf("partner parsed wrong: %+v", part)
		}
	})

	t.Run("rejects bad input", func(t *testing.T) {
		bad := [][]string{
			{"publisher"},                                    // no spec
			{"publisher="},                                   // empty spec
			{"=rate=1gbit"},                                  // no class
			{"publisher=rate="},                              // empty value
			{"publisher=bogus=1gbit"},                        // unknown field
			{"publisher=prio=notanumber"},                    // bad prio
			{"nope=rate=1gbit"},                              // unknown class
			{"publisher=rate=1gbit,ceil=100mbit"},            // ceil < rate
			{"publisher=rate=1gbit", "publisher=ceil=1gbit"}, // duplicate class
		}
		for _, in := range bad {
			if _, err := ParseShapeOverrides(in); err == nil {
				t.Errorf("expected error for %v, got nil", in)
			}
		}
	})
}
