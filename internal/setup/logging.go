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

package setup

const LogNameSpaceSetup = "setup"

// logFields defines various default log field key names
var logFields = struct {
	logPrefix                  string
	workingDirectory           string
	sdkPackageFile             string
	extractedSDKPath           string
	dockerImageDestinationPath string
	imageName                  string
	sourceFolder               string
	imageWorkingFolder         string
	errorCode                  string
	imageSourcePath            string
	networkNodePath            string
	networkNodeSDKDataPath     string
	extractedSDKDataPath       string
	requiredDataFolderPath     string
}{
	logPrefix:                  "log_prefix",
	workingDirectory:           "working_directory",
	sdkPackageFile:             "sdk_package_file",
	extractedSDKPath:           "sdk_destination_path",
	dockerImageDestinationPath: "docker_image_destination_path",
	imageName:                  "image_name",
	sourceFolder:               "source_folder",
	imageWorkingFolder:         "image_working_folder",
	errorCode:                  "error_code",
	imageSourcePath:            "image_source_path",
	networkNodePath:            "network_node_path",
	networkNodeSDKDataPath:     "network_node_sdk_data_path",
	extractedSDKDataPath:       "extracted_sdk_data_path",
	requiredDataFolderPath:     "required_data_folder_path",
}
