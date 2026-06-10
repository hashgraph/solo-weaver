// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/automa-saga/logx"
)

const defaultReadHeaderTimeout = 5 * time.Second

// ServerConfig holds tunable parameters for Server. Zero values use defaults.
type ServerConfig struct {
	// ReadHeaderTimeout is the maximum time to read request headers.
	// Defaults to 5 s if zero. Set to a shorter value in tests.
	ReadHeaderTimeout time.Duration
}

// ComponentHandler is implemented by each component to register its own HTTP
// route sub-tree on the daemon control plane.
//
// Convention: all routes registered by a handler must be prefixed with
// /<component_name>/ (e.g. /consensus_node/..., /block_node/...) to keep the
// API namespace partitioned.  Process-level routes (/health, /status) are
// registered by Server itself and must not be claimed by any ComponentHandler.
type ComponentHandler interface {
	RegisterRoutes(mux *http.ServeMux)
}

// ServerOptions groups all injectable dependencies for NewServer.
// Zero values are safe defaults.
type ServerOptions struct {
	// StatusFn returns the full daemon status for GET /status.
	// Nil disables the endpoint (returns an empty components map).
	StatusFn func() StatusResponse

	// ComponentHandlers registers per-component route sub-trees.
	// Each entry owns its own /<component>/ prefix.
	// Nil or empty slice is valid — no component routes will be registered.
	ComponentHandlers []ComponentHandler
}

// Server is the Unix socket HTTP control plane for solo-provisioner-daemon.
type Server struct {
	sockPath string
	statusFn func() StatusResponse
	srv      *http.Server
}

// NewServer constructs a Server and registers all routes.
//
// Process-level routes (/health, /status) are always registered.
// Component routes are registered by calling RegisterRoutes on each entry in
// opts.ComponentHandlers — the order of registration determines precedence for
// any accidental overlap (avoid overlaps by following the prefix convention).
//
// Route scheme: /<component>/<monitor>/<sub-resource>/<verb>
// Adding a new component means implementing ComponentHandler and appending it
// to opts.ComponentHandlers — NewServer itself never needs to change.
func NewServer(sockPath string, opts ServerOptions, cfg ServerConfig) *Server {
	s := &Server{
		sockPath: sockPath,
		statusFn: opts.StatusFn,
	}

	rht := cfg.ReadHeaderTimeout
	if rht == 0 {
		rht = defaultReadHeaderTimeout
	}

	mux := http.NewServeMux()

	// Process-level endpoints — not component-scoped.
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /status", s.handleStatus)

	// Delegate component-scoped routes to each registered handler.
	for _, h := range opts.ComponentHandlers {
		h.RegisterRoutes(mux)
	}

	s.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: rht,
	}
	return s
}

// Start removes any stale socket file, listens on the Unix socket, serves
// requests, and shuts down cleanly when ctx is cancelled.
func (s *Server) Start(ctx context.Context) error {
	sockPath := s.sockPath

	if err := os.MkdirAll(filepath.Dir(sockPath), 0o750); err != nil {
		return err
	}

	if err := os.Remove(sockPath); err != nil && !os.IsNotExist(err) {
		return err
	}

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return err
	}

	// Restrict socket to owner+group only so unprivileged local users cannot
	// reach privileged endpoints (e.g. POST /consensus_node/migration/soak/start).
	// The daemon runs as root:weaver so only root and weaver group members can connect.
	if err := os.Chmod(sockPath, 0o660); err != nil {
		_ = ln.Close()
		_ = os.Remove(sockPath)
		return err
	}

	logx.As().Info().Str("reason", "ServerStarted").Str("sock", sockPath).Msg("Daemon socket server listening")

	serveErr := make(chan error, 1)
	go func() {
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logx.As().Error().Err(err).Str("reason", "ServerStopped").Msg("Daemon socket server exited with error")
			serveErr <- err
		}
		close(serveErr)
	}()

	select {
	case <-ctx.Done():
		logx.As().Info().Str("reason", "ServerStopped").Msg("Daemon socket server shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutdownErr := s.srv.Shutdown(shutdownCtx)
		_ = os.Remove(sockPath)
		return shutdownErr
	case err := <-serveErr:
		_ = os.Remove(sockPath)
		return err
	}
}
