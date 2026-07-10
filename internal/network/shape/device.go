// SPDX-License-Identifier: Apache-2.0

package shape

import "time"

// DeviceConfig is the persisted root-level tc configuration for one traffic
// direction. One JSON file per device under DeviceConfigDir (named by Dir).
//
// Dir "egress" targets the $EGRESS physical NIC and drives the re-rendered
// tc-egress.sh boot script. Dir "ingress" targets $VETH and is consumed by
// the daemon pod-lifecycle watcher (TS_3); no script is rendered for it.
type DeviceConfig struct {
	Dir          string    `json:"dir"`           // "ingress" or "egress"
	Rate         string    `json:"rate"`          // root HTB trunk class rate
	DefaultClass string    `json:"default_class"` // class name; unmatched traffic falls here
	CreatedAt    time.Time `json:"created_at"`
}
