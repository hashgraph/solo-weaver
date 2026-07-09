// SPDX-License-Identifier: Apache-2.0

//go:build linux

package shape

// procNetRoute is the kernel's routing table in human-readable form.
const procNetRoute = "/proc/net/route"

// DetectEgressInterface returns the name of the interface that carries the
// default route — the physical $EGRESS NIC the HTB hierarchy should be
// attached to. Works for single-NIC hosts (§4.1); multi-NIC support (§4.2)
// is out of scope and requires --egress-interface to be set explicitly.
//
// Fails with an actionable error when no default route is found (e.g. the
// routing table is not yet populated or the host has no default gateway).
func DetectEgressInterface() (string, error) {
	return detectEgressInterfaceFrom(procNetRoute)
}
