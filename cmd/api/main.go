package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cloud.google.com/go/profiler"
	"github.com/go-chi/chi/v5"
	"github.com/olga-mir/playground-sre/internal/config"
	"github.com/olga-mir/playground-sre/internal/telemetry"
)

// gitSHA is set at build time via -ldflags="-X main.gitSHA=<sha>".
var gitSHA = "unknown"

// application holds the dependencies for HTTP handlers and middleware.
type application struct {
	config      *config.Config
	tel         *telemetry.Telemetry
	scenMetrics *scenarioMetrics
}

func main() {
	cfg := config.Load()

	// Optional: GCP Cloud Profiler (flame graphs in Cloud Console).
	if cfg.EnableCloudProfiler && cfg.GCPProjectID != "" {
		profCfg := profiler.Config{
			Service:        "perf-lab",
			ServiceVersion: "1.0.0",
			ProjectID:      cfg.GCPProjectID,
		}
		if err := profiler.Start(profCfg); err != nil {
			log.Printf("Warning: failed to start profiler: %v", err)
		} else {
			log.Println("Cloud Profiler started")
		}
	}

	// Telemetry: OTEL metric SDK → Prometheus exporter → /metrics endpoint.
	// Must be initialised before newScenarioMetrics() reads the global MeterProvider.
	tel, err := telemetry.New()
	if err != nil {
		log.Fatalf("failed to initialize telemetry: %v", err)
	}

	sm, err := newScenarioMetrics()
	if err != nil {
		log.Fatalf("failed to initialize scenario metrics: %v", err)
	}

	app := &application{
		config:      cfg,
		tel:         tel,
		scenMetrics: sm,
	}

	router := chi.NewRouter()
	app.RegisterRoutes(router)

	server := &http.Server{
		Addr:         cfg.ServerAddress,
		Handler:      router,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	go func() {
		log.Printf("Starting server on %s", cfg.ServerAddress)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}
	if err := app.tel.Shutdown(ctx); err != nil {
		log.Printf("Telemetry shutdown error: %v", err)
	}

	log.Println("Server exited")
}
