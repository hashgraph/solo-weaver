// SPDX-License-Identifier: Apache-2.0

// Package ui provides the Bubble Tea TUI layer for solo-provisioner.
// It replaces the default zerolog console output with a structured, human-friendly
// display of workflow step progress using spinners, status icons, and a summary table.
//
// Global flags (set from root command):
//
//	Verbose – when true, doctor.CheckErr shows full stacktrace/profiling detail.
//	NoTUI   – when true, forces the fallback line-based handler even on a TTY.
package ui

// Verbose controls whether error diagnostics include the full stacktrace,
// profiling snapshots, and internal metadata. Set by the --verbose / -V flag.
var Verbose bool

// NoTUI forces the fallback (non-interactive) output handler even when stdout is
// a terminal. Set by the hidden --no-tui flag. Automatically true when stdout is
// not a TTY.
var NoTUI bool

