package main

import (
	cm "playground-sre/internal/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func (app *application) RegisterRoutes(router chi.Router) {
	router.NotFound(app.notFoundResponse)

	router.Use(middleware.Recoverer)

	// ** Router Groups **

	// System endpoints
	router.Get("/v1/health", app.healthHandler)

	// User-facing APIs
	router.Group(func(r chi.Router) {
		r.Use(middleware.Logger)

		// This is the core endpoint specified in requirements
		// It is meant to be called manually, hence aggressive ratelimiting
		r.With(cm.RateLimiter(1, 1, app.rateLimitExceededResponse)).
			Get("/v1/stock", app.stockHandler)

		// Fallback endpoint. It will call endpoint which returns static payload, which can be a few days old
		// The upstream is public, no API key requirment and a very high ratelimit threshold.
		// This client-side ratelimit is just a precaution from runaway testing script
		r.With(cm.RateLimiter(20, 5, app.rateLimitExceededResponse)).
			Get("/v1/stock-fallback", app.stockFallbackHandler)
	})
}
