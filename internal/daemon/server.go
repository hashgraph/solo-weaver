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

// Server is the Unix socket HTTP control plane for solo-provisioner-daemon.
type Server struct {
	sockPath string
	sw       *SoakWatcher
	srv      *http.Server
}

func NewServer(sockPath string, sw *SoakWatcher, cfg ServerConfig) *Server {
	s := &Server{sockPath: sockPath, sw: sw}

	rht := cfg.ReadHeaderTimeout
	if rht == 0 {
		rht = defaultReadHeaderTimeout
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /migration/consensus/soak/status", s.handleSoakStatus)
	mux.HandleFunc("POST /migration/consensus/soak/start", s.handleSoakStart)

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
	// reach privileged endpoints (e.g. POST /soak/start). The daemon runs as
	// root:weaver so only root and weaver group members can connect.
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
