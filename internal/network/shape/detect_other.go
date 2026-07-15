// SPDX-License-Identifier: Apache-2.0

//go:build !linux

package shape

import "github.com/joomcode/errorx"

// DetectEgressInterface is not supported on non-Linux platforms.
// Use --egress-interface to specify the NIC name explicitly.
func DetectEgressInterface() (string, error) {
	return "", errorx.IllegalState.New(
		"egress interface auto-detection requires Linux (/proc/net/route); " +
			"use --egress-interface to specify the NIC explicitly")
}
