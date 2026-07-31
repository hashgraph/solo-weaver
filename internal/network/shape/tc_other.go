// SPDX-License-Identifier: Apache-2.0

//go:build !linux

package shape

import (
	"context"

	"github.com/joomcode/errorx"
)

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

func (r *noopTCRunner) ClassStats(_ context.Context, _ string) (map[string]ClassStat, error) {
	return nil, errUnsupported()
}

// newExecTCRunner returns a no-op runner on non-Linux platforms.
func newExecTCRunner() TCRunner {
	return &noopTCRunner{}
}
