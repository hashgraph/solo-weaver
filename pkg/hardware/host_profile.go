// SPDX-License-Identifier: Apache-2.0

package hardware

import (
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/jaypipes/ghw"
	"github.com/zcalusic/sysinfo"
)

var once sync.Once

func suppressGHWWarnings() {
	once.Do(func() {
		os.Setenv("GHW_DISABLE_WARNINGS", "1")
	})
}

// HostProfile provides an abstraction over system information gathering
// This interface allows for easier testing and separation of concerns
type HostProfile interface {
	// OS information
	GetOSVendor() string
	GetOSVersion() string

	// CPU information
	GetCPUCores() uint

	// Memory information (in GB)
	GetTotalMemoryGB() uint64
	GetAvailableMemoryGB() uint64

	// Storage information (in GB)
	GetTotalStorageGB() uint64
	GetSSDStorageGB() uint64 // NVMe/SSD storage
	GetHDDStorageGB() uint64 // Traditional spinning disk storage

	// Application status
	IsNodeAlreadyRunning() bool

	String() string
}

// cachedBlockInfo holds pre-computed storage values from a single ghw.Block() call
type cachedBlockInfo struct {
	totalGB uint64
	ssdGB   uint64
	hddGB   uint64
}

// DefaultHostProfile implements HostProfile using both sysinfo and ghw libraries
type DefaultHostProfile struct {
	sysInfo   sysinfo.SysInfo
	blockInfo *cachedBlockInfo
	blockOnce sync.Once
}

// GetHostProfile creates a new DefaultHostProfile by gathering system information
func GetHostProfile() HostProfile {
	// Suppress warnings before any ghw operations
	suppressGHWWarnings()

	var si sysinfo.SysInfo
	si.GetSysInfo()

	return &DefaultHostProfile{
		sysInfo: si,
	}
}

// getBlockInfo returns cached block storage info, fetching it once if needed
func (d *DefaultHostProfile) getBlockInfo() *cachedBlockInfo {
	d.blockOnce.Do(func() {
		block, err := ghw.Block()
		if err != nil {
			log.Printf("Error getting block info from ghw: %v", err)
			d.blockInfo = &cachedBlockInfo{}
			return
		}

		var ssdBytes, hddBytes uint64
		for _, disk := range block.Disks {
			switch disk.DriveType {
			case ghw.DriveTypeSSD:
				ssdBytes += disk.SizeBytes
			case ghw.DriveTypeHDD:
				hddBytes += disk.SizeBytes
			}
		}

		d.blockInfo = &cachedBlockInfo{
			totalGB: block.TotalPhysicalBytes / (1024 * 1024 * 1024),
			ssdGB:   ssdBytes / (1024 * 1024 * 1024),
			hddGB:   hddBytes / (1024 * 1024 * 1024),
		}
	})
	return d.blockInfo
}

// GetOSVendor returns the OS vendor/distribution name
func (d *DefaultHostProfile) GetOSVendor() string {
	return d.sysInfo.OS.Vendor
}

// GetOSVersion returns the OS version
func (d *DefaultHostProfile) GetOSVersion() string {
	return d.sysInfo.OS.Version
}

// GetCPUCores returns the number of CPU cores
func (d *DefaultHostProfile) GetCPUCores() uint {
	cpu, err := ghw.CPU()
	if err != nil {
		log.Printf("Error getting CPU info from ghw: %v", err)
		return 0
	}
	return uint(cpu.TotalCores)
}

// GetTotalMemoryGB returns total system memory in GB
func (d *DefaultHostProfile) GetTotalMemoryGB() uint64 {
	memory, err := ghw.Memory()
	if err != nil {
		log.Printf("Error getting memory info from ghw: %v", err)
		return 0
	}
	return uint64(memory.TotalPhysicalBytes / (1024 * 1024 * 1024))
}

// GetTotalStorageGB returns total storage space in GB
func (d *DefaultHostProfile) GetTotalStorageGB() uint64 {
	return d.getBlockInfo().totalGB
}

// GetSSDStorageGB returns total SSD/NVMe storage in GB
func (d *DefaultHostProfile) GetSSDStorageGB() uint64 {
	return d.getBlockInfo().ssdGB
}

// GetHDDStorageGB returns total HDD (spinning disk) storage in GB
func (d *DefaultHostProfile) GetHDDStorageGB() uint64 {
	return d.getBlockInfo().hddGB
}

// GetAvailableMemoryGB returns available system memory in GB
func (d *DefaultHostProfile) GetAvailableMemoryGB() uint64 {
	memory, err := ghw.Memory()
	if err != nil {
		log.Printf("Error getting memory info from ghw: %v", err)
		return 0
	}
	return uint64(memory.TotalUsableBytes / (1024 * 1024 * 1024))
}

// IsNodeAlreadyRunning checks if the node is already running by looking for a lock file
func (d *DefaultHostProfile) IsNodeAlreadyRunning() bool {
	lockFilePath := "/var/run/solo-node.lock"

	if _, err := os.Stat(lockFilePath); os.IsNotExist(err) {
		return false
	}

	// TODO: Could add additional validation here:
	// - Check if the PID in the lock file is still running
	// - Validate lock file format
	// - Check file age to detect stale locks

	return true
}

func (d *DefaultHostProfile) String() string {
	return fmt.Sprintf("OS: %s %s, CPU: %d cores, Memory: %d/%d GB (available/total), Storage: %d GB",
		d.GetOSVendor(), d.GetOSVersion(), d.GetCPUCores(),
		d.GetAvailableMemoryGB(), d.GetTotalMemoryGB(),
		d.GetTotalStorageGB())
}
