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
	"context"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// notelGlobal is the global tracer that can be accessed using the package level methods
var notelGlobal *Otl

func init() {
	notelGlobal = NoOpNotel()
}

// NoOpNotel returns a NoOp Otl instance
// This can be helpful to set up NoOp Otl during tests
func NoOpNotel() *Otl {
	nl := zerolog.Nop()
	ntl := &Otl{
		otelConfig:       OTel{},
		serviceName:      "notel-noop",
		logger:           &nl,
		defaultSpanAttrs: newAttributeCache(AttrSpanIgnore.Bool(false)),
	}

	ntl.setupNoOp()

	return ntl
}

// Meter returns the NMT meter instance
func Meter() metric.Meter {
	return notelGlobal.rootMeter
}

// Start initializes global otel providers
// This should only be called from the main function
func Start(ctx context.Context, opts ...Option) {
	notelGlobal.Shutdown()
	notelGlobal = New(opts...)

	if notelGlobal.otelConfig.Enable == false {
		notelGlobal.setupNoOp()
		notelGlobal.logger.Warn().
			Any(logFields.otelConfig, notelGlobal.otelConfig).
			Msg("OpenTelemetry is disabled")

		return
	}

	notelGlobal.Start(ctx)
}

// Shutdown closes global open telemetry providers
// This should only be called from the main function
func Shutdown() {
	notelGlobal.Shutdown()
}

// StartParentSpan starts a parent span with the logger
//
// This is used by various coroutines that trigger chains of async actions based on events, for example, nmt-watch
// starts a parent span every time a file system event is triggered
func StartParentSpan(logger *zerolog.Logger) (context.Context, trace.Span) {
	return notelGlobal.StartParentSpan()
}

// StartSpan starts a span with the given context and logger
// If context has a span already, it will create a new child span.
// It returns a context with new span and also initialize the logger with the context
func StartSpan(ctx context.Context) (context.Context, trace.Span) {
	return notelGlobal.StartSpan(ctx)
}

// StartSpanWithName starts a span with the given context, logger and
// name.  If context has a span already, it will create a new child span.
// It returns a context with new span and also initialize the logger with the context
func StartSpanWithName(ctx context.Context, name string) (context.Context, trace.Span) {
	return notelGlobal.StartSpanWithName(ctx, name)
}

// EndSpan ends the span and performs any necessary clean up actions
func EndSpan(ctx context.Context, span trace.Span) {
	notelGlobal.EndSpan(span)
}

// IsActivated returns true if it has been able to connect to OTel collector
func IsActivated() bool {
	return notelGlobal.IsActivated()
}
