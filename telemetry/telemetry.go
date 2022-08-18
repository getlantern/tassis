package telemetry

import (
	"context"
	"os"

	"github.com/lightstep/otel-launcher-go/launcher"
	hostMetrics "go.opentelemetry.io/contrib/instrumentation/host"
	runtimeMetrics "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/global"
	"go.opentelemetry.io/otel/metric/instrument"
	"go.opentelemetry.io/otel/metric/instrument/syncfloat64"
	controller "go.opentelemetry.io/otel/sdk/metric/controller/basic"
	"go.opentelemetry.io/otel/sdk/metric/export/aggregation"
	processor "go.opentelemetry.io/otel/sdk/metric/processor/basic"
	selector "go.opentelemetry.io/otel/sdk/metric/selector/simple"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.7.0"

	"github.com/getlantern/golog"
)

var (
	log = golog.LoggerFor("telemetry")

	MessagesSent syncfloat64.Counter
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
			launcher.WithAccessToken(lighstepKey),
		)

		initMetrics()
		return func() { ls.Shutdown() }
	} else if honeycombKey != "" {
		shutdownTracing := initHoneycombTracing(honeycombKey)
		shutdownMetrics := initHoneycombMetrics(honeycombKey)

		initMetrics()
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
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0.01))),
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
			"x-honeycomb-team":    honeycombKey,
			"x-honeycomb-dataset": "tassis",
		}),
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
			aggregation.DeltaTemporalitySelector(),
			processor.WithMemory(true),
		),
		controller.WithExporter(exporter),
		controller.WithResource(resource),
	)
	if startErr := c.Start(context.Background()); startErr != nil {
		log.Errorf("Unable to start metrics controller, will not report metrics to Honeycomb: %v", startErr)
		return func() {}
	}
	if startErr := runtimeMetrics.Start(runtimeMetrics.WithMeterProvider(c)); startErr != nil {
		log.Errorf("Failed to start runtime metrics: %v", startErr)
		return func() {}
	}

	if startErr := hostMetrics.Start(hostMetrics.WithMeterProvider(c)); startErr != nil {
		log.Errorf("Failed to start host metrics: %v", startErr)
		return func() {}
	}

	global.SetMeterProvider(c)
	return func() {
		c.Stop(context.Background())
		exporter.Shutdown(context.Background())
	}
}

func initMetrics() {
	meter := global.Meter("github.com/getlantern/tassis")
	initCounter(meter, "messages_sent", "The number of messages sent by tassis clients")
	initCounter(meter, "messages_received", "The number of messages received by tassis clients")
}

func initCounter(meter metric.Meter, name, description string) {
	var err error
	MessagesSent, err = meter.SyncFloat64().Counter(
		name,
		instrument.WithDescription(description),
	)
	if err != nil {
		log.Errorf("Unable to initialize %v counter, will not track %v: %v", name, description, err)
	}
}
