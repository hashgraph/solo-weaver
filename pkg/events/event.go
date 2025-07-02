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
	"strconv"
	"strings"
	"time"
)

// Event defines the events emitted by the NMT tool
//
// Payload may be non-nil and its type depends on the EventType and should be handled accordingly with cast error
// handling.
//
// Error may be non-nil. This needs to be handled based on EventType.
type Event struct {
	Type      EventType
	Payload   []byte
	Error     error
	CreatedAt time.Time
}

type EventType int

const (
	FsNotifyEventProcessingStart        EventType = 0x01
	FsNotifyEventProcessingEnd          EventType = 0x02
	FsNotifySymlinkEventProcessingStart EventType = 0x03
	FsNotifySymlinkEventProcessingEnd   EventType = 0x04
)

func (ev *Event) String() string {
	return strings.Join([]string{
		strconv.Itoa(int(ev.Type)),
		ev.CreatedAt.Format(time.RFC3339),
	}, ":")
}
