// SPDX-License-Identifier: Apache-2.0

package ui

import "time"

// PhaseBenchmarks maps phase IDs to their expected durations.
// Used by the progress bar to estimate remaining time. Values are rough
// averages from real deployments — the bar never blocks on them.
var PhaseBenchmarks = map[string]time.Duration{
	"block-node-preflight":    1 * time.Second,
	"block-system-setup": 6 * time.Second,
	"kubernetes-setup":        180 * time.Second,
	"setup-block-node":        90 * time.Second,
}
