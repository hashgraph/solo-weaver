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

package plock

import (
	"strconv"
	"strings"
	"time"
)

// Info defines the data model to describe a plock
// Primary purpose of this is to have a serializable data model of the plock
type Info struct {
	ProviderType string
	Name         string
	PID          int
	WorkDir      string
	LockFileName string
	PidFileName  string
	LockFilePath string
	PidFilePath  string
	ActivatedAt  *time.Time
}

// String returns string representation of the Info
// The representation is formatted to be self-descriptive with format as below:
// {providerType}:{name}:{PID}:{ActivatedAt}:{lockFilePath}
func (pli *Info) String() string {
	activatedAt := "-"
	if pli.ActivatedAt != nil {
		activatedAt = pli.ActivatedAt.Format(time.RFC3339)
	}

	return strings.Join([]string{
		pli.ProviderType,
		pli.Name,
		strconv.Itoa(pli.PID),
		activatedAt,
		pli.LockFilePath,
	}, IdentifierSeparator)
}
