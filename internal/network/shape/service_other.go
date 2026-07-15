// SPDX-License-Identifier: Apache-2.0

//go:build !linux

package shape

import "context"

// EnsureTcEgressUnit is a no-op on non-Linux platforms. The tc-egress oneshot
// unit is a Linux systemd concept; the package compiles and tests on all
// platforms, but the service management calls only run on Linux.
func EnsureTcEgressUnit(_ context.Context) error { return nil }
