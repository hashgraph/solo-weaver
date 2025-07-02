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

import (
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
)

// Option sets the option for Otl
type Option func(ntl *Otl)

// WithServiceName sets the service name for Otl instance
func WithServiceName(serviceName string) Option {
	return func(ntl *Otl) {
		ntl.serviceName = serviceName
	}
}

// WithOTelConfig sets the OTel nodeConfig for Otl instance
func WithOTelConfig(config OTel) Option {
	return func(ntl *Otl) {
		ntl.otelConfig = config
	}
}

// WithLogger sets the root logger for Otl
func WithLogger(logger *zerolog.Logger) Option {
	return func(ntl *Otl) {
		if logger != nil {
			ntl.logger = logger
		}
	}
}

// WithSpanAttributes sets the common span attributes
//
// WARN: Do not set default span attributes for nodes such as AttrNodeId and AttrNodeAlias since those will be set from
// the otelConfig
func WithSpanAttributes(attrs []attribute.KeyValue) Option {
	return func(ntl *Otl) {
		for _, attr := range attrs {
			ntl.defaultSpanAttrs.add(attr)
		}
	}
}

// New returns an instance of Otl
// By default it returns a NoOp Otl instance if Options are not provided
func New(opts ...Option) *Otl {
	ntl := NoOpNotel()

	for _, opt := range opts {
		opt(ntl)
	}

	return ntl
}
