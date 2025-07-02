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
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/internal/models"
	"golang.hedera.com/solo-provisioner/pkg/paths"
	"testing"
)

func TestGetPathVerificationList(t *testing.T) {
	req := require.New(t)
	nmtPaths := paths.MockNmtPaths("../../tmp")
	var err error

	base, err := unmarshalPathList(systemPathListBase)
	req.NoError(err)
	req.NotZero(len(base))

	extendedMain, err := unmarshalPathList(systemPathListMain)
	req.NoError(err)
	req.NotZero(len(extendedMain))

	extendedJRS, err := unmarshalPathList(systemPathListJRS)
	req.NoError(err)
	req.NotZero(len(extendedJRS))

	extendedJRSAR, err := unmarshalPathList(systemPathListJRSAR)
	req.NoError(err)
	req.NotZero(len(extendedJRSAR))

	// check for main
	expected := append(base, extendedMain...)
	for i, pv := range expected {
		pv.Path, err = parseTemplatedPaths(pv.Path, &nmtPaths)
		req.NoError(err)
		expected[i] = pv
	}

	list, err := GetPathVerificationList(nmtPaths, core.ImageIDs.ImageMain)
	req.NoError(err)
	req.Equal(expected, list)

	// check for jrs
	expected = append(base, extendedJRS...)
	expected, err = parsePathVerificationList(expected, &nmtPaths)
	req.NoError(err)

	list, err = GetPathVerificationList(nmtPaths, core.ImageIDs.ImageJRS)
	req.NoError(err)
	req.Equal(expected, list)

	// check for jrs-ar
	expected = append(base, extendedJRSAR...)
	expected, err = parsePathVerificationList(expected, &nmtPaths)
	req.NoError(err)
	list, err = GetPathVerificationList(nmtPaths, core.ImageIDs.ImageJRSAR)
	req.NoError(err)
	req.Equal(expected, list)
}
