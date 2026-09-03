// SPDX-License-Identifier: Apache-2.0

//go:build linux

package unitconv

import (
	"bytes"
	"context"
	"os"
	"path/filepath"

	"github.com/automa-saga/logx"
	soos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/joomcode/errorx"
)

// EnsureUnit installs embedded at unitPath, daemon-reloads systemd, and enables
// service for boot. Write-side counterpart of NeedsConverge; a unit that is
// already current and enabled is a no-op, and a failed enable only warns.
// See docs/dev/traffic-shaper.md for why the bytes are compared, not stat-ed.
func EnsureUnit(ctx context.Context, unitPath, service string, embedded []byte) error {
	if current, err := os.ReadFile(unitPath); err == nil && bytes.Equal(current, embedded) {
		return EnableIfDisabled(ctx, service)
	}

	if err := atomicWriteUnit(unitPath, embedded); err != nil {
		return err
	}
	if err := soos.DaemonReload(ctx); err != nil {
		return err
	}
	if err := soos.EnableService(ctx, service); err != nil {
		logx.As().Warn().Err(err).Str("service", service).Msg("could not enable service at boot")
	}
	return nil
}

// atomicWriteUnit writes the unit via temp file + fsync + rename, so a crash
// mid-write cannot leave a torn unit systemd would fail to parse at boot.
func atomicWriteUnit(path string, content []byte) error {
	const perm = 0o644

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to create directory %s", dir)
	}
	tmp, err := os.CreateTemp(dir, ".unitconv-*.tmp")
	if err != nil {
		return errorx.ExternalError.Wrap(err, "failed to create temp file in %s", dir)
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return errorx.ExternalError.Wrap(err, "failed to write temp file %s", tmpName)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return errorx.ExternalError.Wrap(err, "failed to chmod temp file %s", tmpName)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return errorx.ExternalError.Wrap(err, "failed to fsync temp file %s", tmpName)
	}
	if err := tmp.Close(); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to close temp file %s", tmpName)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to rename %s to %s", tmpName, path)
	}
	committed = true

	d, err := os.Open(dir)
	if err != nil {
		return errorx.ExternalError.Wrap(err, "failed to open directory %s for fsync", dir)
	}
	defer func() { _ = d.Close() }()
	if err := d.Sync(); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to fsync directory %s", dir)
	}
	return nil
}
