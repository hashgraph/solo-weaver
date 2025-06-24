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

import (
	"golang.hedera.com/solo-provisioner/internal/otl"
)

// ICSConfig defines the data model for NMT ICS configuration
type ICSConfig struct {
	Daemon Daemon   `yaml:"daemon" mapstructure:"daemon" json:"daemon"`
	Otel   otl.OTel `yaml:"otel" mapstructure:"otel" json:"otel"`
	Log    Log      `yaml:"log" mapstructure:"log" json:"log"`
}
