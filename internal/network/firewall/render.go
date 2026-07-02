// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/joomcode/errorx"
)

// renderData is the flattened view of a Table passed to the nft template.
// Strings are pre-joined here because templates.Render parses without a FuncMap,
// so the template itself cannot call join.
type renderData struct {
	MgmtElements string
	PortElements string
	SSHPort      int
	PodCIDR      string
}

// Render produces the full `inet host` nft document for this table. The same
// output feeds both the kernel apply (`nft -f`) and the on-disk artifact, so
// the live table and the persisted file can never diverge.
func (t *Table) Render() (string, error) {
	if err := t.Validate(); err != nil {
		return "", err
	}

	ports := make([]string, len(t.InClusterPorts))
	for i, p := range t.InClusterPorts {
		ports[i] = strconv.Itoa(p)
	}

	data := renderData{
		MgmtElements: strings.Join(t.MgmtCIDRs, ", "),
		PortElements: strings.Join(ports, ", "),
		SSHPort:      t.SSHPort,
		PodCIDR:      t.PodCIDR,
	}

	rendered, err := templates.Render(hostNftTemplate, data)
	if err != nil {
		return "", errorx.InternalError.Wrap(err, "failed to render inet host table")
	}

	return rendered, nil
}

// atomicWriteFile writes content to path via a temp file in the same directory
// followed by fsync + rename + parent-dir fsync, so a crash mid-write can never
// leave a torn nft file that the boot oneshot would fail to load.
func atomicWriteFile(path, content string, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to create directory %s", dir)
	}

	tmp, err := os.CreateTemp(dir, ".network-host-*.nft.tmp")
	if err != nil {
		return errorx.ExternalError.Wrap(err, "failed to create temp file in %s", dir)
	}
	tmpName := tmp.Name()

	// Best-effort cleanup if we bail before the rename succeeds.
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
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return errorx.ExternalError.Wrap(err, "failed to fsync temp file %s", tmpName)
	}
	if err := tmp.Close(); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to close temp file %s", tmpName)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to chmod temp file %s", tmpName)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to rename %s to %s", tmpName, path)
	}
	committed = true

	// fsync the parent directory so the rename itself is durable.
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
