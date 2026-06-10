// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"github.com/hashgraph/solo-weaver/internal/daemon/consensus"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// NewWithComponents constructs a Daemon from pre-built sub-systems.
// Only compiled during tests; not part of the production API.
// mm may be nil when the migration monitor is not needed by the test.
func NewWithComponents(paths models.WeaverPaths, srv *Server, mm *consensus.MigrationMonitor) *Daemon {
	var components []component
	if mm != nil {
		components = append(components, component{
			name:     "consensus-node",
			monitors: []MonitorRunner{mm},
		})
	}
	return &Daemon{
		paths:      paths,
		server:     srv,
		components: components,
	}
}
