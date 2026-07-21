// SPDX-License-Identifier: Apache-2.0

//go:build !linux

package shape

import (
	"context"

	"github.com/joomcode/errorx"
)

// TCRunner abstracts live kernel tc qdisc/class operations for testability.
type TCRunner interface {
	ClassChange(ctx context.Context, nic, minor, rate, ceil string, prio int) error
	QdiscDelRoot(ctx context.Context, nic string) error
	QdiscAddRoot(ctx context.Context, nic, defaultMinor string) error
	ClassAddRoot(ctx context.Context, nic, rate, ceil string) error
	ClassAdd(ctx context.Context, nic, minor, rate, ceil string, prio int) error
	QdiscAddFqCodel(ctx context.Context, nic, minor, handle string) error
}

type noopTCRunner struct{}

func errUnsupported() error {
	return errorx.IllegalState.New("tc operations not supported on this platform")
}

func (r *noopTCRunner) ClassChange(_ context.Context, _, _, _, _ string, _ int) error {
	return errUnsupported()
}

// QdiscDelRoot is best-effort even on unsupported platforms: teardown must never
// block the caller, matching the Linux runner's swallow-and-continue semantics.
func (r *noopTCRunner) QdiscDelRoot(_ context.Context, _ string) error { return nil }

func (r *noopTCRunner) QdiscAddRoot(_ context.Context, _, _ string) error { return errUnsupported() }

func (r *noopTCRunner) ClassAddRoot(_ context.Context, _, _, _ string) error { return errUnsupported() }

func (r *noopTCRunner) ClassAdd(_ context.Context, _, _, _, _ string, _ int) error {
	return errUnsupported()
}

func (r *noopTCRunner) QdiscAddFqCodel(_ context.Context, _, _, _ string) error {
	return errUnsupported()
}

// newExecTCRunner returns a no-op runner on non-Linux platforms.
func newExecTCRunner() TCRunner {
	return &noopTCRunner{}
}
