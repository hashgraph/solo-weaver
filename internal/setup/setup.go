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

import "context"

type Manager interface {
	// CreateStagingArea creates a temporary install directory for the setup process to use.  Call Cleanup() before
	// exiting using the same manager instance to remove the installation working directory.
	CreateStagingArea() error
	// ExtractSDKArchive extracts the SDK archive to the temporary install directory.
	// Requires CreateStagingArea() to have been called.  Requires a context and requires an
	// SDK archive to be specified.  Supported archive formats are *.tar, *.tar.gz and *.zip.
	ExtractSDKArchive(ctx context.Context, sdkArchive string) error
	// PrepareDocker prepares working directories for Docker build.  Requires CreateStagingArea() to have been called.
	// Requires image ID which is the name of the Docker image to build.
	PrepareDocker(imageID string) error
	// StageSDK stages the SDK for installation.  Requires CreateStagingArea() to have been called.
	// Requires PrepareDocker() to have been called.  Requires the SDK package file to be specified.
	StageSDK(sdkPackageFile string) error
	// GetDockerNodeImageName returns the name of the Docker image to build.  Requires PrepareDocker() to have been called.
	GetDockerNodeImageName() string
	// Cleanup removes the temporary install directory unless NMT_RETAIN_TEMP is set to true.
	Cleanup() error
	// GetInstallWorkingDirectory returns the path to the temporary install directory, or an empty string if
	// CreateStagingArea() has not been called.
	GetInstallWorkingDirectory() string
}
