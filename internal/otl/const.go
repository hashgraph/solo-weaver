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
 */

package otl

import "go.opentelemetry.io/otel/attribute"

const (
	DefaultOtelServiceName = "nmt"
	NmtTracerName          = "nmt-otel-tracer"
	NmtMeterName           = "nmt-otel-meter"
)

// attributes
const (
	AttrSpanIgnore = attribute.Key("span.ignore")
	AttrNodeId     = attribute.Key("node.id")
	AttrNodeAlias  = attribute.Key("node.alias")
	AttrNmtProduct = attribute.Key("nmt.product")
)
