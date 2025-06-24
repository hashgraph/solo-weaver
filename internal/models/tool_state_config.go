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

package models

// ToolState represents the data model to store attributes that are required by NMT.
// ToolState works like a file based local persistence store between NMT commands such as preflight and upgrade.
type ToolState struct {
	NodeID string `yaml:"node_id" json:"node_id"`

	AppVersion string `yaml:"app_version" json:"app_version"`

	ImageID string `yaml:"image_id" json:"image_id"`

	JavaHeapMin string `yaml:"java_heap_min" json:"java_heap_min"`

	JavaHeapMax string `yaml:"java_heap_max" json:"java_heap_max"`

	JavaVersion string `yaml:"java_version" json:"java_version"`
}
