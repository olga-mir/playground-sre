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

		// Fallback endpoint. It will call endpoint which returns static payload, with a few days old data.
		// The upstream this endpoint is relying on is public, with no API key or ratelimiting
		r.With(cm.RateLimiter(20, 5, app.rateLimitExceededResponse)).
			Get("/v1/stock-fallback", app.stockFallbackHandler)
	})
}
