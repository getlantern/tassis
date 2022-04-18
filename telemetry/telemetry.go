package telemetry

import (
	"context"
	"os"
	"time"

	"github.com/lightstep/otel-launcher-go/launcher"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.7.0"
	"google.golang.org/grpc/credentials"

	"github.com/getlantern/golog"
	"github.com/getlantern/ops"
)

var (
	log = golog.LoggerFor("telemetry")
)

// Start configures opentelemetry for collecting metrics and traces, and returns
// a function to shut down telemetry collection.
func Start() func() {
	lighstepKey := os.Getenv("LIGHTSTEP_KEY")
	honeycombKey := os.Getenv("HONEYCOMB_KEY")
	if lighstepKey != "" {
		log.Debug("Will report traces and metrics to Lighstep")
		ls := launcher.ConfigureOpentelemetry(
			launcher.WithServiceName("tassis"),
			launcher.WithMetricReportingPeriod(100*time.Millisecond),
			launcher.WithAccessToken(lighstepKey),
		)
		ops.EnableOpenTelemetry("tassis")
		return func() { ls.Shutdown() }
	} else if honeycombKey != "" {
		// Create gRPC client to talk to Honeycomb's OTEL collector
		opts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint("api.honeycomb.io:443"),
			otlptracegrpc.WithHeaders(map[string]string{
				"x-honeycomb-team": honeycombKey,
			}),
			otlptracegrpc.WithTLSCredentials(credentials.NewClientTLSFromCert(nil, "")),
		}
		client := otlptracegrpc.NewClient(opts...)

		// Create an exporter that exports to the Honeycomb OTEL collector
		exporter, err := otlptrace.New(context.Background(), client)
		if err != nil {
			log.Errorf("Unable to initialize Honeycomb, will not report traces and metrics")
			return func() {}
		}

		// Create a TracerProvider that uses the above exporter
		resource :=
			resource.NewWithAttributes(
				semconv.SchemaURL,
				semconv.ServiceNameKey.String("tassis"),
			)
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(exporter),
			sdktrace.WithResource(resource),
		)

		// Configure OTEL tracing to use the above TracerProvider
		otel.SetTracerProvider(tp)

		// Return stop function that shuts down the above TracerProvider
		ops.EnableOpenTelemetry("tassis")
		return func() { tp.Shutdown(context.Background()) }
	} else {
		log.Debug("No LIGHTSTEP_KEY or HONEYCOMB_KEY in environment, will not report traces and metrics")
		return func() {}
	}
}
