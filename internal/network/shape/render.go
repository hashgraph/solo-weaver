// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"os"
	"path/filepath"
	"regexp"

	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/joomcode/errorx"
)

// nicNameRe matches valid Linux network interface names: letters, digits,
// hyphens, underscores, and periods; 1–15 characters (kernel IFNAMSIZ limit).
var nicNameRe = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,15}$`)

// scriptData is the template context for the tc-egress.sh boot script.
type scriptData struct {
	NIC       string
	SpeedMbit int // <0 omits the SPEED block (explicit per-class rates); >=0 emits sysfs auto-detect
	Device    deviceRenderData
	Classes   []classRenderData
}

// deviceRenderData is the device-level template context.
type deviceRenderData struct {
	DefaultMinor string // hex tc classid minor for the default class, e.g. "60"
	Rate         string // root trunk class rate (may be a shell expr for legacy renders)
}

// classRenderData is the per-class template context.
type classRenderData struct {
	Minor  string // hex tc classid minor, e.g. "40"
	Handle string // fq_codel qdisc handle, e.g. "140"
	Rate   string // htb rate (may be a shell expr for legacy renders)
	Ceil   string // htb ceil (may be a shell expr for legacy renders)
	Prio   int
}

// renderTcEgressScript renders the tc-egress.sh template with default
// SPEED-based rate expressions, preserving backward compatibility when no
// shape config exists. SpeedMbit is left at 0 (sysfs auto-detect at boot).
func renderTcEgressScript(nicName string) (string, error) {
	return renderTcEgressScriptFromData(defaultScriptData(nicName))
}

// renderTcEgressScriptFromConfig renders the tc-egress.sh template from the
// provided device and class configurations. Classes are rendered in the order
// supplied by the caller; loadClassesForDir returns them sorted by name.
func renderTcEgressScriptFromConfig(nicName string, dev *DeviceConfig, classes []*ClassConfig) (string, error) {
	info, err := lookupClassInfo(dev.DefaultClass)
	if err != nil {
		return "", err
	}
	crs := make([]classRenderData, 0, len(classes))
	for _, cls := range classes {
		ci, err := lookupClassInfo(cls.Name)
		if err != nil {
			return "", err
		}
		crs = append(crs, classRenderData{
			Minor:  ci.Minor,
			Handle: ci.Handle,
			Rate:   cls.Rate,
			Ceil:   cls.effectiveCeil(),
			Prio:   cls.Prio,
		})
	}
	return renderTcEgressScriptFromData(scriptData{
		NIC:       nicName,
		SpeedMbit: -1, // all rates are explicit; SPEED variable not needed
		Device: deviceRenderData{
			DefaultMinor: info.Minor,
			Rate:         dev.Rate,
		},
		Classes: crs,
	})
}

// defaultScriptData returns the legacy SPEED-based scriptData for the egress
// device: the three default egress classes (partner/public/reserve-egress) with
// shell arithmetic rate expressions. This matches the pre-shape-config tc-egress.sh
// template content so install-time rendering is backward-compatible.
func defaultScriptData(nicName string) scriptData {
	return scriptData{
		NIC: nicName,
		Device: deviceRenderData{
			DefaultMinor: "60",
			Rate:         "${SPEED}mbit",
		},
		Classes: []classRenderData{
			{Minor: "40", Handle: "140", Rate: `$(( SPEED * 40 / 100 ))mbit`, Ceil: `$(( SPEED * 70 / 100 ))mbit`, Prio: 0},
			{Minor: "50", Handle: "150", Rate: `$(( SPEED * 30 / 100 ))mbit`, Ceil: `$(( SPEED * 70 / 100 ))mbit`, Prio: 5},
			{Minor: "60", Handle: "160", Rate: `$(( SPEED * 30 / 100 ))mbit`, Ceil: `${SPEED}mbit`, Prio: 1},
		},
	}
}

// renderTcEgressScriptFromData validates the NIC name and renders the template
// with the given scriptData. NIC validation lives here — the single funnel every
// render passes through — so operator-supplied names (e.g. block node install
// --egress-interface) cannot inject into the root-executed boot script.
func renderTcEgressScriptFromData(data scriptData) (string, error) {
	if !nicNameRe.MatchString(data.NIC) {
		return "", errorx.IllegalArgument.New(
			"egress NIC name %q is invalid: must match %s", data.NIC, nicNameRe.String())
	}
	rendered, err := templates.Render(tcEgressScriptTemplate, data)
	if err != nil {
		return "", errorx.InternalError.Wrap(err, "failed to render %s", tcEgressScriptTemplate)
	}
	return rendered, nil
}

// atomicWriteFile writes content to path via a temp file in the same directory
// followed by fsync + rename + parent-dir fsync, so a crash mid-write cannot
// leave a torn file that the boot oneshot would fail to execute.
func atomicWriteFile(path, content string, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to create directory %s", dir)
	}
	tmp, err := os.CreateTemp(dir, ".shape-*.tmp")
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
