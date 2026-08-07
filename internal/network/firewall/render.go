// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
)

// renderData is the flattened view of a Table passed to the nft template.
// Strings are pre-joined here because templates.Render parses without a FuncMap,
// so the template itself cannot call join. The `*6` fields carry the IPv6-family
// members so the template can declare parallel ipv6_addr sets and `ip6` rules.
type renderData struct {
	MgmtElements     string
	MgmtElements6    string
	BlockedElements  string
	BlockedElements6 string
	PortElements     string
	SSHPort          int
	PodCIDR          string
	PodCIDR6         string
}

// Render produces the full `inet weaver-host-firewall` nft document for this table. The same
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

	mgmtV4, mgmtV6 := splitCIDRsByFamily(t.MgmtCIDRs)
	blockedV4, blockedV6 := splitCIDRsByFamily(t.BlockedCIDRs)
	podV4, podV6 := routePodCIDRs(t.PodCIDR, t.PodCIDR6)

	data := renderData{
		MgmtElements:     strings.Join(mgmtV4, ", "),
		MgmtElements6:    strings.Join(mgmtV6, ", "),
		BlockedElements:  strings.Join(blockedV4, ", "),
		BlockedElements6: strings.Join(blockedV6, ", "),
		PortElements:     strings.Join(ports, ", "),
		SSHPort:          t.SSHPort,
		PodCIDR:          podV4,
		PodCIDR6:         podV6,
	}

	rendered, err := templates.Render(hostNftTemplate, data)
	if err != nil {
		return "", errorx.InternalError.Wrap(err, "failed to render inet weaver-host-firewall table")
	}

	return rendered, nil
}

// splitCIDRsByFamily partitions a validated mixed CIDR list into its IPv4 and
// IPv6 members, preserving order. Table.Validate has already run, so a
// classification error is not expected here; a value that somehow fails to
// classify is dropped from both lists rather than smuggled into the wrong-family
// nft set (which nft would reject at apply time anyway).
func splitCIDRsByFamily(cidrs []string) (v4, v6 []string) {
	for _, c := range cidrs {
		isV6, err := sanity.CIDRIsIPv6(c)
		if err != nil {
			continue
		}
		if isV6 {
			v6 = append(v6, c)
		} else {
			v4 = append(v4, c)
		}
	}
	return v4, v6
}

// routePodCIDRs assigns the two pod-CIDR fields to the v4/v6 render slots by
// their actual family, so a value placed in either Table field renders into the
// correct `ip`/`ip6` in-cluster rule even if a caller slotted it by the wrong
// field. When both fields carry the same family the later one wins that slot.
func routePodCIDRs(pod, pod6 string) (v4, v6 string) {
	for _, c := range []string{pod, pod6} {
		if c == "" {
			continue
		}
		if isV6, err := sanity.CIDRIsIPv6(c); err == nil && isV6 {
			v6 = c
		} else {
			v4 = c
		}
	}
	return v4, v6
}

// atomicWriteFile writes content to path via a temp file in the same directory
// followed by fsync + rename + parent-dir fsync, so a crash mid-write can never
// leave a torn nft file that the boot oneshot would fail to load.
func atomicWriteFile(path, content string, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to create directory %s", dir)
	}

	tmp, err := os.CreateTemp(dir, ".network-weaver-host-firewall-*.nft.tmp")
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
