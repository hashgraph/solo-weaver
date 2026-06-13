// SPDX-License-Identifier: Apache-2.0

package daemonkit

import "net/http"

// ComponentHandler is implemented by each component to register its own HTTP
// route sub-tree on the daemon control plane.
//
// Convention: all routes registered by a handler must be prefixed with
// /<component_name>/ (e.g. /consensus_node/..., /block_node/...) to keep the
// API namespace partitioned. Process-level routes (/health, /status) are
// registered by the Server itself and must not be claimed by any ComponentHandler.
type ComponentHandler interface {
	RegisterRoutes(mux *http.ServeMux)
}
