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

import (
	"context"
	"github.com/rs/zerolog"
	"golang.hedera.com/solo-provisioner/pkg/security/principal"
	"golang.hedera.com/solo-provisioner/pkg/software/specs"
	"os"
)

// Manager exposes various methods to manage software dependencies
type Manager interface {
	// IsMandatory returns true if the software is mandatory
	IsMandatory(name specs.SoftwareName) bool

	// IsAvailable checks if the software is available on the node or not
	IsAvailable(name specs.SoftwareName) bool

	// CheckState checks if the installed software meets the requirements specified in the software definition file.
	// It checks version as well as hash integrity if the software definition forces the relevant checks.
	CheckState(ctx context.Context, def specs.SoftwareDefinition) error

	// ScanDependencies scans the software dependencies.
	// It checks installed software state using CheckState method which in turn checks for version and hash integrity.
	ScanDependencies(ctx context.Context, defs []specs.SoftwareDefinition) error

	// Exec executes the software with the given arguments.
	// It invokes ExecAsUser using the user specified by security.ServiceAccountUserName.
	// On execution of the program it returns the output as []byte and error (if error exists).
	Exec(ctx context.Context, name specs.SoftwareName, args ...string) ([]byte, error)

	// ExecAsUser executes the software with the given arguments and user.
	// On execution of the program it returns the output as []byte and error (if error exists).
	ExecAsUser(ctx context.Context, name specs.SoftwareName, user principal.User, args ...string) ([]byte, error)
}

// DefinitionManager exposes function to retrieve various software specs
type DefinitionManager interface {
	// GetDefinition returns a software definition for the software executable defined by the name.
	GetDefinition(name specs.SoftwareName) (specs.SoftwareDefinition, error)

	// HasDefinition returns true if a software definition exists.
	HasDefinition(name specs.SoftwareName) bool

	// GetAll returns a list of available specs.SoftwareDefinition.
	GetAll() []specs.SoftwareDefinition
}

// ProgramInfo exposes executable details about a software program
type ProgramInfo interface {
	// GetPath returns the full path to the executable.
	GetPath() string

	// GetFileMode returns the file mode of the program.
	GetFileMode() os.FileMode

	// IsExecAll returns true if it is executable by all (owner, user and group).
	IsExecAll() bool

	// IsExecAny returns true if it is executable by owner or user or group.
	IsExecAny() bool

	// IsExecOwner returns true if it is executable by owner.
	IsExecOwner() bool

	// IsExecGroup returns true if it is executable by group.
	IsExecGroup() bool

	// GetVersion returns the program version.
	GetVersion() string

	// GetHash returns the sha256 hash of the program executable.
	GetHash() string
}

// ProgramDetector returns details about the installed software if it exists
type ProgramDetector interface {
	// DetectExecutablePath detects the full executable path for a given program name
	DetectExecutablePath(name specs.SoftwareName) (string, error)

	// ComputeProgramHash computes the hash of the program at the given path
	ComputeProgramHash(path string) ([32]byte, error)

	// DetectProgramVersion detects the version of the program
	DetectProgramVersion(path string, versionArgs specs.VersionDetectionSpec) (version string, err error)

	// GetProgramInfo returns ProgramInfo given a software executable name.
	GetProgramInfo(ctx context.Context, execInfo specs.SoftwareExecutableSpec) (ProgramInfo, error)

	// SetLogger allows injecting a logger
	SetLogger(logger *zerolog.Logger)
}
