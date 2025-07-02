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
	"fmt"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/pkg/software/specs"
	"testing"
)

func TestNewDefinitionManager(t *testing.T) {
	req := require.New(t)
	expected := &definitionsManager{}
	expected.loadAll()

	dm, err := NewDefinitionManager()
	req.NoError(err)
	req.Equal(expected, dm)

	for key, _ := range definitionMapping {
		a, e := expected.GetDefinition(key)
		req.NoError(e)

		b, e := dm.GetDefinition(key)
		req.NoError(e)
		req.Equal(a, b)
	}

	req.True(dm.HasDefinition(DockerCE))
	req.False(dm.HasDefinition("INVALID"))

	_, e := dm.GetDefinition("INVALID")
	req.Error(e)
}

func TestNewDefinitionManager_With_JSON_Parse_Failure(t *testing.T) {
	req := require.New(t)
	tmp := dockerSoftwareManifest
	defer func() {
		definitionMapping[DockerCE] = tmp
	}()

	definitionMapping[DockerCE] = "INVALID"

	dm, err := NewDefinitionManager()
	req.Error(err)
	req.Contains(err.Error(), fmt.Sprintf("failed to parse software definition for %q", DockerCE))
	req.Nil(dm)
}

func TestDefinitionManager_LoadAll(t *testing.T) {
	req := require.New(t)

	// create a deep copy of definitionMapping to reset the definitionMapping
	tmpList := map[specs.SoftwareName]string{}
	for key, val := range definitionMapping {
		tmpList[key] = val
	}
	defer func() { definitionMapping = tmpList }()

	for key, _ := range tmpList {
		// reset specJSON
		for k, v := range tmpList {
			definitionMapping[k] = v
		}

		// make one element incorrect
		definitionMapping[key] = "INVALID"

		// try to load and fail
		dm := definitionsManager{}
		err := dm.loadAll()
		req.Error(err)
		req.Contains(err.Error(), fmt.Sprintf("failed to parse software definition for %q", key))
	}
}
