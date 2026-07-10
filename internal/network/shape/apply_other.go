// SPDX-License-Identifier: Apache-2.0

//go:build !linux

package shape

import "context"

// ApplyTcEgressScript is a no-op on non-Linux platforms. The tc subsystem is
// Linux-kernel-specific; the package compiles and tests on all platforms but
// the actual `tc` invocation only runs on Linux.
func ApplyTcEgressScript(_ context.Context) error { return nil }
