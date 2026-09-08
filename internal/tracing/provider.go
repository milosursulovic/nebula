// Package tracing wires OpenTelemetry (spec section 36) — one shared
// constructor both nebula-api and nebula-agent call, each with its own
// service name so Jaeger's service list shows both. Exports over OTLP-HTTP
// straight to Jaeger's all-in-one image (COLLECTOR_OTLP_ENABLED=true),
// no separate otel-collector container needed.
package tracing

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// NewProvider builds and registers (as the process-global tracer
// provider/propagator) an OTLP-HTTP-exporting TracerProvider for
// serviceName, sending spans to otlpEndpoint (e.g. "http://jaeger:4318").
// Callers should defer Shutdown at process exit to flush pending spans.
func NewProvider(ctx context.Context, serviceName, otlpEndpoint string) (*sdktrace.TracerProvider, error) {
	endpointHost, insecure, err := splitEndpoint(otlpEndpoint)
	if err != nil {
		return nil, fmt.Errorf("parse otlp endpoint %q: %w", otlpEndpoint, err)
	}

	opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(endpointHost)}
	if insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create otlp exporter: %w", err)
	}

	res, err := resource.New(ctx, resource.WithAttributes(semconv.ServiceName(serviceName)))
	if err != nil {
		return nil, fmt.Errorf("create otel resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return tp, nil
}

// splitEndpoint turns "http://host:port" into ("host:port", insecure=true)
// — otlptracehttp.WithEndpoint wants host:port, not a full URL, and
// insecure (plain HTTP, this project's dev-only setup) needs its own
// explicit option.
func splitEndpoint(endpoint string) (string, bool, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", false, err
	}
	if u.Host == "" {
		return "", false, fmt.Errorf("missing host in %q", endpoint)
	}
	return u.Host, strings.EqualFold(u.Scheme, "http"), nil
}
