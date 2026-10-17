// SPDX-License-Identifier: Apache-2.0

package logview

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

const (
	grayOn   = "\033[90m"
	colorOff = "\033[0m"
)

// Format selects how entries are written.
type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
)

// Renderer writes entries to W. Text output goes through zerolog's ConsoleWriter,
// the same writer logx uses for the daemon's console, so a line looks exactly as
// it did when it was logged.
type Renderer struct {
	W          io.Writer
	Format     Format
	Color      bool
	ShowSource bool
}

// Write prints one entry. JSON output is the original line, and a line that is
// not valid JSON is skipped so the stream stays valid NDJSON.
func (r Renderer) Write(e Entry) {
	if r.Format == FormatJSON {
		if e.Valid {
			fmt.Fprintln(r.W, e.Raw)
		}
		return
	}
	if !e.Valid {
		fmt.Fprintln(r.W, e.Raw)
		return
	}

	if r.ShowSource {
		tag := "[" + e.Source + "] "
		if r.Color {
			tag = grayOn + tag + colorOff
		}
		fmt.Fprint(r.W, tag)
	}

	console := zerolog.ConsoleWriter{
		Out:          r.W,
		NoColor:      !r.Color,
		TimeFormat:   time.RFC3339,
		TimeLocation: time.UTC,
	}
	if _, err := console.Write([]byte(e.Raw)); err != nil {
		fmt.Fprintln(r.W, e.Raw)
	}
}

// plainText is the uncolored caller, message and fields, which --grep searches.
func plainText(e Entry) string {
	parts := make([]string, 0, 6)
	if e.Caller != "" {
		parts = append(parts, e.Caller)
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	for _, kv := range e.fields() {
		parts = append(parts, kv[0]+"="+kv[1])
	}
	return strings.Join(parts, " ")
}
