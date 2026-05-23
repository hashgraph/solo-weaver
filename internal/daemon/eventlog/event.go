// SPDX-License-Identifier: Apache-2.0

package eventlog

import "time"

// Level is the severity of an event — mirrors HIP-defined values.
type Level string

const (
	LevelInfo  Level = "INFO"
	LevelError Level = "ERROR"
)

// Event is a single lifecycle milestone written to a JSONL file.
// All fields are required; zero values produce invalid entries.
type Event struct {
	Ts          time.Time `json:"ts"`
	Level       Level     `json:"level"`
	Reason      string    `json:"reason"`
	Msg         string    `json:"msg"`
	OperationID string    `json:"operationId"`
	NodeID      string    `json:"nodeId"`
}
