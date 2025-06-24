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

package models

// CLI related constants
const (
	NmtProductNameCLI  = "cli"
	OtelServiceNameCLI = "nmt-cli"
	LogFileCLI         = "/opt/hgcapp/solo-provisioner/logs/nmt-cli.log"
)

// ICS related constants
const (
	NmtProductNameICS  = "ics"
	OtelServiceNameICS = "nmt-ics"
	LogFileICS         = "/opt/hgcapp/solo-provisioner/logs/nmt-ics.log"
	LogFileICSMetrics  = "/opt/hgcapp/solo-provisioner/logs/nmt-ics-metrics.log"
)

const (
	StateFileName    = "tool-state.yaml"
	MaxStateFileSize = 1e6 // bytes
)

const (
	ImageProfileMain = "main"

	ImageProfileJRS = "jrs"
)

// Default config flags
const (
	DefaultJVMVersion   = "17.0.2"
	DefaultJVMMinMem    = "32g"
	DefaultJVMMaxMem    = "150g"
	DefaultImageProfile = ImageProfileMain

	// image
	DefaultImageId = "main"
)
