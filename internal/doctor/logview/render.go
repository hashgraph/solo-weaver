// SPDX-License-Identifier: Apache-2.0

package logview

import (
	"encoding/json"
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
		fmt.Fprintln(r.W, Sanitize(e.Raw))
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
	safe := sanitizedJSON(e.Raw)
	if _, err := console.Write([]byte(safe)); err != nil {
		fmt.Fprintln(r.W, Sanitize(e.Raw))
	}
}

// Sanitize makes s safe to print to a terminal. The log file keeps control
// characters escaped, but decoding a line turns them back into real ones, so an
// escape sequence in a logged value would run in the operator's terminal. Each
// control character other than newline and tab is shown as \xNN instead.
func Sanitize(s string) string {
	if !strings.ContainsFunc(s, isControl) {
		return s
	}
	var b strings.Builder
	for _, c := range s {
		if isControl(c) {
			if c < 0x100 {
				fmt.Fprintf(&b, "\\x%02x", c)
			} else {
				fmt.Fprintf(&b, "\\u%04x", c)
			}
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}

func isControl(c rune) bool {
	switch {
	case c == '\n' || c == '\t':
		return false
	case c < 0x20, c == 0x7f, c >= 0x80 && c <= 0x9f:
		return true
	}
	return false
}

// sanitizedJSON returns the JSON object line with every decoded string, and key,
// passed through Sanitize. The line is returned unchanged when it holds no
// control characters or cannot be decoded.
func sanitizedJSON(raw string) string {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		return raw
	}
	out, err := json.Marshal(sanitizeValue(obj))
	if err != nil {
		return raw
	}
	return string(out)
}

func sanitizeValue(v any) any {
	switch t := v.(type) {
	case string:
		return Sanitize(t)
	case []any:
		for i := range t {
			t[i] = sanitizeValue(t[i])
		}
		return t
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[Sanitize(k)] = sanitizeValue(val)
		}
		return out
	}
	return v
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
