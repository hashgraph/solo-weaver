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

import (
	"github.com/rs/zerolog"
	"golang.hedera.com/solo-provisioner/internal/otl"
	"golang.hedera.com/solo-provisioner/pkg/events"
)

// MockICSConfig returns a mock NMT ICS config for unit tests
func MockICSConfig(tmpDir string) *ICSConfig {
	config := ICSConfig{
		Daemon: Daemon{
			Watch: Watch{Paths: []WatchPath{
				{
					Path:   tmpDir,
					Events: []events.WatchEvent{events.Create},
					Filter: "*.mf",
					Execute: Execute{
						Command:   "ps",
						Arguments: []string{},
					},
				},
			}},
		},
		Otel: otl.OTel{
			Enable: false,
			Trace: otl.OTelTraceConfig{
				LogLevel: zerolog.DebugLevel.String(),
			},
			Collector: otl.OTelCollectorConfig{
				Endpoint:      "",
				RetryInterval: "0s",
				TLS: otl.OTelTLSConfig{
					Insecure: true,
				},
			},
		},
		Log: Log{
			MaxAge:  90,  // Days
			MaxSize: 100, // MB
		},
	}

	return &config
}
