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

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"github.com/joomcode/errorx"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/global"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"os"
	"time"
)

// Otl defines the struct to manage OTel tracing
type Otl struct {
	rootCtx                context.Context // this context.Background and initialized in the init function
	rootTracer             trace.Tracer
	rootMeter              metric.Meter
	shutdownTracerProvider func(ctx context.Context)
	shutdownMeterProvider  func(ctx context.Context)
	otelConfig             OTel
	retryCount             int
	serviceName            string
	logger                 *zerolog.Logger
	defaultSpanAttrs       *attributeCache
	activated              bool
}

// setupNoOp sets up a NoOp tracer
func (o *Otl) setupNoOp() {
	o.rootCtx = context.Background()
	o.shutdownTracerProvider = nil
	o.shutdownMeterProvider = nil
	o.otelConfig = OTel{}

	// by default set NoOp tracer provider
	// in setup() a proper provider is initialized if valid otelConfig is passed
	otel.SetTracerProvider(trace.NewNoopTracerProvider())
	o.rootTracer = otel.Tracer(NmtTracerName)

	// setup noop meter provider
	// in setup() a proper provider is initialized if valid otelConfig is passed
	global.SetMeterProvider(metric.NewNoopMeterProvider())
	o.rootMeter = global.Meter(NmtMeterName)
}

// parseTlsConfig parses the TLS configurations loads the required certs and keys
func (o *Otl) parseTlsConfig() (credentials.TransportCredentials, error) {
	var tlsCred credentials.TransportCredentials
	var clientCert tls.Certificate

	// load root CA cert
	serverCerts := x509.NewCertPool()
	b, err := os.ReadFile(o.otelConfig.Collector.TLS.CaFile)
	if err != nil {
		return nil, errorx.IllegalArgument.
			New("credentials: failed to load CA certificate %q", o.otelConfig.Collector.TLS.CaFile).
			WithUnderlyingErrors(err)
	}

	if !serverCerts.AppendCertsFromPEM(b) {
		return nil, errorx.IllegalArgument.
			New("credentials: failed to append CA certificate PEM %q", o.otelConfig.Collector.TLS.CaFile).
			WithUnderlyingErrors(err)
	}

	if o.otelConfig.Collector.TLS.CertFile != "" {
		// setup mTLS if client cert file is specified
		clientCert, err = tls.LoadX509KeyPair(o.otelConfig.Collector.TLS.CertFile, o.otelConfig.Collector.TLS.KeyFile)
		if err != nil {
			return nil, errorx.IllegalArgument.
				New("credentials: failed to load client certificate PEM %q",
					o.otelConfig.Collector.TLS.CertFile).WithUnderlyingErrors(err)
		}

		// setup mTLS credentials
		tlsCred = credentials.NewTLS(&tls.Config{
			RootCAs:      serverCerts,
			Certificates: []tls.Certificate{clientCert},
		})
	} else {
		// just setup server TLS credentials
		tlsCred = credentials.NewTLS(&tls.Config{
			RootCAs: serverCerts,
		})
	}

	return tlsCred, nil
}

// setupTracerGrpcClientOption sets up tracer grpc client options based on otelConfig
func (o *Otl) setupTracerGrpcClientOption() ([]otlptracegrpc.Option, error) {
	clientOptions := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(o.otelConfig.Collector.Endpoint),
		otlptracegrpc.WithDialOption(grpc.WithBlock()),
		otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig{}),
	}

	if o.otelConfig.Collector.TLS.Insecure == true {
		clientOptions = append(clientOptions, otlptracegrpc.WithInsecure())
	} else {
		tlsCred, err := o.parseTlsConfig()
		if err != nil {
			return nil, err
		}

		// include tls credentials
		clientOptions = append(clientOptions, otlptracegrpc.WithTLSCredentials(tlsCred))
	}

	return clientOptions, nil
}

// setupMeterGrpcClientOption sets up grpc meter client options based on otelConfig
func (o *Otl) setupMetricGrpcClientOption() ([]otlpmetricgrpc.Option, error) {
	clientOptions := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(o.otelConfig.Collector.Endpoint),
		otlpmetricgrpc.WithDialOption(grpc.WithBlock()),
		otlpmetricgrpc.WithRetry(otlpmetricgrpc.RetryConfig{}),
	}

	if o.otelConfig.Collector.TLS.Insecure == true {
		clientOptions = append(clientOptions, otlpmetricgrpc.WithInsecure())
	} else {
		tlsCred, err := o.parseTlsConfig()
		if err != nil {
			return nil, err
		}

		// include tls credentials
		clientOptions = append(clientOptions, otlpmetricgrpc.WithTLSCredentials(tlsCred))
	}

	return clientOptions, nil
}

// setupTracerProvider sets up the tracer provider based on otelConfig
func (o *Otl) setupTracerProvider(res *resource.Resource) error {
	ctx, cancel := context.WithTimeout(o.rootCtx, time.Second)
	defer cancel()

	clientOptions, err := o.setupTracerGrpcClientOption()
	if err != nil {
		return errorx.IllegalArgument.New("Failed to setup tracer GRPC client options").WithUnderlyingErrors(err)
	}

	traceClient := otlptracegrpc.NewClient(clientOptions...)
	traceExporter, err := otlptrace.New(ctx, traceClient)
	if err != nil {
		return errorx.IllegalArgument.New("Failed to setup trace exporter").WithUnderlyingErrors(err)
	}

	spanProcessor := sdktrace.NewBatchSpanProcessor(traceExporter)
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(spanProcessor),
	)

	// set global propagator to trace context (the default is no-op).
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	otel.SetTracerProvider(tracerProvider)
	o.rootTracer = otel.Tracer(NmtTracerName)

	// prepare shutdown func
	o.shutdownTracerProvider = func(ctx context.Context) {
		err = spanProcessor.ForceFlush(ctx)
		if err != nil {
			otel.Handle(err)
		}

		cx, cf := context.WithTimeout(ctx, time.Second)
		defer cf()

		err = traceExporter.Shutdown(cx)
		if err != nil {
			otel.Handle(err)
		}
	}

	return nil
}

// setupMeterProvider sets up the meter provider based on otelConfig
func (o *Otl) setupMeterProvider(res *resource.Resource) error {
	ctx, cancel := context.WithTimeout(o.rootCtx, time.Second)
	defer cancel()

	clientOptions, err := o.setupMetricGrpcClientOption()
	if err != nil {
		return errorx.IllegalArgument.New("Failed to setup metric GRPC client options").WithUnderlyingErrors(err)
	}

	metricExp, err := otlpmetricgrpc.New(
		ctx,
		clientOptions...,
	)

	if err != nil {
		return errorx.IllegalArgument.New("Failed to setup metric exporter").WithUnderlyingErrors(err)
	}

	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(
				metricExp,
				sdkmetric.WithInterval(2*time.Second),
			),
		),
	)

	global.SetMeterProvider(meterProvider)
	o.rootMeter = global.Meter(NmtMeterName)

	// prepare shutdown func
	o.shutdownTracerProvider = func(ctx context.Context) {
		err = meterProvider.Shutdown(ctx)
		if err != nil {
			otel.Handle(err)
		}
	}

	return nil
}

// setup initializes Open Telemetry providers
func (o *Otl) setup() error {
	// setup resource
	res, err := resource.New(o.rootCtx,
		resource.WithHost(),
		resource.WithProcess(),
		resource.WithFromEnv(),
		resource.WithOS(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			// the service name used to display traces in backends
			semconv.ServiceNameKey.String(o.serviceName),
		),
	)
	if err != nil {
		return errorx.IllegalArgument.New("Failed to create the collector resource").WithUnderlyingErrors(err)
	}

	// setup trace provider
	err = o.setupTracerProvider(res)
	if err != nil {
		return errorx.IllegalArgument.New("Failed to setup tracer provider").WithUnderlyingErrors(err)
	}

	// setup meter provider
	err = o.setupMeterProvider(res)
	if err != nil {
		return errorx.IllegalArgument.New("Failed to setup meter provider").WithUnderlyingErrors(err)
	}

	eventBus.Publish(TopicConnected, o.otelConfig.Collector.Endpoint)

	return nil
}

// setDefaultSpanAttributes sets default attributes for the span
func (o *Otl) setDefaultSpanAttributes(span trace.Span) {
	span.SetAttributes(o.defaultSpanAttrs.getAll()...)
}

// retryDelay converts the collector retry interval otelConfig as time.Duration
// If the otelConfig is incorrect, it uses a default 15 secs retry interval
func (o *Otl) retryDelay() time.Duration {
	delay, err := time.ParseDuration(o.otelConfig.Collector.RetryInterval)
	if err != nil {
		delay = time.Second * 15
	}

	return delay
}

// Start initializes Open Telemetry providers
func (o *Otl) Start(ctx context.Context) {
	otelConfig := o.otelConfig
	if otelConfig.Enable == false {
		o.setupNoOp()
		o.logger.Warn().
			Any(logFields.otelConfig, o.otelConfig).
			Str(logFields.serviceName, o.serviceName).
			Msg("OpenTelemetry is disabled")
		return
	}

	// schedule retry and connection monitoring
	go func() {
		o.RetryCollector(ctx, o.retryDelay())

		// after RetryCollector is successful, start monitoring the connection and update activated field accordingly
		o.MonitorConnection(ctx, o.retryDelay())
	}()

	// listen to topic connected events
	err := Subscribe(TopicConnected, func(endpoint string) {
		if o.activated == false {
			// if it is reconnection, log an info message
			o.logger.Info().
				Any(logFields.collectURL, endpoint).
				Msg("Reconnected to OTel collector")
		}

		o.activated = true
	})

	if err != nil {
		o.logger.Warn().
			Err(err).
			Msg("Failed to subscribe to OTel collector connected event")
		return
	}

	// listen to topic disconnected events
	err = Subscribe(TopicDisconnected, func(endpoint string) {
		o.logger.Warn().
			Any(logFields.collectURL, endpoint).
			Msg("Disconnected from OTel collector. Ensure OTel collector is running.")

		o.activated = false
	})

	if err != nil {
		o.logger.Warn().
			Err(err).
			Msg("Failed to subscribe to OTel collector disconnected event")
		return
	}
}

// Shutdown closes telemetry providers
func (o *Otl) Shutdown() {
	if o.shutdownTracerProvider != nil {
		o.shutdownTracerProvider(o.rootCtx)
	}

	if o.shutdownMeterProvider != nil {
		o.shutdownMeterProvider(o.rootCtx)
	}
}

// StartRootSpan starts a parent span with the caller function name and logger
func (o *Otl) StartRootSpan(ctx context.Context) (context.Context, trace.Span) {
	name := getCallerFuncName()
	c, s := o.rootTracer.Start(ctx, name)
	return c, s
}

// StartParentSpan starts a parent span with the caller function name and logger
// It doesn't take any new context, so it won't create span under any other span
func (o *Otl) StartParentSpan() (context.Context, trace.Span) {
	name := getCallerFuncName()
	ctxSpan, span := o.rootTracer.Start(o.rootCtx, name)
	o.setDefaultSpanAttributes(span)
	return ctxSpan, span
}

// StartSpan starts a span with the given context and logger
// If context has a span already, it will create a new child span.
// It returns a context with new span and also initialize the logger with the context
func (o *Otl) StartSpan(ctx context.Context) (context.Context, trace.Span) {
	name := getCallerFuncName()
	return o.StartSpanWithName(ctx, name)
}

// StartSpanWithName starts a span with the given context, logger and
// name.  If context has a span already, it will create a new child span.
// It returns a context with new span and also initialize the logger with the context
func (o *Otl) StartSpanWithName(ctx context.Context, name string) (context.Context, trace.Span) {
	ctxSpan, span := o.rootTracer.Start(ctx, name)
	o.setDefaultSpanAttributes(span)

	return ctxSpan, span
}

// EndSpan ends the span and performs any necessary clean up actions
func (o *Otl) EndSpan(span trace.Span) {
	if span != nil {
		span.End()
	}
}

// RetryCollector starts a coroutine to attempt to connect to collector and set up the Otl instance based on otelConfig
func (o *Otl) RetryCollector(ctx context.Context, retryDelay time.Duration) {
	// prepare for retry loop
	collectorURL := o.otelConfig.Collector.Endpoint
	ticker := time.NewTicker(retryDelay)

	scheduleRetries := true // we expect first attempt to fail, so by default we expect to schedule retry attempts

	// Attempt to connect immediately without any delay.
	// This may fail; but that's ok, and we'll schedule retry attempts
	err := o.setup()
	if err != nil {
		o.logger.Warn().
			Str(logFields.serviceName, o.serviceName).
			Int(logFields.retryCount, o.retryCount).
			Float64(logFields.retryInterval, retryDelay.Seconds()).
			Any(logFields.otelConfig, o.otelConfig).
			Str(logFields.setupErrMsg, err.Error()).
			Msg("Failed to setup OpenTelemetry. Re-attempt is scheduled. Ensure OTel collector is running.")
	} else {
		scheduleRetries = false // avoid scheduling unnecessary retry attempts
		o.logger.Info().
			Str(logFields.serviceName, o.serviceName).
			Int(logFields.retryCount, o.retryCount).
			Any(logFields.otelConfig, o.otelConfig).
			Msg("OpenTelemetry setup successful")
	}

	if scheduleRetries == true {
		o.retryCount = 0

		for {
			select {
			case <-ticker.C:
				o.retryCount += 1

				o.logger.Debug().
					Str(logFields.serviceName, o.serviceName).
					Int(logFields.retryCount, o.retryCount).
					Float64(logFields.retryInterval, retryDelay.Seconds()).
					Str(logFields.collectURL, collectorURL).
					Any(logFields.otelConfig, o.otelConfig).
					Msg("Attempting to setup OpenTelemetry")

				err = o.setup()

				if err != nil {
					o.logger.Warn().
						Str(logFields.serviceName, o.serviceName).
						Int(logFields.retryCount, o.retryCount).
						Float64(logFields.retryInterval, retryDelay.Seconds()).
						Any(logFields.otelConfig, o.otelConfig).
						Str(logFields.setupErrMsg, err.Error()).
						Msg("Failed to setup OpenTelemetry. Re-attempt is scheduled. Ensure OTel collector is running.")

					continue
				}

				o.logger.Info().
					Str(logFields.serviceName, o.serviceName).
					Int(logFields.retryCount, o.retryCount).
					Any(logFields.otelConfig, o.otelConfig).
					Msg("OpenTelemetry setup successful")

				return

			case <-ctx.Done():
				o.logger.Debug().
					Str(logFields.serviceName, o.serviceName).
					Str(logFields.collectURL, collectorURL).
					Msg("Cancelling OpenTelemetry setup since parent context is cancelled")

				return // parent context cancelled, so give up trying
			}
		}
	}
}

// MonitorConnection starts a coroutine to attempt to ensure if it is still connected to the OTel collector or not
// It sets activated field accordingly
func (o *Otl) MonitorConnection(ctx context.Context, retryDelay time.Duration) {
	// prepare for retry loop
	ticker := time.NewTicker(retryDelay)

	// start the loop to monitor connection to OTel collector
	for {
		select {
		case <-ticker.C:
			clientOptions, err := o.setupTracerGrpcClientOption()
			if err != nil {
				o.logger.Warn().Msgf("Could not parse client otelConfig: %v", err.Error())
			}

			traceClient := otlptracegrpc.NewClient(clientOptions...)

			ctx2, cancel := context.WithTimeout(ctx, time.Second)
			err = traceClient.Start(ctx2)
			cancel() // this is here just in case it was successfully before the timeout

			if err != nil {
				o.logger.Error().
					Any(logFields.collectURL, o.otelConfig.Collector.Endpoint).
					Err(err).
					Msg("Unable to connect to OTel collector")

				eventBus.Publish(TopicDisconnected, o.otelConfig.Collector.Endpoint)
			} else {
				eventBus.Publish(TopicConnected, o.otelConfig.Collector.Endpoint)
			}

		case <-ctx.Done():
			return // parent context cancelled, so give up trying
		}
	}
}

// IsActivated returns true if it has been able to connect to OTel collector yet
func (o *Otl) IsActivated() bool {
	return o.activated
}
