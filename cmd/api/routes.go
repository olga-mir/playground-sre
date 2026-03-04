package main

import (
	cm "github.com/olga-mir/playground-sre/internal/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// RegisterRoutes configures the chi router with all endpoints and middleware.
func (app *application) RegisterRoutes(router chi.Router) {
	router.NotFound(app.notFoundResponse)

	// Global middleware: panic recovery + OTEL HTTP metrics for every route.
	router.Use(middleware.Recoverer)
	router.Use(app.tel.Middleware)

	// --- Infrastructure endpoints (no access logging) ---
	router.Get("/v1/health", app.healthHandler)
	// Prometheus metrics endpoint – scraped by GKE Managed Prometheus via PodMonitoring.
	router.Handle("/metrics", app.tel.Handler)

	// --- Scenario endpoints (with access logging + per-route rate limiting) ---
	router.Group(func(r chi.Router) {
		r.Use(middleware.Logger)

		// Sleep: baseline, cheap to call repeatedly – moderate rate limit.
		r.With(cm.RateLimiter(50, 10, app.rateLimitExceededResponse)).
			Get("/v1/scenarios/sleep", app.sleepHandler)

		// CPU: deliberately expensive – conservative rate limit to avoid DoS.
		r.With(cm.RateLimiter(5, 2, app.rateLimitExceededResponse)).
			Get("/v1/scenarios/cpu", app.cpuHandler)

		// Upstream: makes outbound HTTP calls – moderate limit.
		r.With(cm.RateLimiter(10, 3, app.rateLimitExceededResponse)).
			Get("/v1/scenarios/upstream", app.upstreamHandler)

		// Disk: allocates large temp files – conservative limit.
		r.With(cm.RateLimiter(5, 2, app.rateLimitExceededResponse)).
			Get("/v1/scenarios/disk", app.diskHandler)

		// Fanout: can spawn thousands of goroutines – conservative limit.
		r.With(cm.RateLimiter(5, 2, app.rateLimitExceededResponse)).
			Get("/v1/scenarios/fanout", app.fanoutHandler)
	})
}
