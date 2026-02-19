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

// DefaultHostProfile implements HostProfile using both sysinfo and ghw libraries
type DefaultHostProfile struct {
	sysInfo sysinfo.SysInfo
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
	// Use ghw for CPU information
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
	// Use ghw for storage information
	block, err := ghw.Block()
	if err != nil {
		log.Printf("Error getting block info from ghw: %v", err)
		return 0
	}
	return uint64(block.TotalPhysicalBytes / (1024 * 1024 * 1024))
}

// GetSSDStorageGB returns total SSD/NVMe storage in GB
func (d *DefaultHostProfile) GetSSDStorageGB() uint64 {
	block, err := ghw.Block()
	if err != nil {
		log.Printf("Error getting block info from ghw: %v", err)
		return 0
	}

	var ssdBytes uint64
	for _, disk := range block.Disks {
		// ghw identifies SSDs via DriveType
		if disk.DriveType == ghw.DriveTypeSSD {
			ssdBytes += disk.SizeBytes
		}
	}
	return ssdBytes / (1024 * 1024 * 1024)
}

// GetHDDStorageGB returns total HDD (spinning disk) storage in GB
func (d *DefaultHostProfile) GetHDDStorageGB() uint64 {
	block, err := ghw.Block()
	if err != nil {
		log.Printf("Error getting block info from ghw: %v", err)
		return 0
	}

	var hddBytes uint64
	for _, disk := range block.Disks {
		// ghw identifies HDDs via DriveType
		if disk.DriveType == ghw.DriveTypeHDD {
			hddBytes += disk.SizeBytes
		}
	}
	return hddBytes / (1024 * 1024 * 1024)
}

// GetAvailableMemoryGB returns available system memory in GB
func (d *DefaultHostProfile) GetAvailableMemoryGB() uint64 {
	// Use ghw for memory information
	memory, err := ghw.Memory()
	if err != nil {
		log.Printf("Error getting memory info from ghw: %v", err)
		return 0
	}

	// Return usable memory as available memory
	return uint64(memory.TotalUsableBytes / (1024 * 1024 * 1024))
}

// IsNodeAlreadyRunning checks if the node is already running by looking for a lock file
func (d *DefaultHostProfile) IsNodeAlreadyRunning() bool {
	// Hardcoded lock file path - adjust this to match your application's lock file location
	lockFilePath := "/var/run/solo-node.lock"

	// Check if the lock file exists
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
