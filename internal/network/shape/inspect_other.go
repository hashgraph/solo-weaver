// SPDX-License-Identifier: Apache-2.0

//go:build !linux

package shape

import "context"

// LaneTrees errors on non-Linux platforms, which cannot answer.
func LaneTrees(_ context.Context) ([]LaneTree, error) { return nil, errUnsupported() }

// TreeFilters errors on non-Linux platforms, which cannot answer.
func TreeFilters(_ context.Context, _ string) ([]string, error) { return nil, errUnsupported() }

// DeviceClassStats errors on non-Linux platforms, which cannot answer.
func DeviceClassStats(_ context.Context, _ string) (map[string]ClassStat, error) {
	return nil, errUnsupported()
}
