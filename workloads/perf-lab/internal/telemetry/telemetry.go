// Package telemetry wires together the OpenTelemetry metric SDK and a Prometheus
// exporter, so that OTEL-instrumented code is automatically scraped by any
// Prometheus-compatible collector (including GKE Managed Prometheus).
//
// # How the layers fit together
//
//   Application code
//       │  uses OTEL metric API (Counter, Histogram, Gauge)
//       ▼
//   OTEL MeterProvider  (sdk/metric)
//       │  periodically reads from instruments
//       ▼
//   Prometheus Exporter  (exporters/prometheus)
//       │  populates a prometheus.Registry
//       ▼
//   promhttp.Handler  ← scraped by GKE Managed Prometheus via PodMonitoring CRD
//
// Traces are intentionally omitted here; add an OTLP trace exporter pointing at
// the GKE OpenTelemetry Collector (or Cloud Trace) when you want distributed
// tracing.  The global TracerProvider is left as the noop default.
package telemetry

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	promexporter "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// Telemetry holds the MeterProvider and pre-built HTTP-level instruments.
// Call New() once during startup; the resulting value is safe to share.
type Telemetry struct {
	// MeterProvider is the root of the OTEL metric hierarchy.
	// Shut it down on server exit to flush any pending data.
	MeterProvider *sdkmetric.MeterProvider

	// Handler serves the /metrics endpoint in Prometheus text format.
	Handler http.Handler

	reqDuration metric.Float64Histogram
	reqTotal    metric.Int64Counter
	inFlight    metric.Int64UpDownCounter
}

// New initialises the OTEL metric pipeline:
//  1. Creates a dedicated prometheus.Registry (isolated from the default global).
//  2. Builds an OTEL Prometheus exporter backed by that registry.
//  3. Wraps it in an sdkmetric.MeterProvider and registers it as the global.
//  4. Pre-creates the three HTTP-level instruments used by Middleware.
func New() (*Telemetry, error) {
	reg := prometheus.NewRegistry()

	exporter, err := promexporter.New(promexporter.WithRegisterer(reg))
	if err != nil {
		return nil, err
	}

	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	// Set as global so otel.GetMeterProvider() works anywhere in the process.
	otel.SetMeterProvider(provider)

	m := provider.Meter("perf-lab/http")

	reqDuration, err := m.Float64Histogram(
		"http_request_duration_seconds",
		metric.WithDescription("HTTP request latency distribution"),
		metric.WithUnit("s"),
		// Buckets tuned for a mix of sub-ms health checks and multi-second CPU/disk scenarios.
		metric.WithExplicitBucketBoundaries(
			0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30,
		),
	)
	if err != nil {
		return nil, err
	}

	reqTotal, err := m.Int64Counter(
		"http_requests_total",
		metric.WithDescription("Total HTTP requests by route, method and status"),
	)
	if err != nil {
		return nil, err
	}

	inFlight, err := m.Int64UpDownCounter(
		"http_requests_in_flight",
		metric.WithDescription("HTTP requests currently being processed"),
	)
	if err != nil {
		return nil, err
	}

	return &Telemetry{
		MeterProvider: provider,
		Handler:       promhttp.HandlerFor(reg, promhttp.HandlerOpts{}),
		reqDuration:   reqDuration,
		reqTotal:      reqTotal,
		inFlight:      inFlight,
	}, nil
}

// Middleware records per-request HTTP metrics. Register it on the root router
// so every route (including health) is covered.
//
//	router.Use(tel.Middleware)
func (t *Telemetry) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()

		t.inFlight.Add(ctx, 1)
		defer func() {
			elapsed := time.Since(start).Seconds()
			attrs := metric.WithAttributes(
				attribute.String("method", r.Method),
				attribute.String("route", chiRoute(r)),
			)
			t.inFlight.Add(ctx, -1)
			t.reqTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("method", r.Method),
				attribute.String("route", chiRoute(r)),
				attribute.Int("status", sw.status),
			))
			t.reqDuration.Record(ctx, elapsed, attrs)
		}()

		next.ServeHTTP(sw, r)
	})
}

// Meter returns an OTEL Meter scoped to the given instrumentation name.
// Call this after New() so the global provider is already set.
func Meter(name string) metric.Meter {
	return otel.GetMeterProvider().Meter(name)
}

// Shutdown flushes pending metrics and stops background goroutines.
// Pass a context with a deadline matching your graceful-shutdown window.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	return t.MeterProvider.Shutdown(ctx)
}

// statusWriter wraps http.ResponseWriter to capture the written status code.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

// chiRoute returns the matched chi route pattern, falling back to the raw path.
// Chi populates RouteContext before invoking the middleware chain, so the
// pattern is available even inside global (root-level) middleware.
func chiRoute(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		if p := rctx.RoutePattern(); p != "" {
			return p
		}
	}
	return r.URL.Path
}
