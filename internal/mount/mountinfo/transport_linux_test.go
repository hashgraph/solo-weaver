// SPDX-License-Identifier: Apache-2.0

//go:build linux

package mountinfo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// sysfsTree builds a fixture shaped like a real /sys: device dirs, dev/block
// symlinks, and the class dirs that name a controller's fabric.
type sysfsTree struct {
	root string
	t    *testing.T
}

func newSysfsTree(t *testing.T) *sysfsTree {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	tree := &sysfsTree{root: root, t: t}
	require.NoError(t, os.MkdirAll(filepath.Join(root, "dev", "block"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "class"), 0o755))

	prevDev, prevClass := sysDevBlockDir, sysClassDir
	sysDevBlockDir = filepath.Join(root, "dev", "block")
	sysClassDir = filepath.Join(root, "class")
	t.Cleanup(func() { sysDevBlockDir, sysClassDir = prevDev, prevClass })

	return tree
}

// device creates a device directory at devPath (relative to the sysfs root) and
// links dev/block/<devID> at it.
func (s *sysfsTree) device(devID, devPath string) string {
	s.t.Helper()
	abs := filepath.Join(s.root, devPath)
	require.NoError(s.t, os.MkdirAll(abs, 0o755))
	require.NoError(s.t, os.Symlink(abs, filepath.Join(sysDevBlockDir, devID)))
	return abs
}

// hostClass registers hostN under a sysfs host class, e.g. "iscsi_host".
func (s *sysfsTree) hostClass(class, host string) {
	s.t.Helper()
	require.NoError(s.t, os.MkdirAll(filepath.Join(sysClassDir, class, host), 0o755))
}

// nvmeController registers an NVMe controller with a transport attribute,
// e.g. ("nvme", "nvme0", "tcp") or ("nvme-subsystem", "nvme-subsys0/nvme0", "rdma").
func (s *sysfsTree) nvmeController(class, ctrl, transport string) {
	s.t.Helper()
	dir := filepath.Join(sysClassDir, class, ctrl)
	require.NoError(s.t, os.MkdirAll(dir, 0o755))
	require.NoError(s.t, os.WriteFile(filepath.Join(dir, "transport"), []byte(transport+"\n"), 0o644))
}

// slave links an underlying device into a virtual device's slaves/ directory.
func (s *sysfsTree) slave(virtualDir, name, targetDir string) {
	s.t.Helper()
	slaves := filepath.Join(virtualDir, "slaves")
	require.NoError(s.t, os.MkdirAll(slaves, 0o755))
	require.NoError(s.t, os.Symlink(targetDir, filepath.Join(slaves, name)))
}

// stubBlockDevice makes stat report path as a block device node with the given
// major:minor. Creating a real node needs CAP_MKNOD, which tests don't have.
func stubBlockDevice(t *testing.T, path string, major, minor uint32) {
	t.Helper()
	prev := statFn
	statFn = func(p string, st *unix.Stat_t) error {
		if p != path {
			return unix.Stat(p, st)
		}
		st.Mode = unix.S_IFBLK | 0o660
		st.Rdev = unix.Mkdev(major, minor)
		return nil
	}
	t.Cleanup(func() { statFn = prev })
}

func Test_NetworkTransport_DirectISCSIDevice(t *testing.T) {
	tree := newSysfsTree(t)
	// Shape taken from a live session: /sys/dev/block/8:0 ->
	// ../../devices/platform/host1/session1/target1:0:0/1:0:0:0/block/sda.
	tree.device("8:17", "devices/platform/host7/session3/target7:0:0/7:0:0:1/block/sdb/sdb1")
	tree.hostClass("iscsi_host", "host7")
	tree.hostClass("scsi_host", "host7")

	assert.Equal(t, "iSCSI", NetworkTransport("8:17", "/dev/sdb1"))
}

func Test_NetworkTransport_FibreChannelDevice(t *testing.T) {
	tree := newSysfsTree(t)
	tree.device("8:32", "devices/pci0000:00/0000:00:03.0/host3/target3:0:0/3:0:0:0/block/sdc")
	tree.hostClass("fc_host", "host3")

	assert.Equal(t, "Fibre Channel", NetworkTransport("8:32", "/dev/sdc"))
}

func Test_NetworkTransport_LocalSATADisk(t *testing.T) {
	tree := newSysfsTree(t)
	// A local disk has a hostN too; only the class directory tells them apart.
	tree.device("8:1", "devices/pci0000:00/0000:00:1f.2/ata1/host0/target0:0:0/0:0:0:0/block/sda/sda1")
	tree.hostClass("scsi_host", "host0")

	assert.Empty(t, NetworkTransport("8:1", "/dev/sda1"))
}

func Test_NetworkTransport_SASHostIsLocalFabric(t *testing.T) {
	tree := newSysfsTree(t)
	tree.device("8:48", "devices/pci0000:00/0000:00:02.0/host5/target5:0:0/5:0:0:0/block/sdd")
	tree.hostClass("sas_host", "host5")

	assert.Empty(t, NetworkTransport("8:48", "/dev/sdd"), "SAS is a local fabric, not network storage")
}

func Test_NetworkTransport_LocalPCIeNVMe(t *testing.T) {
	tree := newSysfsTree(t)
	tree.device("259:0", "devices/pci0000:00/0000:00:04.0/nvme/nvme0/nvme0n1")
	tree.nvmeController("nvme", "nvme0", "pcie")

	assert.Empty(t, NetworkTransport("259:0", "/dev/nvme0n1"))
}

func Test_NetworkTransport_NVMeWithoutTransportAttributeIsLocal(t *testing.T) {
	tree := newSysfsTree(t)
	// An older kernel, or a controller the class tree does not list: never
	// report network storage on a missing attribute.
	tree.device("259:0", "devices/pci0000:00/0000:00:04.0/nvme/nvme0/nvme0n1")

	assert.Empty(t, NetworkTransport("259:0", "/dev/nvme0n1"))
}

func Test_NetworkTransport_NVMeOverTCP(t *testing.T) {
	tree := newSysfsTree(t)
	// A fabrics namespace hangs off the virtual nvme-fabrics controller; the
	// controller's transport attribute is the only thing that says it is remote.
	tree.device("259:1", "devices/virtual/nvme-fabrics/ctl/nvme1/nvme1n1")
	tree.nvmeController("nvme", "nvme1", "tcp")

	assert.Equal(t, "NVMe/TCP", NetworkTransport("259:1", "/dev/nvme1n1"))
}

func Test_NetworkTransport_NVMeMultipathSubsystemNamesItsControllers(t *testing.T) {
	tree := newSysfsTree(t)
	// With native multipath the namespace sits under the subsystem, not under
	// any one controller; the controllers are the subsystem's nvmeN children.
	tree.device("259:2", "devices/virtual/nvme-subsystem/nvme-subsys0/nvme0n1")
	tree.nvmeController("nvme-subsystem", "nvme-subsys0/nvme0", "rdma")
	tree.nvmeController("nvme-subsystem", "nvme-subsys0/nvme1", "rdma")

	assert.Equal(t, "NVMe/RDMA", NetworkTransport("259:2", "/dev/nvme0n1"))
}

func Test_NetworkTransport_LVMOverISCSIWalksSlaves(t *testing.T) {
	tree := newSysfsTree(t)
	// LUN -> PV -> LV is the common enterprise layout; the LV's own sysfs path
	// is under devices/virtual and names no controller.
	lun := tree.device("8:17", "devices/platform/host7/session3/target7:0:0/7:0:0:1/block/sdb/sdb1")
	tree.hostClass("iscsi_host", "host7")
	dm := tree.device("253:0", "devices/virtual/block/dm-0")
	tree.slave(dm, "sdb1", lun)

	assert.Equal(t, "iSCSI", NetworkTransport("253:0", "/dev/mapper/vg-data"))
}

func Test_NetworkTransport_LVMOverLocalDiskStaysLocal(t *testing.T) {
	tree := newSysfsTree(t)
	disk := tree.device("8:1", "devices/pci0000:00/ata1/host0/target0:0:0/0:0:0:0/block/sda/sda1")
	tree.hostClass("scsi_host", "host0")
	dm := tree.device("253:0", "devices/virtual/block/dm-0")
	tree.slave(dm, "sda1", disk)

	assert.Empty(t, NetworkTransport("253:0", "/dev/mapper/vg-data"))
}

func Test_NetworkTransport_SlaveCycleTerminates(t *testing.T) {
	tree := newSysfsTree(t)
	a := tree.device("253:0", "devices/virtual/block/dm-0")
	b := tree.device("253:1", "devices/virtual/block/dm-1")
	tree.slave(a, "dm-1", b)
	tree.slave(b, "dm-0", a)

	assert.Empty(t, NetworkTransport("253:0", "/dev/mapper/vg-data"))
}

func Test_NetworkTransport_UnresolvableDeviceIsNotNetwork(t *testing.T) {
	newSysfsTree(t)

	// tmpfs, nfs and friends carry a synthetic maj:min and a source that is not
	// a device node; an unknown device must never be reported as network-backed.
	assert.Empty(t, NetworkTransport("0:46", "s3fs"))
	assert.Empty(t, NetworkTransport("", "tmpfs"))
	assert.Empty(t, NetworkTransport("8:17", "/dev/sdb1"), "no sysfs entry for this device")
}

func Test_NetworkTransport_RejectsNonDevIDLookups(t *testing.T) {
	tree := newSysfsTree(t)
	tree.device("8:17", "devices/platform/host7/session3/target7:0:0/7:0:0:1/block/sdb/sdb1")
	tree.hostClass("iscsi_host", "host7")

	// The maj:min comes from a kernel file, but it is still joined into a path.
	assert.Empty(t, NetworkTransport("../../class/iscsi_host", "/dev/sdb1"))
}

func Test_NetworkTransport_FallsBackToTheDeviceNodeForAnonymousDevIDs(t *testing.T) {
	tree := newSysfsTree(t)
	// btrfs reports an anonymous device number, so the mountinfo maj:min
	// resolves to nothing and only the source device node is left to go on.
	stubBlockDevice(t, "/dev/sdb1", 8, 17)
	tree.device("8:17", "devices/platform/host7/session3/target7:0:0/7:0:0:1/block/sdb/sdb1")
	tree.hostClass("iscsi_host", "host7")

	assert.Equal(t, "iSCSI", NetworkTransport("0:28", "/dev/sdb1"))
}

func Test_deviceNumberOf(t *testing.T) {
	stubBlockDevice(t, "/dev/fixture", 253, 7)
	assert.Equal(t, "253:7", deviceNumberOf("/dev/fixture"))

	assert.Empty(t, deviceNumberOf("/dev/null"), "a char device backs no filesystem")
	assert.Empty(t, deviceNumberOf("/etc/hostname"), "a regular file is not a device node")
	assert.Empty(t, deviceNumberOf("tank/data"), "a zfs pool name is not a path")
}
