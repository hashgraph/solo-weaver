// SPDX-License-Identifier: Apache-2.0

package logview

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var now = time.Date(2026, 10, 17, 1, 0, 0, 0, time.UTC)

func line(ts, level, msg string, extra string) string {
	if extra != "" {
		extra = "," + extra
	}
	return fmt.Sprintf(`{"time":%q,"level":%q,"caller":"a/b.go:1","message":%q%s}`, ts, level, msg, extra)
}

func writeLog(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.log")
	require.NoError(t, os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644))
	return p
}

func mustFilter(t *testing.T, o FilterOptions) Filter {
	t.Helper()
	f, err := NewFilter(o, now)
	require.NoError(t, err)
	return f
}

func TestParseLine(t *testing.T) {
	e := ParseLine("cli", line("2026-10-17T00:38:42Z", "INFO", "hello", `"pid":7,"obj":{"a":1}`))
	require.True(t, e.Valid)
	require.Equal(t, "info", e.Level)
	require.Equal(t, "hello", e.Message)
	require.False(t, e.Time.IsZero())
	require.Equal(t, [][2]string{{"obj", `{"a":1}`}, {"pid", "7"}}, e.fields())

	bad := ParseLine("cli", "panic: boom")
	require.False(t, bad.Valid)
	require.Equal(t, "panic: boom", bad.Raw)
}

func TestFilter(t *testing.T) {
	info := ParseLine("cli", line("2026-10-17T00:55:00Z", "info", "Watch established", `"reason":"R1","step":{"name":"x"}`))
	errE := ParseLine("cli", line("2026-10-17T00:10:00Z", "error", "Boom happened", `"reason":"R2"`))
	text := ParseLine("cli", "plain text")

	tests := []struct {
		name string
		opts FilterOptions
		e    Entry
		want bool
	}{
		{"no filter passes plain text", FilterOptions{}, text, true},
		{"filter drops plain text", FilterOptions{Level: "debug"}, text, false},
		{"level at or above", FilterOptions{Level: "warn"}, errE, true},
		{"level below", FilterOptions{Level: "warn"}, info, false},
		{"since duration keeps recent", FilterOptions{Since: "10m"}, info, true},
		{"since duration drops old", FilterOptions{Since: "10m"}, errE, false},
		{"since rfc3339", FilterOptions{Since: "2026-10-17T00:30:00Z"}, errE, false},
		{"message regex", FilterOptions{Message: "^Watch"}, info, true},
		{"message regex ignores fields", FilterOptions{Message: "R1"}, info, false},
		{"grep searches fields", FilterOptions{Grep: "reason=R1"}, info, true},
		{"grep searches caller", FilterOptions{Grep: "a/b.go"}, info, true},
		{"field exact", FilterOptions{Fields: []string{"reason=R1"}}, info, true},
		{"field mismatch", FilterOptions{Fields: []string{"reason=R2"}}, info, false},
		{"field nested", FilterOptions{Fields: []string{"step.name=x"}}, info, true},
		{"fields are ANDed", FilterOptions{Fields: []string{"reason=R1", "level=error"}}, info, false},
		{"standard key as field", FilterOptions{Fields: []string{"level=error"}}, errE, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, mustFilter(t, tt.opts).Match(tt.e))
		})
	}
}

func TestNewFilterRejectsBadInput(t *testing.T) {
	for _, o := range []FilterOptions{
		{Level: "loud"}, {Since: "yesterday"}, {Since: "-5m"}, {Grep: "("}, {Message: "["}, {Fields: []string{"novalue"}}, {Fields: []string{"=v"}},
	} {
		_, err := NewFilter(o, now)
		require.Error(t, err, "%+v", o)
	}
}

func TestTailHeadAndMissingFile(t *testing.T) {
	p := writeLog(t,
		line("2026-10-17T00:00:01Z", "info", "one", ""),
		line("2026-10-17T00:00:02Z", "debug", "two", ""),
		line("2026-10-17T00:00:03Z", "info", "three", ""),
		line("2026-10-17T00:00:04Z", "info", "four", ""),
	)

	tail, off, err := Tail(p, "cli", Filter{}, 2)
	require.NoError(t, err)
	require.Equal(t, []string{"three", "four"}, messages(tail))
	st, _ := os.Stat(p)
	require.Equal(t, st.Size(), off)

	head, err := Head(p, "cli", Filter{}, 2)
	require.NoError(t, err)
	require.Equal(t, []string{"one", "two"}, messages(head))

	filtered, _, err := Tail(p, "cli", mustFilter(t, FilterOptions{Level: "info"}), 10)
	require.NoError(t, err)
	require.Equal(t, []string{"one", "three", "four"}, messages(filtered))

	none, off, err := Tail(filepath.Join(t.TempDir(), "missing.log"), "cli", Filter{}, 5)
	require.NoError(t, err)
	require.Empty(t, none)
	require.Zero(t, off)
}

func TestScanIgnoresIncompleteLastLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.log")
	complete := line("2026-10-17T00:00:01Z", "info", "done", "") + "\n"
	require.NoError(t, os.WriteFile(p, []byte(complete+`{"level":"info","mess`), 0o644))

	got, off, err := Tail(p, "cli", Filter{}, 10)
	require.NoError(t, err)
	require.Equal(t, []string{"done"}, messages(got))
	require.Equal(t, int64(len(complete)), off)
}

func TestMerge(t *testing.T) {
	cli := []Entry{ParseLine("cli", line("2026-10-17T00:00:01Z", "info", "c1", "")), ParseLine("cli", line("2026-10-17T00:00:03Z", "info", "c3", ""))}
	dmn := []Entry{ParseLine("daemon", line("2026-10-17T00:00:02Z", "info", "d2", ""))}

	require.Equal(t, []string{"c1", "d2", "c3"}, messages(Merge([][]Entry{cli, dmn}, 10, false)))
	require.Equal(t, []string{"d2", "c3"}, messages(Merge([][]Entry{cli, dmn}, 2, false)))
	require.Equal(t, []string{"c1", "d2"}, messages(Merge([][]Entry{cli, dmn}, 2, true)))
}

func TestRender(t *testing.T) {
	e := ParseLine("daemon", line("2026-10-17T00:00:01Z", "debug", "Hello", `"pid":7,"a":"b"`))

	var plain bytes.Buffer
	Renderer{W: &plain, Format: FormatText, ShowSource: true}.Write(e)
	require.Equal(t, "[daemon] 2026-10-17T00:00:01Z DBG a/b.go:1 > Hello a=b pid=7\n", plain.String())

	var colored bytes.Buffer
	Renderer{W: &colored, Format: FormatText, Color: true}.Write(e)
	// Which levels get a color is zerolog's choice; assert only that the output
	// is colored at all and that it matches the console writer's own rendering.
	require.Contains(t, colored.String(), "\033[")
	require.Equal(t, strip(colored.String()), plain.String()[len("[daemon] "):])

	var raw bytes.Buffer
	r := Renderer{W: &raw, Format: FormatJSON}
	r.Write(e)
	r.Write(ParseLine("cli", "not json"))
	require.Equal(t, e.Raw+"\n", raw.String())

	var text bytes.Buffer
	Renderer{W: &text, Format: FormatText}.Write(ParseLine("cli", "not json"))
	require.Equal(t, "not json\n", text.String())
}

type syncBuf struct {
	mu   sync.Mutex
	msgs []string
}

func (s *syncBuf) add(e Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, e.Message)
}

func (s *syncBuf) get() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.msgs...)
}

func appendTo(t *testing.T, p string, lines ...string) {
	t.Helper()
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = f.WriteString(strings.Join(lines, "\n") + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

func TestFollowSurvivesRotationAndLateFile(t *testing.T) {
	p := writeLog(t, line("2026-10-17T00:00:01Z", "info", "old", ""))
	_, off, err := Tail(p, "cli", Filter{}, 10)
	require.NoError(t, err)

	got := &syncBuf{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Follow(ctx, p, "cli", off, Filter{}, 10*time.Millisecond, got.add) }()

	appendTo(t, p, line("2026-10-17T00:00:02Z", "info", "appended", ""))
	require.Eventually(t, func() bool { return len(got.get()) == 1 }, 3*time.Second, 10*time.Millisecond)

	// Rotate: move the file aside and start a new one at the same path.
	require.NoError(t, os.Rename(p, p+".1"))
	appendTo(t, p, line("2026-10-17T00:00:03Z", "info", "after-rotate", ""))
	require.Eventually(t, func() bool { return len(got.get()) == 2 }, 3*time.Second, 10*time.Millisecond)

	// Truncate in place.
	// The pause lets one poll see the empty file; a truncate that regrows past
	// the old offset within a single poll is undetectable by any tailer.
	require.NoError(t, os.WriteFile(p, nil, 0o644))
	time.Sleep(50 * time.Millisecond)
	appendTo(t, p, line("2026-10-17T00:00:04Z", "info", "after-truncate", ""))
	require.Eventually(t, func() bool { return len(got.get()) == 3 }, 3*time.Second, 10*time.Millisecond)

	require.Equal(t, []string{"appended", "after-rotate", "after-truncate"}, got.get())

	cancel()
	require.NoError(t, <-done)
}

func TestFollowWaitsForMissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "late.log")
	got := &syncBuf{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = Follow(ctx, p, "cli", 0, Filter{}, 10*time.Millisecond, got.add) }()

	time.Sleep(50 * time.Millisecond)
	appendTo(t, p, line("2026-10-17T00:00:01Z", "info", "first", ""))
	require.Eventually(t, func() bool { return len(got.get()) == 1 }, 3*time.Second, 10*time.Millisecond)
}

func messages(es []Entry) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.Message)
	}
	return out
}

func TestMergeKeepsUntimedLinesInFileOrder(t *testing.T) {
	cli := []Entry{
		ParseLine("cli", line("2026-10-17T00:00:01Z", "info", "c1", "")),
		ParseLine("cli", "stack trace line"),
		ParseLine("cli", line("2026-10-17T00:00:03Z", "info", "c3", "")),
	}
	dmn := []Entry{ParseLine("daemon", line("2026-10-17T00:00:02Z", "info", "d2", ""))}

	got := Merge([][]Entry{cli, dmn}, 10, false)
	require.Equal(t, []string{"c1", "", "d2", "c3"}, messages(got))
	require.Equal(t, "stack trace line", got[1].Raw)

	single := Merge([][]Entry{{ParseLine("cli", "first"), cli[0]}}, 10, false)
	require.Equal(t, "first", single[0].Raw)
}

// strip removes ANSI color codes.
func strip(s string) string {
	return regexp.MustCompile("\x1b\\[[0-9;]*m").ReplaceAllString(s, "")
}

// wrapped is how the daemon's file sink writes a line: the finished JSON entry
// logged again as the message of an outer entry.
func wrapped(inner string) string {
	b, _ := json.Marshal(map[string]string{"time": "2026-10-17T00:43:42Z", "message": inner})
	return string(b)
}

func TestParseLineUnwrapsDoubleEncodedEntry(t *testing.T) {
	inner := `{"level":"debug","caller":"daemon/x.go:1","time":"2026-10-17T00:43:41Z","reason":"R","retry_in":2000,"message":"Watch closed"}`
	e := ParseLine("daemon", wrapped(inner))

	require.True(t, e.Valid)
	require.Equal(t, "debug", e.Level)
	require.Equal(t, "Watch closed", e.Message)
	require.Equal(t, "daemon/x.go:1", e.Caller)
	require.Equal(t, inner, e.Raw, "JSON output is the inner entry")
	require.Equal(t, [][2]string{{"reason", "R"}, {"retry_in", "2000"}}, e.fields())

	var out bytes.Buffer
	Renderer{W: &out, Format: FormatText}.Write(e)
	require.Equal(t, "2026-10-17T00:43:41Z DBG daemon/x.go:1 > Watch closed reason=R retry_in=2000\n", out.String())

	require.True(t, mustFilter(t, FilterOptions{Level: "debug", Fields: []string{"reason=R"}}).Match(e))
	require.False(t, mustFilter(t, FilterOptions{Level: "warn"}).Match(e))
}

func TestParseLineLeavesOrdinaryEntriesAlone(t *testing.T) {
	// A message that merely contains JSON must not be unwrapped, and an entry
	// that has its own level is never treated as a wrapper.
	for _, line := range []string{
		`{"level":"info","message":"{\"level\":\"debug\",\"message\":\"x\"}"}`,
		`{"time":"2026-10-17T00:00:00Z","message":"{not json"}`,
		`{"time":"2026-10-17T00:00:00Z","message":"{\"a\":1}"}`,
	} {
		e := ParseLine("cli", line)
		require.Equal(t, line, e.Raw, line)
	}
}

func TestRenderEscapesControlCharacters(t *testing.T) {
	e := ParseLine("cli", `{"time":"2026-10-17T00:00:01Z","level":"info","caller":"a/b.go:1","message":"clear\u001b[2J\u0007done","title":"\u001b]0;pwned\u0007","n":1}`)
	require.True(t, e.Valid)

	var out bytes.Buffer
	Renderer{W: &out, Format: FormatText}.Write(e)
	require.NotContains(t, out.String(), "\x1b")
	require.NotContains(t, out.String(), "\x07")
	require.Contains(t, out.String(), `clear\x1b[2J\x07done`)
	require.Contains(t, out.String(), `x1b]0;pwned`, "field values are escaped too")

	var colored bytes.Buffer
	Renderer{W: &colored, Format: FormatText, Color: true}.Write(e)
	require.NotContains(t, strip(colored.String()), "\x1b")

	var text bytes.Buffer
	Renderer{W: &text, Format: FormatText}.Write(ParseLine("cli", "raw \x1b[2J line"))
	require.Equal(t, "raw \\x1b[2J line\n", text.String())

	var js bytes.Buffer
	Renderer{W: &js, Format: FormatJSON}.Write(e)
	require.NotContains(t, js.String(), "\x1b", "JSON output keeps the escaped original")
}

func TestFollowDoesNotReplayWhenLinesLandDuringRead(t *testing.T) {
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, line("2026-10-17T00:00:01Z", "info", fmt.Sprintf("old-%d", i), ""))
	}
	p := writeLog(t, lines...)
	_, off, err := Tail(p, "cli", Filter{}, 0)
	require.NoError(t, err)

	got := &syncBuf{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Follow(ctx, p, "cli", off, Filter{}, time.Millisecond, got.add) }()

	const n = 300
	for i := 0; i < n; i++ {
		appendTo(t, p, line("2026-10-17T00:00:02Z", "info", fmt.Sprintf("new-%d", i), ""))
		time.Sleep(200 * time.Microsecond)
	}
	require.Eventually(t, func() bool { return len(got.get()) >= n }, 5*time.Second, 10*time.Millisecond)
	time.Sleep(20 * time.Millisecond)

	msgs := got.get()
	require.Len(t, msgs, n)
	require.Equal(t, "new-0", msgs[0])
	cancel()
	require.NoError(t, <-done)
}
