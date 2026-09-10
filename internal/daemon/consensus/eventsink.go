// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"time"

	"github.com/automa-saga/daemonkit/eventlog"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/rs/zerolog"
)

// eventSink is the helper every execute step calls to record a milestone on both
// channels HIP-1496 requires: the per-operation JSONL audit log and a paired logx
// line carrying the same reason. The daemon emits no Kubernetes events.
//
// JSONL is best-effort: a nil logger (events dir unset, or open failed) degrades
// the sink to logx-only, and a write failure is logged once and dropped so a bad
// disk never blocks the upgrade. All methods are nil-safe. One sink wraps one
// EventLogger for the lifetime of a single operation.
type eventSink struct {
	logger      *eventlog.EventLogger // nil ⇒ logx-only
	nodeID      string
	operationID string
}

func newEventSink(logger *eventlog.EventLogger, nodeID, operationID string) *eventSink {
	return &eventSink{logger: logger, nodeID: nodeID, operationID: operationID}
}

func (s *eventSink) info(reason errx.Reason, msg string)  { s.emit(eventlog.LevelInfo, reason, msg) }
func (s *eventSink) warn(reason errx.Reason, msg string)  { s.emit(eventlog.LevelWarn, reason, msg) }
func (s *eventSink) error(reason errx.Reason, msg string) { s.emit(eventlog.LevelError, reason, msg) }

func (s *eventSink) emit(level eventlog.Level, reason errx.Reason, msg string) {
	if s == nil {
		return
	}
	s.logLine(level, reason, msg)
	s.writeJSONL(level, reason, msg)
}

func (s *eventSink) logLine(level eventlog.Level, reason errx.Reason, msg string) {
	var e *zerolog.Event
	switch level {
	case eventlog.LevelError:
		e = logx.As().Error()
	case eventlog.LevelWarn:
		e = logx.As().Warn()
	default:
		e = logx.As().Info()
	}
	e.Str("reason", reason.String()).
		Str("operation_id", s.operationID).
		Str("node_id", s.nodeID).
		Msg(msg)
}

func (s *eventSink) writeJSONL(level eventlog.Level, reason errx.Reason, msg string) {
	if s.logger == nil {
		return
	}
	if err := s.logger.Log(eventlog.Event{
		Ts:          time.Now().UTC(),
		Level:       level,
		Reason:      reason.String(),
		Msg:         msg,
		OperationID: s.operationID,
		NodeID:      s.nodeID,
	}); err != nil {
		logx.As().Warn().Err(err).
			Str("reason", reason.String()).
			Str("operation_id", s.operationID).
			Msg("could not append execute event to JSONL — continuing")
	}
}

func (s *eventSink) close() {
	if s == nil || s.logger == nil {
		return
	}
	if err := s.logger.Close(); err != nil {
		logx.As().Warn().Err(err).
			Str("operation_id", s.operationID).
			Msg("error closing execute event log")
	}
}
