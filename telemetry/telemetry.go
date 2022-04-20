package telemetry

import (
	"context"
	"os"
	"time"

	"github.com/lightstep/otel-launcher-go/launcher"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric/global"
	controller "go.opentelemetry.io/otel/sdk/metric/controller/basic"
	"go.opentelemetry.io/otel/sdk/metric/export/aggregation"
	processor "go.opentelemetry.io/otel/sdk/metric/processor/basic"
	selector "go.opentelemetry.io/otel/sdk/metric/selector/simple"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.7.0"
	"google.golang.org/grpc/credentials"

	"github.com/getlantern/golog"
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
		return func() { ls.Shutdown() }
	} else if honeycombKey != "" {
		shutdownTracing := initHoneycombTracing(honeycombKey)
		shutdownMetrics := initHoneycombMetrics(honeycombKey)

		// Return stop function that shuts down the above TracerProvider
		return func() {
			shutdownTracing()
			shutdownMetrics()
		}
	} else {
		log.Debug("No LIGHTSTEP_KEY or HONEYCOMB_KEY in environment, will not report traces and metrics")
		return func() {}
	}
}

func initHoneycombTracing(honeycombKey string) func() {
	log.Debug("Will report traces to Honeycomb")
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
		log.Errorf("Unable to initialize Honeycomb tracing, will not report traces")
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

	return func() {
		tp.Shutdown(context.Background())
		exporter.Shutdown(context.Background())
	}
}

func initHoneycombMetrics(honeycombKey string) func() {
	log.Debug("Will report metrics to Honeycomb")

	// Create gRPC client to talk to Honeycomb's OTEL collector
	opts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint("api.honeycomb.io:443"),
		otlpmetricgrpc.WithHeaders(map[string]string{
			"x-honeycomb-team": honeycombKey,
		}),
		otlpmetricgrpc.WithTLSCredentials(credentials.NewClientTLSFromCert(nil, "")),
	}
	client := otlpmetricgrpc.NewClient(opts...)

	// Create an exporter that exports to the Honeycomb OTEL collector
	exporter, err := otlpmetric.New(context.Background(), client)
	if err != nil {
		log.Errorf("Unable to initialize Honeycomb metrics, will not report metrics")
		return func() {}
	}

	resource :=
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String("tassis"),
		)

	c := controller.New(
		processor.NewFactory(
			selector.NewWithInexpensiveDistribution(),
			aggregation.CumulativeTemporalitySelector(),
			processor.WithMemory(true),
		),
		controller.WithExporter(exporter),
		controller.WithResource(resource),
	)

	global.SetMeterProvider(c)
	return func() {
		c.Stop(context.Background())
		exporter.Shutdown(context.Background())
	}
}
