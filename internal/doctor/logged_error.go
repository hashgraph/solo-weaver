// SPDX-License-Identifier: Apache-2.0

package doctor

import (
	"fmt"
	"io"
	"strings"

	"github.com/hashgraph/solo-weaver/internal/doctor/logview"
)

// RenderLoggedError prints a logged error entry in the same layout as the -V
// panel, rebuilt from the fields CheckErr writes to the log: the stack trace is
// the message, and the reason, hints, build and pid are fields. Fields the log
// does not carry (error type and code) are left out. logfile is shown as the
// place the entry came from.
func RenderLoggedError(w io.Writer, e logview.Entry, logfile string, color bool) {
	paint := func(code, s string) string {
		if !color {
			return s
		}
		return code + s + Reset
	}
	label := func(code, s string) string { return paint(code, s+":") }
	str := func(key string) string {
		v, _ := e.Obj[key].(string)
		return logview.Sanitize(v)
	}

	reason := str("reason")
	message := logview.Sanitize(e.Message)
	if reason != "" {
		message = stripPrintableProperty(message, "reason", reason)
	}

	fmt.Fprintf(w, "\n%s\n", paint(Bold+Gray, "************************************** Error Stacktrace ******************************************"))
	fmt.Fprintf(w, "\n%s\n", message)

	fmt.Fprintf(w, "\n%s\n", paint(Bold+Red, "************************************** Error Diagnostics ******************************************"))
	fmt.Fprintf(w, "\t%s %s\n", label(Bold+White, "Error"), firstLine(message))
	if reason != "" {
		fmt.Fprintf(w, "\t%s %s\n", label(Bold+White, "Reason"), reason)
	}
	if !e.Time.IsZero() {
		fmt.Fprintf(w, "\t%s %s\n", label(Gray, "Time"), str("time"))
	}
	if e.Caller != "" {
		fmt.Fprintf(w, "\t%s %s\n", label(Gray, "Caller"), logview.Sanitize(e.Caller))
	}
	if v := str("build_commit"); v != "" {
		fmt.Fprintf(w, "\t%s %s\n", label(Gray, "Commit"), v)
	}
	if v := str("build_version"); v != "" {
		fmt.Fprintf(w, "\t%s %s\n", label(Gray, "Version"), v)
	}
	if v, ok := e.Obj["pid"]; ok {
		fmt.Fprintf(w, "\t%s %v\n", label(Gray, "Pid"), logview.Sanitize(fmt.Sprint(v)))
	}
	if logfile != "" {
		fmt.Fprintf(w, "\t%s %s\n", label(Cyan, "Logfile"), logview.Sanitize(logfile))
	}

	hints := loggedHints(e)
	if len(hints) == 0 {
		return
	}
	fmt.Fprintf(w, "\n%s\n", paint(Bold+Yellow, "****************************************** Resolution *********************************************"))
	for i, h := range hints {
		fmt.Fprintf(w, "\t%s\n", paint(White, fmt.Sprintf("%d. %s", i+1, h)))
	}
}

// loggedHints returns the remediation hints CheckErr logged in the "hints" field.
func loggedHints(e logview.Entry) []string {
	raw, _ := e.Obj["hints"].([]any)
	hints := make([]string, 0, len(raw))
	for _, h := range raw {
		if s, ok := h.(string); ok && s != "" {
			hints = append(hints, logview.Sanitize(s))
		}
	}
	return hints
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}
