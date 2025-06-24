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

package software

import "golang.hedera.com/solo-provisioner/pkg/software/specs"

// name of software
const (
	DockerCE specs.SoftwareName = "docker"
)

// list of OSType
const (
	DefaultOSType specs.OSType = "linux"
	Linux         specs.OSType = "linux"
	Darwin        specs.OSType = "darwin"
)

// list of OS flavor
const (
	DefaultOSFlavor  specs.OSFlavor = "_default"
	DarwinSierra     specs.OSFlavor = "sierra"
	DarwinHighSierra specs.OSFlavor = "high_sierra"
	DarwinMojave     specs.OSFlavor = "mojave"
	DarwinCatalina   specs.OSFlavor = "catalina"
	DarwinBigSur     specs.OSFlavor = "big_sur"
)

// list of OS versions
const (
	DefaultOSVersion specs.OSVersion = "_default"
	Ubuntu16         specs.OSVersion = "16.04"
	Ubuntu18         specs.OSVersion = "18.04"
	Ubuntu20         specs.OSVersion = "20.04"
	Rhel7            specs.OSVersion = "7"
	Rhel8            specs.OSVersion = "8"
	Centos7          specs.OSVersion = "7"
	Centos8          specs.OSVersion = "8"
)
