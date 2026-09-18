// SPDX-License-Identifier: Apache-2.0

//go:build linux

package mountinfo

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/sys/unix"
)

// sysfs roots and the stat syscall, vars so tests can point the probe at a
// fixture tree and fake a block device node without CAP_MKNOD.
var (
	sysDevBlockDir = "/sys/dev/block"
	sysClassDir    = "/sys/class"
	statFn         = unix.Stat
)

// networkHostClasses maps a SCSI host class directory to its fabric name. SAS and
// SATA are local fabrics and are deliberately absent.
var networkHostClasses = []struct{ class, transport string }{
	{"iscsi_host", "iSCSI"},
	{"fc_host", "Fibre Channel"},
}

// nvmeFabrics maps an NVMe controller's sysfs transport attribute to its fabric
// name. "pcie" and "loop" are local and deliberately absent.
var nvmeFabrics = map[string]string{
	"tcp":  "NVMe/TCP",
	"rdma": "NVMe/RDMA",
	"fc":   "NVMe/FC",
}

// maxSlaveDepth bounds the walk down stacked devices; LVM on multipath on iSCSI
// is three levels. visited guards against a cycle.
const maxSlaveDepth = 4

var (
	devIDPattern      = regexp.MustCompile(`^[0-9]+:[0-9]+$`)
	hostSegmentRegexp = regexp.MustCompile(`^host[0-9]+$`)
	nvmeCtrlRegexp    = regexp.MustCompile(`^nvme[0-9]+$`)
	nvmeSubsysRegexp  = regexp.MustCompile(`^nvme-subsys[0-9]+$`)
)

// NetworkTransport reports the network fabric behind a mount's block device,
// or "" when it's local or can't be resolved.
func NetworkTransport(devID, source string) string {
	dir, ok := blockDeviceDir(devID)
	if !ok {
		// btrfs and friends report an anonymous maj:min that resolves to
		// nothing; the device node still does.
		dir, ok = blockDeviceDir(deviceNumberOf(source))
	}
	if !ok {
		return ""
	}
	return transportOf(dir, 0, map[string]bool{})
}

// blockDeviceDir resolves a "maj:min" to its real sysfs device directory.
func blockDeviceDir(devID string) (string, bool) {
	if !devIDPattern.MatchString(devID) {
		return "", false
	}
	dir, err := filepath.EvalSymlinks(filepath.Join(sysDevBlockDir, devID))
	if err != nil {
		return "", false
	}
	return dir, true
}

// deviceNumberOf returns the "maj:min" of the block device node at source, or ""
// when source is not one (tmpfs, an NFS export, a zfs pool name, a char device).
func deviceNumberOf(source string) string {
	var st unix.Stat_t
	if err := statFn(source, &st); err != nil {
		return ""
	}
	if st.Mode&unix.S_IFMT != unix.S_IFBLK {
		return ""
	}
	rdev := uint64(st.Rdev)
	return fmt.Sprintf("%d:%d", unix.Major(rdev), unix.Minor(rdev))
}

// transportOf reports the fabric of the device at dir, descending into slaves/
// for dm and md devices, which sit under devices/virtual and name no controller.
func transportOf(dir string, depth int, visited map[string]bool) string {
	if depth > maxSlaveDepth || visited[dir] {
		return ""
	}
	visited[dir] = true

	if t := controllerTransport(dir); t != "" {
		return t
	}

	slavesDir := filepath.Join(dir, "slaves")
	slaves, err := os.ReadDir(slavesDir)
	if err != nil {
		return ""
	}
	for _, s := range slaves {
		sub, err := filepath.EvalSymlinks(filepath.Join(slavesDir, s.Name()))
		if err != nil {
			continue
		}
		if t := transportOf(sub, depth+1, visited); t != "" {
			return t
		}
	}
	return ""
}

// controllerTransport maps the controller in dir's sysfs path to a fabric: a
// hostN for SCSI, or an nvmeN (direct or under a multipath subsystem) for NVMe.
func controllerTransport(dir string) string {
	for _, seg := range strings.Split(dir, string(os.PathSeparator)) {
		switch {
		case hostSegmentRegexp.MatchString(seg):
			for _, hc := range networkHostClasses {
				if _, err := os.Stat(filepath.Join(sysClassDir, hc.class, seg)); err == nil {
					return hc.transport
				}
			}
		case nvmeCtrlRegexp.MatchString(seg):
			if t := nvmeFabric(filepath.Join(sysClassDir, "nvme", seg)); t != "" {
				return t
			}
		case nvmeSubsysRegexp.MatchString(seg):
			subsys := filepath.Join(sysClassDir, "nvme-subsystem", seg)
			ctrls, _ := os.ReadDir(subsys)
			for _, c := range ctrls {
				if !nvmeCtrlRegexp.MatchString(c.Name()) {
					continue
				}
				if t := nvmeFabric(filepath.Join(subsys, c.Name())); t != "" {
					return t
				}
			}
		}
	}
	return ""
}

// nvmeFabric reads the transport attribute of the NVMe controller at ctrlDir.
func nvmeFabric(ctrlDir string) string {
	b, err := os.ReadFile(filepath.Join(ctrlDir, "transport"))
	if err != nil {
		return ""
	}
	return nvmeFabrics[strings.TrimSpace(string(b))]
}
