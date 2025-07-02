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
	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"
	"math"
)

// memoryManger implements MemoryManager interface
type memoryManager struct {
	detector MemoryDetector
	logger   *zerolog.Logger
}

// MemoryManagerOption allows setting various option for memoryManager
type MemoryManagerOption = func(mm *memoryManager)

// WithSystemMemoryDetector allows injecting a MemoryDetector instance
func WithSystemMemoryDetector(detector MemoryDetector) MemoryManagerOption {
	return func(mm *memoryManager) {
		if detector != nil {
			mm.detector = detector
		}
	}
}

// WithMemoryManagerLogger allows injecting a logger instance for memory manager
func WithMemoryManagerLogger(logger *zerolog.Logger) MemoryManagerOption {
	return func(mm *memoryManager) {
		if logger != nil {
			mm.logger = logger
		}
	}
}

// NewMemoryManager returns an instance of MemoryManager
func NewMemoryManager(opts ...MemoryManagerOption) MemoryManager {
	mm := &memoryManager{
		detector: &systemMemoryDetector{},
		logger:   &nolog,
	}

	for _, opt := range opts {
		opt(mm)
	}

	return mm
}

// GetSystemMemory returns total and free system memory rounded to 4 valid numbers (eg. "2.746GB", "796KB").
// It rounds the memory into a human-readable approximation of the total bytes such as 2.746GB.
// We are rounding it because in practice we shall be dealing with Gb of memory rather than bytes.
func (mm *memoryManager) GetSystemMemory() (SystemMemoryInfo, error) {
	var err error
	var sm SystemMemoryInfo

	sm.TotalBytes, err = mm.detector.TotalMemory()
	if err != nil {
		return sm, err
	}

	sm.FreeBytes, err = mm.detector.FreeMemory()
	if err != nil {
		return sm, err
	}

	mm.logger.Info().
		Str(logFields.totalMemory, sm.TotalStr()).
		Str(logFields.freeMemory, sm.FreeStr()).
		Msg("Memory Check: System Memory Size Detected")

	return sm, nil
}

// CheckJavaMemoryPair parse min and max size and verifies that the format is correct
func (mm *memoryManager) CheckJavaMemoryPair(minSize string, maxSize string) (minBytes uint64,
	maxBytes uint64, err error) {

	minSizeB, err := ParseMemorySizeInBytes(minSize)
	if err != nil {
		err = errors.Wrapf(err, "failed to parse minSize %q", minSize)
		return
	}

	mm.logger.Debug().
		Str(logFields.minMemory, minSize).
		Uint64(logFields.minMemoryBytes, minSizeB).
		Msg("Memory Check: Parsed Minimum Memory Size Requirement")

	maxSizeB, err := ParseMemorySizeInBytes(maxSize)
	if err != nil {
		err = errors.Wrapf(err, "failed to parse maxSize %q", maxSize)
		return
	}

	mm.logger.Debug().
		Str(logFields.maxMemory, maxSize).
		Uint64(logFields.maxMemoryBytes, maxSizeB).
		Msg("Memory Check: Parsed Maximum Memory Size Requirement")

	if minSizeB > maxSizeB {
		err = errors.Newf("illegal minimum & maximum memory allocation request (minimum > maximum) "+
			"[ minimum = %q, maximum = %q ]", minSize, maxSize)
		return
	}

	// set the return variables
	minBytes = minSizeB
	maxBytes = maxSizeB

	err = nil

	mm.logger.Info().
		Str(logFields.minMemory, minSize).
		Uint64(logFields.minMemoryBytes, minBytes).
		Str(logFields.maxMemory, maxSize).
		Uint64(logFields.maxMemoryBytes, maxBytes).
		Msg("Memory Check: Successfully Parsed Memory Size Requirements")

	return
}

// HasTotalMemory checks if the system has the requested amount of total physical memory
func (mm *memoryManager) HasTotalMemory(reqBytes uint64) error {
	systemMemory, err := mm.GetSystemMemory()
	if err != nil {
		return errors.Wrap(err, "failed to retrieve system memory")
	}

	reqSize := HumanizeBytes(reqBytes)
	actualTotal := mm.deductReserve(systemMemory.TotalBytes, systemMemory)
	actualTotalStr := HumanizeBytes(actualTotal)
	if reqBytes <= actualTotal {
		mm.logger.Debug().
			Str(logFields.reqMemory, reqSize).
			Str(logFields.totalMemory, systemMemory.TotalStr()).
			Str(logFields.totalAfterReserve, actualTotalStr).
			Msg("Memory Check: Verified Required Memory Allocations Based on Total Physical Memory")
		return nil
	}

	return errors.Newf("required memory allocation of %q "+
		"exceeds currently available total physical memory of %q(with reserve %q)",
		reqSize, systemMemory.TotalStr(), actualTotalStr)
}

// HasFreeMemory checks if the system has the required amount of free physical memory
func (mm *memoryManager) HasFreeMemory(reqBytes uint64) error {
	systemMemory, err := mm.GetSystemMemory()
	if err != nil {
		return errors.Wrap(err, "failed to retrieve system memory information")
	}

	reqSize := HumanizeBytes(reqBytes)
	actualFree := mm.deductReserve(systemMemory.FreeBytes, systemMemory)
	actualFreeStr := HumanizeBytes(actualFree)
	if reqBytes <= actualFree {
		mm.logger.Debug().
			Str(logFields.reqMemory, reqSize).
			Str(logFields.totalMemory, systemMemory.TotalStr()).
			Str(logFields.freeMemory, systemMemory.FreeStr()).
			Str(logFields.freeAfterReserve, actualFreeStr).
			Msg("Memory Check: Verified Required Memory Allocations Based on Currently Available System Memory")
		return nil
	}

	return errors.Newf("required memory allocation of %q "+
		"exceeds currently available system memory of %q(with reserve %q)",
		reqSize, systemMemory.TotalStr(), actualFreeStr)
}

// deductReserve returns the size after deducting the reserved fraction
func (mm *memoryManager) deductReserve(memSize uint64, sm SystemMemoryInfo) uint64 {
	var reserveFrac float64
	smallSizeStr := HumanizeBytes(smallSystemMaxMemSize)

	if sm.TotalBytes <= smallSystemMaxMemSize {
		reserveFrac = smallSystemMemReserve
		mm.logger.Debug().
			Float64(logFields.reserveFrac, reserveFrac).
			Str(logFields.totalMemory, sm.TotalStr()).
			Str(logFields.smallSystemSizeLimit, smallSizeStr).
			Msg("Memory Check: Selected Small Reserve Memory Fraction (Total < Small System Mem Size Limit)")
	} else {
		reserveFrac = largeSystemMemReserve
		mm.logger.Debug().
			Float64(logFields.reserveFrac, reserveFrac).
			Str(logFields.totalMemory, sm.TotalStr()).
			Str(logFields.smallSystemSizeLimit, smallSizeStr).
			Msg("Memory Check: Selected Large Reserve Memory Fraction (Total > Small System Mem Size Limit)")
	}

	// calculate and convert reserve size into uint64
	reserveSize := uint64(math.Round(float64(memSize) * reserveFrac))

	return memSize - reserveSize
}
