/*
 * Copyright 2016-2022 Hedera Hashgraph, LLC
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

// DefaultDirMode is the mode that is used if a new directory is created
const DefaultDirMode = 0755

// HederaApp directory related constants
const (
	HederaAppDirName      = "hgcapp"
	NodeMgmtDirName       = "solo-provisioner"
	UploaderMirrorDirName = "uploader-mirror"
	HederaBackupsDirName  = "hedera-backups"
	ConfigDirName         = "config"
	LogsDirName           = "logs"
	HederaServicesDirName = "services-hedera"
	HederaApiDirName      = "HapiApp2.0"
	DataDirName           = "data"
	UpgradeDirName        = "upgrade"
	ComposeDirName        = "compose"
)

const DefaultMaxDepth = -1 // -1 denotes there is no limit

// DirectoryNames defines various directory names for path discovery
// Scout uses this in path discovery
type DirectoryNames struct {
	ConfigDirName      string
	LogDirName         string
	HgcAppDirName      string
	NodeMgmtDirName    string
	SkippedFolderNames []string
}

// defaultSkippedFolderNames lists the default folder that are skipped during app root path discovery
// For example, we assume the work directory to be "bin" or "local", so the app directory is parent of such directory and
// therefore the "bin" is trimmed from the work directory path (e.g "/a/b/c/bin") to retrieve the app root (e.g. "/a/b/c")
var defaultSkippedFolderNames = []string{"bin", "local"}
