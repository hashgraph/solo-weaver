// SPDX-License-Identifier: Apache-2.0

//go:build !linux

package shape

// ReadLinkSpeedMbit is not supported on non-Linux platforms.
func ReadLinkSpeedMbit(_ string) (int, bool) { return 0, false }
