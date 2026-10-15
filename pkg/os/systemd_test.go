// SPDX-License-Identifier: Apache-2.0

package os

import (
	"context"
	"testing"
	"time"
)

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

func Test_Systemd_WithJobTimeout(t *testing.T) {
	// No caller deadline: the helper must impose one.
	ctx, cancel := withJobTimeout(context.Background())
	defer cancel()
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("withJobTimeout returned a context without a deadline")
	}

	// Caller deadline: it must be honoured unchanged.
	want := time.Now().Add(time.Hour)
	parent, parentCancel := context.WithDeadline(context.Background(), want)
	defer parentCancel()
	got, gotCancel := withJobTimeout(parent)
	defer gotCancel()
	deadline, ok := got.Deadline()
	if !ok {
		t.Fatal("withJobTimeout dropped the caller's deadline")
	}
	if !deadline.Equal(want) {
		t.Errorf("withJobTimeout deadline = %v, want %v", deadline, want)
	}
}
