// SPDX-License-Identifier: Apache-2.0

//go:build !linux

package policy

import "context"

// defaultEnsureService is a no-op on non-Linux platforms so the package builds
// and unit-tests on macOS/CI. Enabling the systemd oneshot only makes sense on
// the Linux host where solo-provisioner runs.
func defaultEnsureService(_ context.Context) error { return nil }
