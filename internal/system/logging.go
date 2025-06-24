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

package common

const LogNameSpaceCommon = "common"

var logFields = struct {
	program    string
	version    string
	path       string
	pathType   string
	targetPath string
	userID     string
	userName   string
	groupID    string
	groupName  string
	permission string
	state      string
	exists     string
	errMsg     string
}{
	program:    "program",
	version:    "version",
	path:       "path",
	pathType:   "path_type",
	targetPath: "target_path",
	permission: "perms",
	userName:   "user",
	userID:     "user_id",
	groupName:  "group",
	groupID:    "group_id",
	state:      "state",
	exists:     "exists",
	errMsg:     "error_msg",
}
