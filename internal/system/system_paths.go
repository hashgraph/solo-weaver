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
 *
 *
 *
 */

package common

import (
	_ "embed"
	"github.com/cockroachdb/errors"
	"golang.hedera.com/solo-provisioner/internal/models"
	"golang.hedera.com/solo-provisioner/pkg/paths"
	"gopkg.in/yaml.v3"
)

//go:embed system_paths.yaml
var systemPathListBase []byte

//go:embed system_paths_main.yaml
var systemPathListMain []byte

//go:embed system_paths_jrs.yaml
var systemPathListJRS []byte

//go:embed system_paths_jrs-ar.yaml
var systemPathListJRSAR []byte

var systemPathsMapByImageID = map[string][]byte{
	core.ImageIDs.ImageMain:  systemPathListMain,
	core.ImageIDs.ImageJRS:   systemPathListJRS,
	core.ImageIDs.ImageJRSAR: systemPathListJRSAR,
}

func unmarshalPathList(payload []byte) ([]PathVerification, error) {
	var paths []PathVerification
	err := yaml.Unmarshal(payload, &paths)
	if err != nil {
		return nil, err
	}

	return paths, nil
}

func parsePathVerificationList(paths []PathVerification, nmtPaths *paths.Paths) ([]PathVerification, error) {
	var parsedPath string
	var err error

	for i, p := range paths {
		parsedPath, err = parseTemplatedPaths(p.Path, nmtPaths)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to parse path %q", p.Path)
		}

		paths[i].Path = parsedPath
	}

	return paths, nil
}

// GetPathVerificationList returns a list of required folder and file paths
func GetPathVerificationList(nmtPaths paths.Paths, imageID string) ([]PathVerification, error) {
	paths, err := unmarshalPathList(systemPathListBase)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load system path list")
	}

	if b, ok := systemPathsMapByImageID[imageID]; ok {
		var extraPaths []PathVerification
		extraPaths, err = unmarshalPathList(b)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to load system path list for image ID: %s", imageID)
		}

		paths = append(paths, extraPaths...)
	}

	paths, err = parsePathVerificationList(paths, &nmtPaths)
	if err != nil {
		return nil, err
	}

	return paths, nil
}
