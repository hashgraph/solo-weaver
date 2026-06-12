// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/hashgraph/solo-weaver/internal/daemon/core"
)

const defaultReadHeaderTimeout = 5 * time.Second

// ServerConfig holds tunable parameters for Server. Zero values use defaults.
type ServerConfig struct {
	// ReadHeaderTimeout is the maximum time to read request headers.
	// Defaults to 5 s if zero. Set to a shorter value in tests.
	ReadHeaderTimeout time.Duration
}

// ServerOptions groups all injectable dependencies for NewServer.
type ServerOptions struct {
	// StatusFn returns the full daemon status for GET /status.
	// Nil disables the endpoint (returns an empty components map).
	StatusFn func() StatusResponse

	// ComponentHandlers registers per-component route sub-trees.
	// Each entry owns its own /<component>/ prefix.
	ComponentHandlers []core.ComponentHandler
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
// opts.ComponentHandlers.
//
// Route scheme: /<component>/<monitor>/<sub-resource>/<verb>
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

	if err := os.Chmod(sockPath, 0o660); err != nil {
		_ = ln.Close()
		_ = os.Remove(sockPath)
		return err
	}

	slog.Info("Daemon socket server listening", "reason", "ServerStarted", "sock", sockPath)

	serveErr := make(chan error, 1)
	go func() {
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Daemon socket server exited with error", "error", err, "reason", "ServerStopped")
			serveErr <- err
		}
		close(serveErr)
	}()

	select {
	case <-ctx.Done():
		slog.Info("Daemon socket server shutting down", "reason", "ServerStopped")
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
