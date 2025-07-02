/*
 * Copyright 2016-2023 Hedera Hashgraph, LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License";
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

const LogNameSpaceNotel = "notel"

// logFields defines various default log field key names
var logFields = struct {
	otelConfig    string
	collectURL    string
	setupErrMsg   string
	retryInterval string
	retryCount    string
	serviceName   string
}{
	otelConfig:    "otel_config",
	collectURL:    "collector_url",
	setupErrMsg:   "setup_error",
	retryInterval: "retry_interval",
	retryCount:    "retry_count",
	serviceName:   "service_name",
}
