// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/automa-saga/logx"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/hashgraph/solo-weaver/pkg/version"
	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	// detailThrottleInterval is the minimum time between consecutive
	// detail sends. This prevents rapid-fire log messages from
	// overwhelming the TUI render loop or producing flicker.
	detailThrottleInterval = 80 * time.Millisecond

	// detailMaxLen is the maximum character length for a detail message
	// displayed in the TUI. Longer messages are truncated with an ellipsis.
	detailMaxLen = 200
)

// logHook is a zerolog.Hook that forwards log messages as detail text.
// Used by the TUI to forward log messages as StepDetailMsg.
// It filters by level, throttles rapid sends, and sanitizes messages.
type logHook struct {
	onDetail func(string)
	mu       sync.Mutex
	lastSend time.Time
}

// Run implements zerolog.Hook. It forwards Info and Warn level messages as
// detail text. Debug messages are included only at VerboseLevel >= 1.
// Error/Fatal/Panic levels are not forwarded — they surface via notify callbacks.
func (h *logHook) Run(_ *zerolog.Event, level zerolog.Level, message string) {
	if message == "" {
		return
	}

	switch level {
	case zerolog.InfoLevel, zerolog.WarnLevel:
		// always forward
	case zerolog.DebugLevel:
		if VerboseLevel < 1 {
			return
		}
	default:
		return
	}

	// Throttle rapid messages (detail is always transient) to prevent flicker.
	h.mu.Lock()
	now := time.Now()
	if now.Sub(h.lastSend) < detailThrottleInterval {
		h.mu.Unlock()
		return
	}
	h.lastSend = now
	h.mu.Unlock()

	detail := sanitizeDetail(message)
	if detail == "" {
		return
	}

	h.onDetail(detail)
}

// newTUILogHook creates a hook that sends log messages to the given tea.Program.
func newTUILogHook(program *tea.Program) *logHook {
	return &logHook{
		onDetail: func(s string) {
			program.Send(StepDetailMsg{Detail: s})
		},
	}
}

// sanitizeDetail cleans and truncates a log message for TUI display.
func sanitizeDetail(msg string) string {
	// Strip newlines and collapse whitespace.
	msg = strings.Join(strings.Fields(msg), " ")
	msg = strings.TrimSpace(msg)

	runes := []rune(msg)
	if len(runes) > detailMaxLen {
		msg = string(runes[:detailMaxLen-1]) + "…"
	}

	return msg
}

// newFileOnlyLogger creates a zerolog.Logger that writes only to a log file
// (no ConsoleWriter). Used by SuppressConsoleLogging.
func newFileOnlyLogger(cfg logx.LoggingConfig) zerolog.Logger {
	fileWriter := &lumberjack.Logger{
		Filename:   filepath.Join(cfg.Directory, cfg.Filename),
		MaxSize:    cfg.MaxSize,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxAge,
		Compress:   cfg.Compress,
	}

	pid := os.Getpid()
	return zerolog.New(fileWriter).With().
		Timestamp().
		Int("pid", pid).
		Str("version", version.Number()).
		Logger()
}

// SuppressConsoleLogging replaces the global logx logger with a file-only
// writer (no ConsoleWriter) so that raw zerolog lines do not appear on stdout.
//
// When program is non-nil a tuiLogHook is attached so that log messages are
// forwarded to the TUI as transient "thinking" detail text. When program is
// nil (called before the program exists) only the console suppression is
// applied.
//
// This works around the upstream logx.Initialize() unconditionally creating a
// ConsoleWriter regardless of the ConsoleLogging config field.
func SuppressConsoleLogging(cfg logx.LoggingConfig, program ...*tea.Program) {
	logger := newFileOnlyLogger(cfg)

	// Attach the TUI hook if a program was provided.
	if len(program) > 0 && program[0] != nil {
		logger = logger.Hook(newTUILogHook(program[0]))
	}

	// Replace the global logx logger atomically.
	logx.SetLogger(logger)
}
