package notify

import (
	"context"
	"github.com/automa-saga/automa"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/internal/workflows/steps"
	"testing"
)

func TestNotificationHandler_Callbacks(t *testing.T) {
	var completed, failed bool
	var gotMsg string

	handler := &Handler{
		StepCompletion: func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{}) {
			completed = true
			gotMsg = msg
		},
		StepFailure: func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{}) {
			failed = true
			gotMsg = msg
		},
	}

	SetDefault(handler)

	step := &steps.mockStep{id: "test-step"}
	report := &automa.Report{Status: automa.StatusSuccess}
	handler.StepCompletion(context.Background(), step, report, "done")
	require.True(t, completed)
	require.Equal(t, "done", gotMsg)

	report = &automa.Report{Status: automa.StatusFailed, Error: errorx.IllegalState.New("fail")}
	handler.StepFailure(context.Background(), step, report, "fail")
	require.True(t, failed)
	require.Equal(t, "fail", gotMsg)
}

func TestSetDefaultCallbackHandler_PartialUpdate(t *testing.T) {
	orig := As()
	defer SetDefault(orig)

	called := false
	handler := &Handler{
		StepCompletion: func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{}) {
			called = true
		},
		// StepFailure is nil, should not overwrite existing
	}
	SetDefault(handler)

	step := &steps.mockStep{id: "id"}
	report := &automa.Report{Status: automa.StatusSuccess}
	handler.StepCompletion(context.Background(), step, report, "msg")
	require.True(t, called)
	require.NotNil(t, handler.StepFailure)
}
