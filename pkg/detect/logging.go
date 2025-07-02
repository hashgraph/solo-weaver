/*
 * Copyright 2016-2023 Hedera Hashgraph, LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License";
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

import "github.com/rs/zerolog"

var nolog = zerolog.Nop()

const LogNameSpaceDetect = "detect"

var logFields = struct {
	minMemory            string
	minMemoryBytes       string
	maxMemory            string
	maxMemoryBytes       string
	totalMemory          string
	totalMemoryBytes     string
	freeMemory           string
	totalAfterReserve    string
	freeMemoryBytes      string
	systemMemory         string
	freeAfterReserve     string
	osType               string
	osVersion            string
	osCodename           string
	osFlavor             string
	osArch               string
	reserveFrac          string
	smallSystemSizeLimit string
	reqMemory            string
}{
	minMemory:            "min_memory",
	maxMemory:            "max_memory",
	totalMemory:          "total_memory",
	freeMemory:           "free_memory",
	minMemoryBytes:       "min_memory_bytes",
	maxMemoryBytes:       "max_memory_bytes",
	totalMemoryBytes:     "total_memory_bytes",
	freeMemoryBytes:      "free_memory_bytes",
	systemMemory:         "system_memory",
	totalAfterReserve:    "actual_total",
	freeAfterReserve:     "actual_free",
	osType:               "os_type",
	osVersion:            "os_version",
	osCodename:           "os_codename",
	osFlavor:             "os_flavor",
	osArch:               "os_architecture",
	reserveFrac:          "reserve_frac",
	smallSystemSizeLimit: "small_system_size_limit",
	reqMemory:            "req_memory",
}
