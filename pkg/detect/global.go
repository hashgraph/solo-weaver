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
	"math"
	"strings"
)

// HumanizeBytes returns the total memory as a human-readable approximation of the memory size
// capped at 4 valid numbers (e.g. "2.746 MB", "796 KB").
func HumanizeBytes(size uint64) string {
	return units.HumanSize(float64(size))
}

// HumanizeBytesAsJavaSpec returns the total memory as a human-readable approximation of the memory size
// as used in Java spec.
// It is capped at 4 valid numbers (e.g. "2.746 MB", "796 KB").
// It supports upto ExaBytes as limited by uint64 data type. However, we don't expect a node to have exabyte level
// memory yet. So this limitation is ok.
func HumanizeBytesAsJavaSpec(size uint64) string {
	return units.CustomSize("%.4g%s", float64(size), 1000, javaMemoryUnits)
}

// ParseMemorySizeInBytes parses memory size string into a bytes.
//
// It accepts unit specifiers such as: g/gb/G/GB, m/mb/M/MB, k/kb/K/KB, t/tb/T/TB, or p/pb/P/PB that is supported by
// https://github.com/docker/go-units.
func ParseMemorySizeInBytes(memSize string) (uint64, error) {
	s := strings.ToLower(strings.TrimSpace(memSize))
	byteSize, err := units.FromHumanSize(s)
	if err != nil {
		return 0, err
	}

	return uint64(byteSize), err
}

// AddBuffer adds the buffer to the memory size.
func AddBuffer(size uint64) uint64 {
	buffer := math.Round(float64(size) * defaultMemBufferPercent)
	bufferedSize := float64(size) + buffer
	return uint64(bufferedSize)
}
