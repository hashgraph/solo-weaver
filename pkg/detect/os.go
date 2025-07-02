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

package detect

import (
	"github.com/rs/zerolog"
)

// osManager implements OSManager interface
type osManager struct {
	logger   *zerolog.Logger
	detector OSDetector
}

// OSManagerOption allows setting various option for osManager
type OSManagerOption = func(om *osManager)

// WithOSManagerLogger allows injecting a logger instance for OS manager
func WithOSManagerLogger(logger *zerolog.Logger) OSManagerOption {
	return func(om *osManager) {
		if logger != nil {
			om.logger = logger
		}
	}
}

// WithOSDetector allows injecting an OSDetector instance for OS manager
func WithOSDetector(detector OSDetector) OSManagerOption {
	return func(om *osManager) {
		if detector != nil {
			om.detector = detector
		}
	}
}

// NewOSManager returns an instance of OSManager
func NewOSManager(opts ...OSManagerOption) OSManager {
	om := &osManager{
		logger:   &nolog,
		detector: NewOSDetector(),
	}

	for _, opt := range opts {
		opt(om)
	}

	return om
}

func (om *osManager) GetOSInfo() (*OSInfo, error) {
	info, err := om.detector.ScanOS()
	if err != nil {
		om.logger.Error().Err(err).Msg("Failed To Detect Operating System")
		return info, err
	}

	om.logger.Info().
		Str(logFields.osType, info.Type).
		Str(logFields.osVersion, info.Version).
		Str(logFields.osVersion, info.Version).
		Str(logFields.osFlavor, info.Flavor).
		Str(logFields.osArch, info.Architecture).
		Str(logFields.osCodename, info.CodeName).
		Msg("Detected Operating System")

	return info, nil
}
