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

package models

import "golang.hedera.com/solo-provisioner/pkg/events"

// Daemon defines the settings for various NMT daemons
type Daemon struct {
	Watch Watch
}

// Watch defines the configs for nmt-ics that watch for filesystem events
type Watch struct {
	Paths []WatchPath
}

// WatchPath defines paths that nmt-ics should monitor
type WatchPath struct {
	Path    string
	Events  []events.WatchEvent
	Filter  string
	Execute Execute
}

// Execute defines the config of the programs that nmt-ics should execute upon receiving certain file system events
type Execute struct {
	Command   string
	Arguments []string
}
