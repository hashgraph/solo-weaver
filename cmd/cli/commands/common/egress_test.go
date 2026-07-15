// SPDX-License-Identifier: Apache-2.0

package common

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestValidateEgressFlags_LinkRate(t *testing.T) {
	cases := []struct {
		val     string
		wantErr bool
	}{
		{"auto", false},
		{"AUTO", false},
		{"1gbit", false},
		{"100mbit", false},
		{"", false}, // set-but-empty is skipped
		{"fast", true},
		{"1gig", true},
	}
	for _, c := range cases {
		var egressIface, linkRate string
		cmd := &cobra.Command{Use: "x", RunE: func(*cobra.Command, []string) error { return nil }}
		RegisterEgressFlags(cmd, &egressIface, &linkRate)
		if err := cmd.Flags().Set(FlagNameLinkRate, c.val); err != nil {
			t.Fatalf("set %s=%q: %v", FlagNameLinkRate, c.val, err)
		}
		err := ValidateEgressFlags(cmd, linkRate)
		if (err != nil) != c.wantErr {
			t.Errorf("ValidateEgressFlags(--%s %q) err=%v, wantErr=%v", FlagNameLinkRate, c.val, err, c.wantErr)
		}
	}
}
