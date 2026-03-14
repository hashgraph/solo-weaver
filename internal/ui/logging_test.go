// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizeDetail_TruncatesLongMessages(t *testing.T) {
	long := strings.Repeat("a", 250)
	result := sanitizeDetail(long)
	assert.LessOrEqual(t, len([]rune(result)), detailMaxLen)
	assert.True(t, strings.HasSuffix(result, "…"))
}

func TestSanitizeDetail_ShortMessageUnchanged(t *testing.T) {
	msg := "Installing Helm chart"
	assert.Equal(t, msg, sanitizeDetail(msg))
}

func TestSanitizeDetail_CollapsesWhitespace(t *testing.T) {
	msg := "Installing\n  Helm\t chart"
	assert.Equal(t, "Installing Helm chart", sanitizeDetail(msg))
}

func TestSanitizeDetail_EmptyString(t *testing.T) {
	assert.Equal(t, "", sanitizeDetail(""))
}

func TestSanitizeDetail_WhitespaceOnly(t *testing.T) {
	assert.Equal(t, "", sanitizeDetail("   \n\t  "))
}

func TestTuiLogHook_FiltersByLevel(t *testing.T) {
	tests := []struct {
		name         string
		level        zerolog.Level
		verboseLevel int
		want         bool // true = should forward
	}{
		{"info forwards", zerolog.InfoLevel, 0, true},
		{"warn forwards", zerolog.WarnLevel, 0, true},
		{"debug skipped at level 0", zerolog.DebugLevel, 0, false},
		{"debug skipped at level 1", zerolog.DebugLevel, 1, false},
		{"debug forwards at level 2", zerolog.DebugLevel, 2, true},
		{"error skipped", zerolog.ErrorLevel, 0, false},
		{"fatal skipped", zerolog.FatalLevel, 0, false},
		{"trace skipped", zerolog.TraceLevel, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origVerbose := VerboseLevel
			VerboseLevel = tt.verboseLevel
			defer func() { VerboseLevel = origVerbose }()

			sent := false
			hook := &testableHook{
				onSend: func(detail string) { sent = true },
			}

			hook.run(tt.level, "test message")
			assert.Equal(t, tt.want, sent)
		})
	}
}

func TestTuiLogHook_ThrottlesRapidMessages(t *testing.T) {
	var messages []string
	hook := &testableHook{
		onSend: func(detail string) { messages = append(messages, detail) },
	}

	// First message should go through
	hook.run(zerolog.InfoLevel, "first")
	require.Len(t, messages, 1)

	// Immediate second message should be throttled
	hook.run(zerolog.InfoLevel, "second")
	assert.Len(t, messages, 1)

	// After waiting past the throttle interval, next message should go through
	hook.lastSend = time.Now().Add(-detailThrottleInterval - time.Millisecond)
	hook.run(zerolog.InfoLevel, "third")
	assert.Len(t, messages, 2)
	assert.Equal(t, "third", messages[1])
}

func TestTuiLogHook_SkipsEmptyMessages(t *testing.T) {
	sent := false
	hook := &testableHook{
		onSend: func(detail string) { sent = true },
	}

	hook.run(zerolog.InfoLevel, "")
	assert.False(t, sent)
}

func TestFallbackLogHook_FiltersByLevel(t *testing.T) {
	tests := []struct {
		name         string
		level        zerolog.Level
		verboseLevel int
		want         bool
	}{
		{"info forwards", zerolog.InfoLevel, 0, true},
		{"warn forwards", zerolog.WarnLevel, 0, true},
		{"debug skipped at level 0", zerolog.DebugLevel, 0, false},
		{"debug forwards at level 2", zerolog.DebugLevel, 2, true},
		{"error skipped", zerolog.ErrorLevel, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origVerbose := VerboseLevel
			VerboseLevel = tt.verboseLevel
			defer func() { VerboseLevel = origVerbose }()

			sent := false
			hook := &logHook{
				onDetail: func(detail string) { sent = true },
			}

			hook.Run(nil, tt.level, "test message")
			assert.Equal(t, tt.want, sent)
		})
	}
}

func TestFallbackLogHook_ThrottlesRapidMessages(t *testing.T) {
	var messages []string
	hook := &logHook{
		onDetail: func(detail string) { messages = append(messages, detail) },
	}

	hook.Run(nil, zerolog.InfoLevel, "first")
	require.Len(t, messages, 1)

	// Immediate second message should be throttled
	hook.Run(nil, zerolog.InfoLevel, "second")
	assert.Len(t, messages, 1)

	// After waiting past the throttle interval, next message should go through
	hook.mu.Lock()
	hook.lastSend = time.Now().Add(-detailThrottleInterval - time.Millisecond)
	hook.mu.Unlock()

	hook.Run(nil, zerolog.InfoLevel, "third")
	assert.Len(t, messages, 2)
	assert.Equal(t, "third", messages[1])
}

func TestFallbackLogHook_SkipsEmptyMessages(t *testing.T) {
	sent := false
	hook := &logHook{
		onDetail: func(detail string) { sent = true },
	}

	hook.Run(nil, zerolog.InfoLevel, "")
	assert.False(t, sent)
}

// testableHook mirrors tuiLogHook behaviour but calls onSend instead of
// program.Send, allowing unit tests without a real tea.Program.
type testableHook struct {
	lastSend time.Time
	onSend   func(detail string)
}

func (h *testableHook) run(level zerolog.Level, message string) {
	if message == "" {
		return
	}

	switch level {
	case zerolog.InfoLevel, zerolog.WarnLevel:
		// forward
	case zerolog.DebugLevel:
		if VerboseLevel < 2 {
			return
		}
	default:
		return
	}

	now := time.Now()
	if now.Sub(h.lastSend) < detailThrottleInterval {
		return
	}
	h.lastSend = now

	detail := sanitizeDetail(message)
	if detail == "" {
		return
	}

	h.onSend(detail)
}
