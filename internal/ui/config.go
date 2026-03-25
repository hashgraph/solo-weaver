// SPDX-License-Identifier: Apache-2.0

// Package ui provides the Bubble Tea TUI layer for solo-provisioner.
// It replaces the default zerolog console output with a structured, human-friendly
// display of workflow step progress using spinners, status icons, and a summary table.
//
// Global flags (set from root command):
//
//	VerboseLevel    – controls output detail: 0=compact, 1=verbose.
//	NonInteractive  – disables TUI, outputs raw zerolog for CI/pipelines.
package ui

import "os"

// VerboseLevel controls output verbosity. Set by the -V flag.
//
//	0 (default) — collapsed phases with progress bar + current step name
//	1 (-V)      — all steps (start + completion) + transient detail + version info
var VerboseLevel int

// NonInteractive disables the TUI and outputs raw zerolog lines. Set by --non-interactive.
// Intended for CI pipelines and machine-readable output.
var NonInteractive bool

// IsUnformatted returns true when the TUI should be bypassed and zerolog
// writes directly to the console. This is the case when --non-interactive is
// set OR when stdout is not a terminal (CI, pipes, redirected output).
func IsUnformatted() bool {
	if NonInteractive {
		return true
	}
	stat, err := os.Stdout.Stat()
	if err != nil {
		return true
	}
	return stat.Mode()&os.ModeCharDevice == 0
}
