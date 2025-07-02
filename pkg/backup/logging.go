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
 *
 *
 *
 */

package backup

const LogNameSpaceBackup = "backup"
const LogMessagePrefix = "backup"

var logFields = struct {
	srcPath        string
	symlinkPath    string
	backupDirName  string
	backupRootPath string
	backupPath     string
	resolvedPath   string
	relativePath   string
	timestamp      string
	softwareFolder string
	toolsFolder    string
	snapshotName   string
	folder         string
	destPath       string
	link           string
	target         string
}{
	srcPath:        "src_path",
	symlinkPath:    "symlink_path",
	backupDirName:  "backup_dir_name",
	backupRootPath: "backup_dir",
	backupPath:     "backup_path",
	resolvedPath:   "resolved_path",
	relativePath:   "relative_path",
	timestamp:      "timestamp",
	softwareFolder: "software_folder",
	toolsFolder:    "tools_folder",
	snapshotName:   "snapshot_name",
	folder:         "folder",
	destPath:       "dest_path",
	link:           "link",
	target:         "target",
}
