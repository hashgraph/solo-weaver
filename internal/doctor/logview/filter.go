// SPDX-License-Identifier: Apache-2.0

package logview

import (
	"regexp"
	"strings"
	"time"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"github.com/rs/zerolog"
)

// rankOf parses a log level with zerolog. Only trace through panic are real
// levels; zerolog also parses "disabled" and "" into values above panic.
func rankOf(level string) (zerolog.Level, bool) {
	l, err := zerolog.ParseLevel(level)
	if err != nil || l < zerolog.TraceLevel || l > zerolog.PanicLevel {
		return 0, false
	}
	return l, true
}

// FilterOptions are the raw, unparsed filter inputs from the command line.
type FilterOptions struct {
	Level   string
	Since   string
	Grep    string
	Message string
	Fields  []string
}

// Filter decides which entries are shown. All set criteria must match.
type Filter struct {
	minLevel zerolog.Level
	hasLevel bool
	since    time.Time
	grep     *regexp.Regexp
	message  *regexp.Regexp
	fields   [][2]string
}

// NewFilter validates opts and compiles them. now is the reference for a
// relative --since such as "10m".
func NewFilter(opts FilterOptions, now time.Time) (Filter, error) {
	var f Filter

	if opts.Level != "" {
		rank, ok := rankOf(opts.Level)
		if !ok {
			return f, invalid(errorx.IllegalArgument.New("unknown log level %q", opts.Level),
				"Use one of: trace, debug, info, warn, error, fatal, panic.")
		}
		f.minLevel, f.hasLevel = rank, true
	}

	if opts.Since != "" {
		since, err := parseSince(opts.Since, now)
		if err != nil {
			return f, err
		}
		f.since = since
	}

	var err error
	if f.grep, err = compile("--grep", opts.Grep); err != nil {
		return f, err
	}
	if f.message, err = compile("--message", opts.Message); err != nil {
		return f, err
	}

	for _, kv := range opts.Fields {
		key, value, ok := strings.Cut(kv, "=")
		if !ok || key == "" {
			return f, invalid(errorx.IllegalArgument.New("invalid --field %q", kv),
				"Use the form key=value, for example --field reason=UpgradeMonitorStarted.")
		}
		f.fields = append(f.fields, [2]string{key, value})
	}

	return f, nil
}

// active reports whether any criterion is set.
func (f Filter) active() bool {
	return f.hasLevel || !f.since.IsZero() || f.grep != nil || f.message != nil || len(f.fields) > 0
}

// Match reports whether e passes every criterion. When any criterion is set, a
// line that is not valid JSON never matches.
func (f Filter) Match(e Entry) bool {
	if !f.active() {
		return true
	}
	if !e.Valid {
		return false
	}

	if f.hasLevel {
		if rank, ok := rankOf(e.Level); !ok || rank < f.minLevel {
			return false
		}
	}
	if !f.since.IsZero() && (e.Time.IsZero() || e.Time.Before(f.since)) {
		return false
	}
	if f.message != nil && !f.message.MatchString(e.Message) {
		return false
	}
	if f.grep != nil && !f.grep.MatchString(plainText(e)) {
		return false
	}
	for _, kv := range f.fields {
		v, ok := e.lookup(kv[0])
		if !ok || asString(v) != kv[1] {
			return false
		}
	}
	return true
}

func compile(flag, pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, invalid(errorx.IllegalArgument.Wrap(err, "invalid %s pattern %q", flag, pattern),
			"Use a valid Go regular expression (RE2 syntax).")
	}
	return re, nil
}

// parseSince accepts a duration back from now ("10m", "2h") or an RFC3339 time.
func parseSince(v string, now time.Time) (time.Time, error) {
	if d, err := time.ParseDuration(v); err == nil {
		if d < 0 {
			return time.Time{}, invalid(errorx.IllegalArgument.New("--since must not be negative: %q", v),
				"Use a positive duration such as 10m or 2h.")
		}
		return now.Add(-d), nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	return time.Time{}, invalid(errorx.IllegalArgument.New("invalid --since value %q", v),
		"Use a duration such as 10m or 2h, or an RFC3339 time such as 2026-10-17T00:00:00Z.")
}

func invalid(err error, hint string) error {
	return errx.Decorate(err, reasons.InvalidArgument, hint)
}
