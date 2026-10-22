// SPDX-License-Identifier: Apache-2.0

// Package logview reads, filters and renders the NDJSON log files written by the
// CLI and the daemon.
package logview

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// Entry is one log line. A line that is not a JSON object is kept with Valid
// false so it can still be shown as-is.
type Entry struct {
	Source  string
	Raw     string
	Valid   bool
	Time    time.Time
	Level   string
	Caller  string
	Message string
	Obj     map[string]any
}

// standardKey reports whether k is one of the fixed parts of a line, under the
// names zerolog writes them with. Every other key is a field.
func standardKey(k string) bool {
	return k == zerolog.TimestampFieldName || k == zerolog.LevelFieldName ||
		k == zerolog.CallerFieldName || k == zerolog.MessageFieldName
}

// ParseLine turns one raw line into an Entry.
//
// The daemon's log file is double-encoded: logx hands its file sink to zerolog as
// an io.Writer, which logs each finished JSON line again as the message of a new
// one, giving {"time":...,"message":"{\"level\":...}"}. When a line looks like
// that, the inner entry is used, so levels, fields and --output json work.
func ParseLine(source, line string) Entry {
	e := parseObject(source, line)
	if inner := strings.TrimSpace(e.Message); e.Valid && e.Level == "" && strings.HasPrefix(inner, "{") {
		if unwrapped := parseObject(source, inner); unwrapped.Valid && unwrapped.Level != "" {
			return unwrapped
		}
	}
	return e
}

func parseObject(source, line string) Entry {
	e := Entry{Source: source, Raw: line}

	dec := json.NewDecoder(strings.NewReader(line))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return e
	}

	e.Valid = true
	e.Obj = obj
	e.Level = strings.ToLower(asString(obj[zerolog.LevelFieldName]))
	e.Caller = asString(obj[zerolog.CallerFieldName])
	e.Message = asString(obj[zerolog.MessageFieldName])
	if ts, ok := obj[zerolog.TimestampFieldName].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			e.Time = t
		}
	}
	return e
}

// fields returns the non-standard fields as sorted key and value pairs.
func (e Entry) fields() [][2]string {
	keys := make([]string, 0, len(e.Obj))
	for k := range e.Obj {
		if !standardKey(k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	out := make([][2]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, [2]string{k, asString(e.Obj[k])})
	}
	return out
}

// lookup finds a value by key. A dotted key walks into nested objects.
func (e Entry) lookup(key string) (any, bool) {
	if v, ok := e.Obj[key]; ok {
		return v, true
	}

	var cur any = e.Obj
	for _, part := range strings.Split(key, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[part]; !ok {
			return nil, false
		}
	}
	return cur, true
}

// asString renders a decoded JSON value the way it appears in a log line.
func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		return strconv.FormatBool(t)
	default:
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(t); err != nil {
			return ""
		}
		return strings.TrimRight(buf.String(), "\n")
	}
}
