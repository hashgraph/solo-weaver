// SPDX-License-Identifier: Apache-2.0

// Package ui provides the Bubble Tea TUI layer for solo-provisioner.
// It replaces the default zerolog console output with a structured, human-friendly
// display of workflow step progress using spinners, status icons, and a summary table.
//
// Global flags (set from root command):
//
//	VerboseLevel – controls output detail: 0=compact, 1=completions only, 2=full detail, 3=unformatted.
package ui

// VerboseLevel controls output verbosity. Set by the -V count flag.
//
//	0 (default) — collapsed phases with progress bar + current step name
//	1 (-V)      — completion lines only (✓/✗/⊘), no start lines or transient detail
//	2 (-VV)     — all steps (start + completion) + detail lines + version info + full error stacktraces
//	3 (-VVV)    — unformatted: no TUI/fallback, zerolog writes directly to the console
var VerboseLevel int

// IsUnformatted returns true when verbosity is high enough (-VVV) to bypass
// all output formatting and let zerolog write directly to the console.
func IsUnformatted() bool {
	return VerboseLevel >= 3
}
