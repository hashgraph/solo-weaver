// SPDX-License-Identifier: Apache-2.0

//go:build linux

package shape

// procNetRoute is the kernel's routing table in human-readable form.
const procNetRoute = "/proc/net/route"

// DetectEgressInterface returns the name of the interface that carries the
// default route — the physical $EGRESS NIC the HTB hierarchy should be
// attached to. On multi-NIC hosts the desired interface must be specified
// explicitly via --egress-interface.
//
// Fails with an actionable error when no default route is found (e.g. the
// routing table is not yet populated or the host has no default gateway).
func DetectEgressInterface() (string, error) {
	return detectEgressInterfaceFrom(procNetRoute)
}
