package notify

import (
	"context"
	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
)

// Default notification handler that logs to standard output
// Caller may override using SetDefault
var handler = &Handler{
	StepCompletion: func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{}) {
		logx.As().Info().
			Str("step_id", stp.Id()).
			Str("status", report.Status.String()).
			Msgf(msg, args...)
	},
	StepFailure: func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{}) {
		logx.As().Error().Err(report.Error).
			Str("step_id", stp.Id()).
			Str("status", report.Status.String()).
			Msgf(msg, args...)
	},
}

// Handler defines callbacks for step events
// Caller may pass a custom handler to pass message to a channel or different logging mechanism or webhook (e.g. Slack).
type Handler struct {
	StepCompletion func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{})
	StepFailure    func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{})
}

// SetDefault sets the default callback handler for step events
// It only updates non-nil handlers to preserve existing defaults
// Caller may pass a custom handler to pass message to a channel or different logging mechanism etc.
func SetDefault(h *Handler) {
	if handler.StepCompletion != nil {
		handler.StepCompletion = h.StepCompletion
	}
	if handler.StepFailure != nil {
		handler.StepFailure = h.StepFailure
	}
}

// As returns the current notification handler
func As() *Handler {
	return handler
}
