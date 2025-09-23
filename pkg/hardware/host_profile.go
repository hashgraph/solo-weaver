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
	// Use ghw for more accurate CPU information
	cpu, err := ghw.CPU()
	if err != nil {
		log.Printf("Error getting CPU info from ghw: %v, falling back to sysinfo", err)
		// Fallback to sysinfo
		if d.sysInfo.CPU.Cpus == 0 {
			return d.sysInfo.CPU.Threads
		}
		return d.sysInfo.CPU.Cpus
	}
	return uint(cpu.TotalCores)
}

// GetTotalMemoryGB returns total system memory in GB
func (d *DefaultHostProfile) GetTotalMemoryGB() uint64 {
	// Use ghw for more accurate memory information
	memory, err := ghw.Memory()
	if err != nil {
		log.Printf("Error getting memory info from ghw: %v, falling back to sysinfo", err)
		// Fallback to sysinfo (convert KB to GB)
		return uint64(d.sysInfo.Memory.Size) / (1024 * 1024)
	}
	return uint64(memory.TotalPhysicalBytes / (1024 * 1024 * 1024))
}

// GetTotalStorageGB returns total storage space in GB
func (d *DefaultHostProfile) GetTotalStorageGB() uint64 {
	// Use ghw for more accurate storage information
	block, err := ghw.Block()
	if err != nil {
		log.Printf("Error getting block info from ghw: %v, falling back to sysinfo", err)
		// Fallback to sysinfo
		if len(d.sysInfo.Storage) > 0 {
			return uint64(d.sysInfo.Storage[0].Size)
		}
		return 0
	}
	return uint64(block.TotalPhysicalBytes / (1024 * 1024 * 1024))
}

// GetAvailableMemoryGB returns available system memory in GB
func (d *DefaultHostProfile) GetAvailableMemoryGB() uint64 {
	// Use ghw for more accurate memory information
	memory, err := ghw.Memory()
	if err != nil {
		log.Printf("Error getting memory info from ghw: %v, falling back to sysinfo", err)
		// Fallback to sysinfo - estimate available as ~80% of total
		totalGB := uint64(d.sysInfo.Memory.Size) / (1024 * 1024)
		return uint64(float64(totalGB) * 0.8)
	}

	// Calculate available memory (total - used)
	totalGB := uint64(memory.TotalPhysicalBytes / (1024 * 1024 * 1024))
	usedGB := uint64(memory.TotalUsableBytes / (1024 * 1024 * 1024))
	if totalGB > usedGB {
		return totalGB - usedGB
	}
	// Fallback: estimate available as ~80% of total
	return uint64(float64(totalGB) * 0.8)
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
