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

package events

import (
	"encoding/json"
	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/require"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestEvent_JSON(t *testing.T) {
	req := require.New(t)
	mockTime, err := time.Parse(time.RFC3339, "2021-01-01T00:00:05Z")
	req.NoError(err)

	fsEv := fsnotify.Event{
		Name: ".tmp",
		Op:   fsnotify.Create,
	}
	payloadBytes, err := json.Marshal(fsEv)
	nev := Event{
		Type:      FsNotifyEventProcessingStart,
		Payload:   payloadBytes,
		Error:     nil,
		CreatedAt: mockTime,
	}

	bytes, err := json.Marshal(nev)
	req.NoError(err)
	req.NotNil(bytes)

	var nev2 Event
	err = json.Unmarshal(bytes, &nev2)
	req.NoError(err)
	req.Equal(nev, nev2)
}

func TestEvent_String(t *testing.T) {
	req := require.New(t)

	mockTime, err := time.Parse(time.RFC3339, "2021-01-01T00:00:05Z")
	req.NoError(err)

	fsEv := fsnotify.Event{
		Name: ".tmp",
		Op:   fsnotify.Create,
	}
	payloadBytes, err := json.Marshal(fsEv)
	nev := Event{
		Type:      FsNotifyEventProcessingStart,
		Payload:   payloadBytes,
		Error:     nil,
		CreatedAt: mockTime,
	}

	str := strings.Join([]string{
		strconv.Itoa(int(nev.Type)),
		nev.CreatedAt.Format(time.RFC3339),
	}, ":")

	req.Equal(str, nev.String())
}
