/*
 * Copyright 2016-2023 Hedera Hashgraph, LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package detect

import (
	"github.com/docker/go-units"
)

// SystemMemoryInfo describes total and free physical memory in the system
type SystemMemoryInfo struct {
	TotalBytes uint64 `yaml:"total_bytes" json:"total_bytes"`
	FreeBytes  uint64 `yaml:"free_bytes" json:"free_bytes"`
}

// TotalStr returns the total physical memory as a human-readable approximation of the memory size
// capped at 4 valid numbers (e.g. "2.746 MB", "796 KB").
func (smi *SystemMemoryInfo) TotalStr() string {
	return units.HumanSize(float64(smi.TotalBytes))
}

// FreeStr returns the free physical memory as a human-readable approximation of the memory size
// capped at 4 valid numbers (e.g. "2.746 MB", "796 KB").
func (smi *SystemMemoryInfo) FreeStr() string {
	return units.HumanSize(float64(smi.FreeBytes))
}

// MemoryDetector provides interface to detect system memory
type MemoryDetector interface {
	// TotalMemory returns total system memory in bytes
	TotalMemory() (uint64, error)

	// FreeMemory returns total free memory in bytes
	FreeMemory() (uint64, error)
}

// MemoryManager defines various memory related functionalities
type MemoryManager interface {
	// CheckJavaMemoryPair parse min and max size and verifies that the format is correct
	CheckJavaMemoryPair(minSize string, maxSize string) (minBytes uint64, maxBytes uint64, err error)

	// GetSystemMemory returns total and free system memory in bytes
	GetSystemMemory() (SystemMemoryInfo, error)

	// HasTotalMemory checks if the system has the required amount of total physical memory
	HasTotalMemory(reqBytes uint64) error

	// HasFreeMemory checks if the system has the required amount of free physical memory
	HasFreeMemory(reqBytes uint64) error
}

// OSInfo defines the data model to contain OS related information
type OSInfo struct {
	Type         string
	Version      string
	Flavor       string
	CodeName     string
	Architecture string
}

// OSManager defines various OS related functionalities
type OSManager interface {
	// GetOSInfo returns OS related information
	GetOSInfo() (*OSInfo, error)
}

// OSDetector provides interface to detect OS related details
type OSDetector interface {
	ScanOS() (*OSInfo, error)
}
