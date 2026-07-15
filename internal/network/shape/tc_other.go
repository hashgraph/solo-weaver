// SPDX-License-Identifier: Apache-2.0

//go:build !linux

package shape

import (
	"context"

	"github.com/joomcode/errorx"
)

// TCRunner abstracts live kernel tc class operations for testability.
type TCRunner interface {
	ClassChange(ctx context.Context, nic, minor, rate, ceil string, prio int) error
}

type noopTCRunner struct{}

func (r *noopTCRunner) ClassChange(_ context.Context, _, _, _, _ string, _ int) error {
	return errorx.IllegalState.New("tc operations not supported on this platform")
}

// newExecTCRunner returns a no-op runner on non-Linux platforms.
func newExecTCRunner() TCRunner {
	return &noopTCRunner{}
}
