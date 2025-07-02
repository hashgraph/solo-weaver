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
	"github.com/pbnjay/memory"
)

// systemMemoryDetector is an OS independent memory detector
// Currently this is just a wrapper for "github.com/pbnjay/memory" that we can replace if we need to implement our own
type systemMemoryDetector struct {
}

// TotalMemory returns total memory in bytes
// If accessible memory size could not be determined, then 0 is returned.
func (smd *systemMemoryDetector) TotalMemory() (uint64, error) {
	return memory.TotalMemory(), nil
}

// FreeMemory returns free memory in bytes
// If accessible memory size could not be determined, then 0 is returned.
func (smd *systemMemoryDetector) FreeMemory() (uint64, error) {
	return memory.FreeMemory(), nil
}
