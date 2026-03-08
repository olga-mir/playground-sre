package main

import (
	"net/http"

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

	// --- Scenario endpoints ---
	router.Group(func(r chi.Router) {
		// Access logging is off by default — too noisy under load.
		// Set LOG_LEVEL=debug in the ConfigMap to enable it.
		if app.config.LogLevel == "debug" {
			r.Use(middleware.Logger)
		}

		r.With(app.maybeRateLimit(50, 10)).Get("/v1/scenarios/sleep", app.sleepHandler)
		r.With(app.maybeRateLimit(5, 2)).Get("/v1/scenarios/cpu", app.cpuHandler)
		r.With(app.maybeRateLimit(10, 3)).Get("/v1/scenarios/upstream", app.upstreamHandler)
		r.With(app.maybeRateLimit(5, 2)).Get("/v1/scenarios/disk", app.diskHandler)
		r.With(app.maybeRateLimit(5, 2)).Get("/v1/scenarios/fanout", app.fanoutHandler)
	})
}

// maybeRateLimit returns a rate-limiting middleware, or a passthrough if
// DISABLE_RATE_LIMIT=true is set in the config (useful during load tests).
func (app *application) maybeRateLimit(rps float64, burst int) func(http.Handler) http.Handler {
	if app.config.DisableRateLimit {
		return func(next http.Handler) http.Handler { return next }
	}
	return cm.RateLimiter(rps, burst, app.rateLimitExceededResponse)
}
