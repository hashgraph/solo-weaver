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
	"crypto/sha256"
	"fmt"
	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"
	"golang.hedera.com/solo-provisioner/pkg/software/specs"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// NewProgramDetector returns an instance of ProgramDetector
// This returns unixProgramDetector that works for linux and darwin
func NewProgramDetector(logger *zerolog.Logger) ProgramDetector {
	return NewUnixProgramDetector(logger)
}

// unixProgramDetector implements the ProgramDetector interface for unix.
// This also works for darwin.
type unixProgramDetector struct {
	logger *zerolog.Logger
}

func (ud *unixProgramDetector) SetLogger(logger *zerolog.Logger) {
	if logger != nil {
		ud.logger = logger
	}
}

func (ud *unixProgramDetector) DetectExecutablePath(name specs.SoftwareName) (string, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	cmd := exec.Command(shell, "-c", fmt.Sprintf("command -v %s", name.String()))
	output, err := cmd.Output()
	if err != nil {
		return "", errors.Wrapf(err, "error on running command -v for the program at %q", name.String())
	}

	programPath := strings.Trim(string(output), "\n")
	return programPath, nil
}

func (ud *unixProgramDetector) ComputeProgramHash(path string) ([32]byte, error) {
	hash := [32]byte{}

	b, err := os.ReadFile(path)
	if err != nil {
		return hash, errors.Wrapf(err, "failed to compute sha256 of the program at %q", path)
	}

	hash = sha256.Sum256(b)
	return hash, nil
}

func (ud *unixProgramDetector) DetectProgramVersion(path string, versionArgs specs.VersionDetectionSpec) (version string, err error) {
	args := strings.Split(versionArgs.Arguments, " ")
	cmd := exec.Command(path, args...)
	verStr, err := cmd.Output()
	if err != nil {
		return "", errors.Wrapf(err, "failed to get program version info args: %q", versionArgs.Arguments)
	}

	reg, err := regexp.Compile(versionArgs.Regex)
	if err != nil {
		return "", errors.Wrapf(err, "failed to parse version regex: %q", versionArgs.Regex)
	}

	version = reg.FindString(string(verStr))

	return version, nil
}

func (ud *unixProgramDetector) GetProgramInfo(ctx context.Context, execInfo specs.SoftwareExecutableSpec) (ProgramInfo, error) {
	var err error
	var statInfo os.FileInfo
	var path string

	ud.logger.Debug().
		Str(logFields.name, execInfo.Name.String()).
		Msg("Scan Software: Checking Software State")

	if execInfo.DefaultLocation == "" {
		// attempt path resolution if default location was not present
		path, err = ud.DetectExecutablePath(execInfo.Name)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to find path to the program %q", execInfo.Name)
		}
	} else {
		// try to get info of the executable at the default location
		path = execInfo.DefaultLocation
		statInfo, err = os.Stat(path)
		if err != nil {
			// attempt path resolution if default location was not accessible
			path, err = ud.DetectExecutablePath(execInfo.Name)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to find path to the program %q or access at %q", execInfo.Name, path)
			}
		}
	}

	// get info of the executable at the path
	if statInfo == nil {
		statInfo, err = os.Stat(path)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to access the program at %q", path)
		}
	}

	ud.logger.Debug().
		Str(logFields.name, execInfo.Name.String()).
		Str(logFields.path, path).
		Msg("Software State: Located Potential Executable")

	// obtain actual hash of executable
	hash, err := ud.ComputeProgramHash(path)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to calculate hash of the program at %q", path)
	}

	ud.logger.Debug().
		Str(logFields.name, execInfo.Name.String()).
		Str(logFields.path, path).
		Str(logFields.hash, fmt.Sprintf("%x", hash)).
		Msg("Software State: Computed Executable Hash")

	// get version of the executable
	version, err := ud.DetectProgramVersion(path, execInfo.VersionInfo)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to determine version of the program at %q", path)
	}

	ud.logger.Debug().
		Str(logFields.name, execInfo.Name.String()).
		Str(logFields.path, path).
		Str(logFields.version, version).
		Msg("Software State: Detected Program Version")

	info := &programInfo{
		path:       path,
		mode:       statInfo.Mode(),
		version:    version,
		sha256Hash: fmt.Sprintf("%x", hash),
	}

	ud.logger.Debug().
		Str(logFields.name, execInfo.Name.String()).
		Str(logFields.path, info.GetPath()).
		Str(logFields.hash, info.GetHash()).
		Str(logFields.version, info.GetVersion()).
		Msg("Software State: Identified program details")

	return info, nil
}

func NewUnixProgramDetector(logger *zerolog.Logger) ProgramDetector {
	if logger == nil {
		logger = &nolog
	}

	ud := &unixProgramDetector{
		logger: logger,
	}
	return ud
}
