// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/internal/daemon/core"
	"github.com/stretchr/testify/assert"
)

// ---- local probe helpers (used only by RunCompositeProbe tests) ----

type fakeLeafProbe struct {
	fn func(ctx context.Context) error
}

func (p *fakeLeafProbe) Probe(ctx context.Context) error { return p.fn(ctx) }

func blockingProbe() core.Probe {
	return &fakeLeafProbe{fn: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
}

func unblockedBy(ch <-chan struct{}) core.Probe {
	return &fakeLeafProbe{fn: func(ctx context.Context) error {
		select {
		case <-ch:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
}

// ---- runCompositeProbe / Daemon-level tests ----

func TestRunCompositeProbe_NoComponents_ReturnsImmediately(t *testing.T) {
	d := &Daemon{}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() { d.runCompositeProbe(ctx); close(done) }()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("runCompositeProbe blocked with no components")
	}
}

func TestRunCompositeProbe_SkipsNilProbe(t *testing.T) {
	d := &Daemon{components: []component{{name: "host-only", probe: nil}}}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() { d.runCompositeProbe(ctx); close(done) }()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("runCompositeProbe blocked on nil-probe component")
	}
}

func TestRunCompositeProbe_BlocksUntilAllPass(t *testing.T) {
	unblock := make(chan struct{})
	d := &Daemon{
		components: []component{{
			name:  "slow",
			probe: core.NewCompositeProbe("slow", unblockedBy(unblock)),
		}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() { d.runCompositeProbe(ctx); close(done) }()

	select {
	case <-done:
		t.Fatal("runCompositeProbe returned before probe passed")
	case <-time.After(50 * time.Millisecond):
	}

	close(unblock)
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("runCompositeProbe did not return after probe passed")
	}
}

func TestRunCompositeProbe_CtxCancelAborts(t *testing.T) {
	d := &Daemon{
		components: []component{{
			name:  "blocking",
			probe: core.NewCompositeProbe("blocking", blockingProbe()),
		}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.runCompositeProbe(ctx); close(done) }()

	cancel()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("runCompositeProbe did not exit after ctx cancel")
	}
}

// TestRunCompositeProbe_SkipsNilProbe_TwoComponents verifies that a nil-probe
// and a real probe are handled correctly in the same run.
func TestRunCompositeProbe_MixedProbes(t *testing.T) {
	unblock := make(chan struct{})
	d := &Daemon{
		components: []component{
			{name: "host-only", probe: nil},
			{name: "k8s", probe: core.NewCompositeProbe("k8s", unblockedBy(unblock))},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() { d.runCompositeProbe(ctx); close(done) }()

	select {
	case <-done:
		t.Fatal("runCompositeProbe returned before real probe passed")
	case <-time.After(30 * time.Millisecond):
	}

	close(unblock)
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("runCompositeProbe did not return after probe passed")
	}
}

// TestRunCompositeProbe_AllNilProbes verifies immediate return when all probes are nil.
func TestRunCompositeProbe_AllNilProbes(t *testing.T) {
	d := &Daemon{
		components: []component{
			{name: "a", probe: nil},
			{name: "b", probe: nil},
		},
	}

	done := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	go func() { d.runCompositeProbe(ctx); close(done) }()

	select {
	case <-done:
		assert.True(t, true) // passes immediately
	case <-time.After(200 * time.Millisecond):
		t.Fatal("runCompositeProbe blocked on all-nil probes")
	}
}
