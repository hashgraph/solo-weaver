// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"github.com/hashgraph/solo-weaver/internal/daemon/consensus"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// NewWithComponents constructs a Daemon from pre-built sub-systems.
// Only compiled during tests; not part of the production API.
func NewWithComponents(paths models.WeaverPaths, srv *Server, mm *consensus.MigrationMonitor) *Daemon {
	return &Daemon{
		paths:            paths,
		server:           srv,
		upgradeMonitor:   nil,
		migrationMonitor: mm,
	}
}
