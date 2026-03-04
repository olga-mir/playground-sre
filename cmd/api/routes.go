package main

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// RegisterRoutes sets up the routing for the application.
func (app *application) RegisterRoutes(router chi.Router) {
	router.NotFound(app.notFoundResponse)

	router.Use(middleware.Recoverer)

	// System endpoints
	router.Get("/v1/health", app.healthHandler)

	// Performance scenario endpoints
	router.Group(func(r chi.Router) {
		r.Use(middleware.Logger)
	})
}
