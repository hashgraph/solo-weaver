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

import (
	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/redact"
	"path/filepath"
)

type FolderLocationResolver func() (string, error)

// ResolveFolder resolves the given folder name from the root path resolved by the rootResolver
// This can also be used to ensure the folder exists by creating the folder at the desired location.
// For example, in order to find config folder in the root location can be ensured by calling:
//   - ResolveFolder(rootResolver, "config", true)
//
// It throws error if the folder doesn't exist and if it is not meant to be created
func ResolveFolder(rootResolver FolderLocationResolver, name string, createIfNotFound bool) (string, error) {
	if rootResolver == nil {
		return "", errors.New("the root resolver cannot be nil")
	}

	rootFolder, err := rootResolver()

	if err != nil {
		return "", errors.Wrap(err, err.Error())
	}

	folder := filepath.Join(rootFolder, name)

	if !FolderExists(folder) {
		if createIfNotFound {
			err = makeDir(folder)
			if err != nil {
				return "", errors.Wrapf(err, "%s folder does not exist and attempts to create the path failed: %s",
					redact.Safe(name), redact.Safe(folder))
			}
		} else {
			return "", errors.Errorf("%s folder does not exist: %s", redact.Safe(name), redact.Safe(folder))
		}
	}

	return folder, nil
}
