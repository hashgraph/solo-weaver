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

package otl

// OTel defines the OTel configuration
type OTel struct {
	Enable    bool
	Collector OTelCollectorConfig
	Trace     OTelTraceConfig
}

type OTelTraceConfig struct {
	LogLevel string `yaml:"log_level" mapstructure:"log_level" json:"log_level"`
}

type OTelCollectorConfig struct {
	Endpoint      string
	RetryInterval string `yaml:"retry_interval" mapstructure:"retry_interval" json:"retry_interval"`
	TLS           OTelTLSConfig
}

type OTelTLSConfig struct {
	Insecure   bool
	CaFile     string `yaml:"ca_file" mapstructure:"ca_file" json:"ca_file"`
	CertFile   string `yaml:"cert_file" mapstructure:"cert_file" json:"cert_file"`
	KeyFile    string `yaml:"key_file" mapstructure:"key_file" json:"key_file"`
	MinVersion string `yaml:"min_version" mapstructure:"min_version" json:"min_version"`
	MaxVersion string `yaml:"max_version" mapstructure:"max_version" json:"max_version"`
}
