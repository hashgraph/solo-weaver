// SPDX-License-Identifier: Apache-2.0

package os

import "testing"

func Test_Systemd_EnsureServiceSuffix(t *testing.T) {
	cases := map[string]string{
		"foo":         "foo.service",
		"foo.service": "foo.service",
		"foo.timer":   "foo.timer",
		"foo.socket":  "foo.socket",
		"solo-provisioner-network-dns-refresh.timer": "solo-provisioner-network-dns-refresh.timer",
	}
	for in, want := range cases {
		if got := ensureServiceSuffix(in); got != want {
			t.Errorf("ensureServiceSuffix(%q) = %q, want %q", in, got, want)
		}
	}
}
