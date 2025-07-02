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

package common

import (
	"golang.hedera.com/solo-provisioner/internal/models"
)

type Manager interface {
	// LoadToolState loads the tool state from a state file
	LoadToolState() (*models.ToolState, error)

	// WriteToolState writes the tool state to a state file
	WriteToolState(state *models.ToolState) error

	// GetStateFilePath returns the path to the state file
	GetStateFilePath() string
}

// IntegrityChecker checks for integrity of the tool and environment
type IntegrityChecker interface {
	// CheckEnvironmentIntegrity ensures the bare minimum of NMT system software dependencies are present.
	// It checks for various system software such as GNU linux-utils, jq, sha256sum, unzip, tar, gunzip, curl, etc.
	CheckEnvironmentIntegrity() error

	// CheckToolIntegrity checks if integrity of NMT folders and files
	CheckToolIntegrity() error
}
