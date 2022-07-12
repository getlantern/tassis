package telemetry

import (
	"context"
	"crypto/tls"

	"github.com/getlantern/keyman"
	hostMetrics "go.opentelemetry.io/contrib/instrumentation/host"
	runtimeMetrics "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
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

var caCert = `
-----BEGIN CERTIFICATE-----
MIIB2TCCAX+gAwIBAgIUALZZcdj7gI5fPFtaMc1JzBSGtEkwCgYIKoZIzj0EAwIw
KDESMBAGA1UEChMJTGFudGVybmV0MRIwEAYDVQQDEwlsYW50ZXJuZXQwHhcNMjIw
NTA1MDgzMDIwWhcNMzIwNTAyMDgzMDE5WjAoMRIwEAYDVQQKEwlMYW50ZXJuZXQx
EjAQBgNVBAMTCWxhbnRlcm5ldDBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABPQ7
JmXBOWjoXZsKHoAAA4fdnZHPE2aIftlVWgVOqsUqXJHfcm0RjgDJ2E0oME4DMsFw
HOiqRZJYYVrN3jGnQ/ujgYYwgYMwDgYDVR0PAQH/BAQDAgEGMB0GA1UdJQQWMBQG
CCsGAQUFBwMBBggrBgEFBQcDAjASBgNVHRMBAf8ECDAGAQH/AgEAMB0GA1UdDgQW
BBT//YqPe5Su0DMo/SOWQLcyOFI0VzAfBgNVHSMEGDAWgBT//YqPe5Su0DMo/SOW
QLcyOFI0VzAKBggqhkjOPQQDAgNIADBFAiEAlwyNsSgcy6/jvBXKSrfYMm/ef1eP
Nr0cfu9U5QFcClkCICEk02jI+ANnoGkbh1TEFK60JJadHDu7pLwe9wmUdF0P
-----END CERTIFICATE-----
`

// Start configures opentelemetry for collecting metrics and traces, and returns
// a function to shut down telemetry collection.
// StartTeleport() does this for Teleport, an OTEL gateway collector.

func StartTeleport() func() {
	// Teleport ingests telemetry data for subsequent processing and export to downstream services.
	// Teleport is built on OpenTelemetry's (gateway) collector.
	// TODO current export to Teleport is insecure; custom authenticator in development
	shutdownTeleportTracing := initTeleportTracing()
	shutdownTeleportMetrics := initTeleportMetrics()

	initMetrics()
	return func() {
		shutdownTeleportTracing()
		shutdownTeleportMetrics()
	}
}

func initTeleportTracing() func() {
	log.Debug("Will report traces to Teleport")
	// Create http client to talk to Teleport, our OTEL gateway collector

	cert, err := keyman.LoadCertificateFromPEMBytes([]byte(caCert))
	if err != nil {
		panic(err)
	}
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint("telemetry.lantr.net"),
		otlptracehttp.WithTLSClientConfig(&tls.Config{
			RootCAs: cert.PoolContainingCert(),
		}),
	}
	client := otlptracehttp.NewClient(opts...)

	// Create an exporter that exports to the Honeycomb OTEL collector
	exporter, err := otlptrace.New(context.Background(), client)
	if err != nil {
		log.Errorf("Unable to initialize Teleport tracing, will not report traces to Teleport")
		return func() {}
	}

	// Create a TracerProvider that uses the above exporter
	resource :=
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String("tassis-teleport"),
		)
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource),
	)

	// Configure OTEL tracing to use the above TracerProvider
	otel.SetTracerProvider(tracerProvider)

	return func() {
		tracerProvider.Shutdown(context.Background())
		exporter.Shutdown(context.Background())
	}
}

func initTeleportMetrics() func() {
	log.Debug("Will report metrics to Teleport")

	cert, err := keyman.LoadCertificateFromPEMBytes([]byte(caCert))
	if err != nil {
		panic(err)
	}

	// Create http client to talk to Teleport, our OTEL gateway collector
	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint("telemetry.lantr.net"),
		otlpmetrichttp.WithTLSClientConfig(&tls.Config{
			RootCAs: cert.PoolContainingCert(),
		}),
	}
	client := otlpmetrichttp.NewClient(opts...)

	// Create an exporter that exports to the Honeycomb OTEL collector
	exporter, err := otlpmetric.New(context.Background(), client)
	if err != nil {
		log.Errorf("(Teleport: Unable to initialize metrics, will not report metrics")
		return func() {}
	}

	resource :=
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String("tassis-teleport"),
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
	if startErr := c.Start(context.Background()); startErr != nil {
		log.Errorf("Teleport: Unable to start metrics controller, will not report metrics to Teleport: %v", startErr)
		return func() {}
	}
	if startErr := runtimeMetrics.Start(runtimeMetrics.WithMeterProvider(c)); startErr != nil {
		log.Errorf("Teleport: Failed to start runtime metrics: %v", startErr)
		return func() {}
	}

	if startErr := hostMetrics.Start(hostMetrics.WithMeterProvider(c)); startErr != nil {
		log.Errorf("Teleport: Failed to start host metrics: %v", startErr)
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
	var err error
	MessagesSent, err = meter.SyncFloat64().Counter(
		"messages_sent",
		instrument.WithDescription("The number of messages sent by tassis clients"),
	)
	if err != nil {
		log.Errorf("Unable to initialize messagesSent counter, will not track number of messages sent: %v", err)
	}
}
