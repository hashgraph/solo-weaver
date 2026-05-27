// SPDX-License-Identifier: Apache-2.0

package eventlog

import (
	"errors"
	"time"
)

// Level is the severity of a lifecycle milestone — mirrors HIP-defined values.
// Only INFO and ERROR are defined because every event in this file is either a
// milestone that happened (INFO) or one that failed terminally (ERROR). Operational
// states such as retries, backoff, and auth errors belong in journald, not here.
// Do not add WARN or DEBUG — they have no meaning in a sparse milestone audit trail.
type Level string

const (
	LevelInfo  Level = "INFO"
	LevelError Level = "ERROR"
)

// Event is a single lifecycle milestone written to a JSONL file.
// All fields are required; Log rejects an Event with any zero value.
type Event struct {
	Ts          time.Time `json:"ts"`
	Level       Level     `json:"level"`
	Reason      string    `json:"reason"`
	Msg         string    `json:"msg"`
	OperationID string    `json:"operationId"`
	NodeID      string    `json:"nodeId"`
}

func (e Event) validate() error {
	var errs []error
	if e.Ts.IsZero() {
		errs = append(errs, ErrInvalidEvent.New("Ts is required"))
	}
	if e.Level == "" {
		errs = append(errs, ErrInvalidEvent.New("Level is required"))
	}
	if e.Reason == "" {
		errs = append(errs, ErrInvalidEvent.New("Reason is required"))
	}
	if e.Msg == "" {
		errs = append(errs, ErrInvalidEvent.New("Msg is required"))
	}
	if e.OperationID == "" {
		errs = append(errs, ErrInvalidEvent.New("OperationID is required"))
	}
	if e.NodeID == "" {
		errs = append(errs, ErrInvalidEvent.New("NodeID is required"))
	}
	return errors.Join(errs...)
}
