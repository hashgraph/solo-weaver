// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"

	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/joomcode/errorx"
)

// scriptData is the template context for the tc-egress.sh boot script.
type scriptData struct {
	NIC       string
	SpeedMbit int // 0 means auto-detect from sysfs at boot
}

// renderTcEgressScript renders the template and returns the script string
// without writing anything to disk. Used by RenderTcEgressScript and tests.
func renderTcEgressScript(nicName string, speedMbit int) (string, error) {
	rendered, err := templates.Render(tcEgressScriptTemplate, scriptData{NIC: nicName, SpeedMbit: speedMbit})
	if err != nil {
		return "", errorx.InternalError.Wrap(err, "failed to render %s", tcEgressScriptTemplate)
	}
	return rendered, nil
}

// RenderTcEgressScript renders the tc-egress.sh boot script with the given NIC
// name and writes it atomically to TcEgressScriptPath (mode 0755). If the
// on-disk content is already identical (SHA-256 match) the write is skipped
// and the function returns nil — making it safe to call from idempotent
// install flows.
//
// speedMbit sets the link rate in Mbit/s directly in the rendered script,
// bypassing the runtime sysfs detection. Pass 0 to keep the auto-detect
// behaviour (reads /sys/class/net/<nic>/speed at boot, falls back to 1000).
func RenderTcEgressScript(nicName string, speedMbit int) error {
	if nicName == "" {
		return errorx.IllegalArgument.New("egress NIC name must not be empty")
	}

	rendered, err := renderTcEgressScript(nicName, speedMbit)
	if err != nil {
		return err
	}

	// Skip the write when the on-disk file already has the same content.
	if existing, readErr := os.ReadFile(TcEgressScriptPath); readErr == nil {
		if sha256.Sum256([]byte(rendered)) == sha256.Sum256(existing) {
			return nil
		}
	}

	return atomicWriteFile(TcEgressScriptPath, rendered, 0o755)
}

// atomicWriteFile writes content to path via a temp file in the same directory
// followed by fsync + rename + parent-dir fsync, so a crash mid-write cannot
// leave a torn script that the boot oneshot would fail to execute.
func atomicWriteFile(path, content string, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to create directory %s", dir)
	}

	tmp, err := os.CreateTemp(dir, ".tc-egress-*.sh.tmp")
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

	if _, err := tmp.WriteString(content); err != nil {
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

// contentEqual is a helper for tests that need to compare rendered output
// without going through SHA-256 directly.
func contentEqual(a, b string) bool {
	return bytes.Equal([]byte(a), []byte(b))
}
