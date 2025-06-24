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

package paths

// HederaAppDir reflects the directory tree of HederaApp in the file system
// Usually it is located at "/opt/hgcapp"
//
// A sample of very high-level tree structure is as below for reference.
// hgcapp
// ├── hedera-backups
// ├── solo-provisioner
// │   ├── bin
// │   ├── common
// │   │   ├── backup
// │   │   ├── cli
// │   │   ├── detect
// │   │   ├── docker
// │   │   ├── incron
// │   │   ├── jvm
// │   │   ├── keys
// │   │   ├── logging
// │   │   ├── platform
// │   │   ├── plock
// │   │   ├── preflight
// │   │   ├── security
// │   │   ├── setup
// │   │   ├── software
// │   │   └── upgrade
// │   ├── compose
// │   │   └── network-node
// │   ├── config
// │   │   ├── docker
// │   │   │   └── compose
// │   │   │       └── network-node
// │   │   ├── keys
// │   │   ├── manifest
// │   │   └── software
// │   ├── images
// │   │   ├── jrs-ar-network-node
// │   │   ├── jrs-network-node
// │   │   ├── main-network-node
// │   │   ├── network-node-base
// │   │   │   └── checksums
// │   │   └── network-node-haveged
// │   │       └── checksums
// │   ├── logs
// │   ├── state
// │   └── upgrade
// │       ├── current
// │       ├── pending
// │       └── previous
// ├── services-hedera
// │   ├── HapiApp2.0
// │   │   ├── data
// │   │   │   ├── config
// │   │   │   ├── diskFs
// │   │   │   ├── keys
// │   │   │   ├── onboard
// │   │   │   ├── saved
// │   │   │   ├── stats
// │   │   │   └── upgrade
// │   │   │       ├── current
// │   │   │       ├── pending
// │   │   │       └── previous
// │   │   └── output
// └── uploader-mirror
type HederaAppDir struct {
	Root           string
	NodeMgmtTools  *NodeMgmtToolsDir
	HederaServices *HederaServicesDir
	UploaderMirror string
	HederaBackups  string
}

// NodeMgmtToolsDir reflects the directory tree of node-mgmt-tool directory
type NodeMgmtToolsDir struct {
	Root    string
	Bin     string
	Common  string
	Compose *ComposeDir
	Config  string
	Image   string
	Logs    string
	State   string
	Upgrade *UpgradeDir
}

// HederaServicesDir reflects the directory tree of services-hedera dir
type HederaServicesDir struct {
	Root    string
	HapiApp *HapiAppDir
}

// HapiAppDir reflects the directory structure of HapiApp dir
type HapiAppDir struct {
	Root string
	Data *HapiAppDataDir
}

// HapiAppDataDir reflects the directory structure of HapiApp.data dir
type HapiAppDataDir struct {
	Root    string
	Config  string
	DiskFs  string
	Keys    string
	OnBoard string
	Saved   string
	Stats   string
	Upgrade *UpgradeDir
}

// UpgradeDir reflects the directory structure of upgrade dir
// It is shared in few locations such as NmtMgmtDir and HapiDataDir
type UpgradeDir struct {
	Root     string
	Current  string
	Pending  string
	Previous string
}

type ComposeDir struct {
	Root        string
	NetworkNode string
}
