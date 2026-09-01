// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/joomcode/errorx"
)

// renderData is the flattened view of a Table passed to the nft template. The
// three reserved blocks get their own fields because each renders into a
// position an allow rule cannot reach; Allow is ranged over uniformly.
type renderData struct {
	Mgmt      ruleRender
	Blocked   ruleRender
	InCluster ruleRender
	Allow     []ruleRender
}

// ruleRender is one Rule flattened for the template. Element lists are
// pre-joined here because templates.Render parses without a FuncMap, so the
// template itself cannot call join. The `*6` fields carry the IPv6-family
// members so the template can declare parallel ipv6_addr sets and `ip6` rules.
type ruleRender struct {
	Name         string
	AddrSet      string
	AddrSet6     string
	PortsSet     string
	Elements     string
	Elements6    string
	PortElements string
	Proto        string
	// HasV4/HasV6 gate the per-family rule, so a rule whose sources are all one
	// family does not emit a dead rule in the other family's chain.
	HasV4    bool
	HasV6    bool
	HasPorts bool
	ICMPEcho bool
}

// Render produces the full `inet weaver-host-firewall` nft document for this table. The same
// output feeds both the kernel apply (`nft -f`) and the on-disk artifact, so
// the live table and the persisted file can never diverge.
func (t *Table) Render() (string, error) {
	if err := t.Validate(); err != nil {
		return "", err
	}

	data := renderData{}
	for _, f := range []struct {
		rule *Rule
		dst  *ruleRender
	}{
		{&t.Mgmt, &data.Mgmt},
		{&t.Blocked, &data.Blocked},
		{&t.InCluster, &data.InCluster},
	} {
		flat, err := flattenRule(f.rule)
		if err != nil {
			return "", err
		}
		*f.dst = flat
	}
	for i := range t.Allow {
		flat, err := flattenRule(&t.Allow[i])
		if err != nil {
			return "", err
		}
		data.Allow = append(data.Allow, flat)
	}

	rendered, err := templates.Render(hostNftTemplate, data)
	if err != nil {
		return "", errorx.InternalError.Wrap(err, "failed to render inet weaver-host-firewall table")
	}

	return rendered, nil
}

// flattenRule converts a validated Rule into its template view, splitting the
// address list by family and joining each list into an nft `elements = { … }`
// body. It errors on an address that is not a CIDR — see splitCIDRs for why an
// unexpanded FQDN must not be skipped.
func flattenRule(r *Rule) (ruleRender, error) {
	v4, v6, err := splitCIDRs(r.CIDRs)
	if err != nil {
		return ruleRender{}, err
	}
	return ruleRender{
		Name:         r.Name,
		AddrSet:      addrSetName(r.Name),
		AddrSet6:     v6SetName(r.Name),
		PortsSet:     portsSetName(r.Name),
		Elements:     strings.Join(v4, ", "),
		Elements6:    strings.Join(v6, ", "),
		PortElements: strings.Join(r.Ports, ", "),
		Proto:        string(r.proto()),
		HasV4:        len(v4) > 0,
		HasV6:        len(v6) > 0,
		HasPorts:     len(r.Ports) > 0,
		ICMPEcho:     r.ICMPEcho,
	}, nil
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
