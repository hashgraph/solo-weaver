// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/pkg/reasons"
)

func runWaitClusterReadable(t *testing.T, read func(context.Context) error, timeout, interval time.Duration) *automa.Report {
	t.Helper()
	wf, err := automa.NewWorkflowBuilder().WithId("wait").
		Steps(WaitClusterReadable(read, timeout, interval)).Build()
	require.NoError(t, err)
	return wf.Execute(context.Background())
}

func TestWaitClusterReadable_ReadableAtOnce(t *testing.T) {
	calls := 0
	report := runWaitClusterReadable(t, func(context.Context) error { calls++; return nil }, time.Second, time.Millisecond)

	require.True(t, report.IsSuccess())
	require.Equal(t, 1, calls)
}

func TestWaitClusterReadable_ReadableAfterRetries(t *testing.T) {
	calls := 0
	report := runWaitClusterReadable(t, func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("node info incomplete")
		}
		return nil
	}, time.Second, time.Millisecond)

	require.True(t, report.IsSuccess())
	require.Equal(t, 3, calls)
}

func TestWaitClusterReadable_TimeoutFailsWithTheLastCause(t *testing.T) {
	report := runWaitClusterReadable(t, func(context.Context) error {
		return errors.New("the API server did not answer")
	}, 20*time.Millisecond, time.Millisecond)

	require.False(t, report.IsSuccess())
	err := FailureOf(report)
	require.Error(t, err)
	require.Contains(t, err.Error(), "the API server did not answer")
	require.Contains(t, err.Error(), "could not be fully read within 20ms")
	reason, _ := errx.ReasonOf(err)
	require.Equal(t, reasons.PreconditionNotMet, reason)
	hints, _ := errx.Hints(err)
	require.NotEmpty(t, hints)
}

func TestWaitClusterReadable_RejectsBadArguments(t *testing.T) {
	ok := func(context.Context) error { return nil }
	for _, tc := range []struct {
		name              string
		read              func(context.Context) error
		timeout, interval time.Duration
	}{
		{"no read function", nil, time.Second, time.Millisecond},
		{"zero interval", ok, time.Second, 0},
		{"negative interval", ok, time.Second, -time.Millisecond},
		{"interval longer than the timeout", ok, time.Millisecond, time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := runWaitClusterReadable(t, tc.read, tc.timeout, tc.interval)
			require.False(t, report.IsSuccess())
			reason, _ := errx.ReasonOf(FailureOf(report))
			require.Equal(t, reasons.Internal, reason)
		})
	}
}
