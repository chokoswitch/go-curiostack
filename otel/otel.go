package otel

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"

	gcppropagator "github.com/GoogleCloudPlatform/opentelemetry-operations-go/propagator"
	"go.opentelemetry.io/contrib/detectors/gcp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

var (
	meterProvider  *sdkmetric.MeterProvider
	tracerProvider *sdktrace.TracerProvider

	initOnce sync.Once
)

func init() {
	Initialize()
}

// Initialize initializes the OpenTelemetry SDK with the default configuration
// and instruments globals where applicable.
func Initialize() {
	initOnce.Do(doInitialize)
}

func doInitialize() {
	ctx := context.Background()

	// Avoid autoexport package because we prefer to default to none, which is not easy,
	// and don't want multiple OTLP exporters included in the binary.
	res := newResource(ctx)
	meterProvider = newMeterProvider(ctx, res)
	otel.SetMeterProvider(meterProvider)
	tracerProvider = newTracerProvider(ctx, res)
	otel.SetTracerProvider(tracerProvider)

	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			gcppropagator.CloudTraceOneWayPropagator{},
			propagation.TraceContext{},
			propagation.Baggage{},
		))

	http.DefaultClient = &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
}

func newResource(ctx context.Context) *resource.Resource {
	// Ignore resource creation errors, our logic is simple and any error is in
	// a library out of our control. Even with errors there will generally be enough
	// information in the resource.
	res, _ := resource.New(ctx,
		resource.WithDetectors(gcp.NewDetector()),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithContainer(),
		resource.WithProcess(),
		resource.WithOS(),
		resource.WithFromEnv(),
	)

	return res
}

func newTracerProvider(ctx context.Context, res *resource.Resource) *sdktrace.TracerProvider {
	exporter, err := newSpanExporter(ctx)
	if err != nil {
		log.Fatalf("Failed to create span exporter: %v\n", err)
	}

	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
	}
	if exporter != nil {
		opts = append(opts, sdktrace.WithBatcher(exporter))
	}

	return sdktrace.NewTracerProvider(opts...)
}

func newSpanExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	switch os.Getenv("OTEL_TRACES_EXPORTER") {
	case "console":
		exp, err := stdouttrace.New()
		if err != nil {
			return nil, fmt.Errorf("otel: creating stdout span exporter: %w", err)
		}
		return exp, nil
	case "otlp":
		exp, err := otlptracehttp.New(ctx, otlptracehttp.WithInsecure())
		if err != nil {
			return nil, fmt.Errorf("otel: creating otlp span exporter: %w", err)
		}
		return exp, nil
	case "none":
		fallthrough
	default:
		return nil, nil //nolint:nilnil
	}
}

func newMeterProvider(ctx context.Context, res *resource.Resource) *sdkmetric.MeterProvider {
	exporter, err := newMetricExporter(ctx)
	if err != nil {
		log.Fatalf("Failed to create metric exporter: %v\n", err)
	}

	opts := []sdkmetric.Option{
		sdkmetric.WithResource(res),
	}
	if exporter != nil {
		opts = append(opts, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)))
	}

	return sdkmetric.NewMeterProvider(opts...)
}

func newMetricExporter(ctx context.Context) (sdkmetric.Exporter, error) {
	switch os.Getenv("OTEL_METRICS_EXPORTER") {
	case "console":
		exp, err := stdoutmetric.New()
		if err != nil {
			return nil, fmt.Errorf("otel: creating stdout metric exporter: %w", err)
		}
		return exp, nil
	case "otlp":
		exp, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithInsecure())
		if err != nil {
			return nil, fmt.Errorf("otel: creating otlp metric exporter: %w", err)
		}
		return exp, nil
	case "none":
		fallthrough
	default:
		return nil, nil //nolint:nilnil
	}
}
