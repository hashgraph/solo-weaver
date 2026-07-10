// SPDX-License-Identifier: Apache-2.0

//go:build !linux

package policy

import "context"

// RestartNetworkNftService is a no-op on non-Linux platforms so the package
// builds and unit-tests on macOS/CI. Restarting the systemd oneshot only makes
// sense on the Linux host where solo-provisioner runs.
func RestartNetworkNftService(_ context.Context) error { return nil }
