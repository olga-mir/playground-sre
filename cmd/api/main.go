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
)

// gitSHA is the git commit hash of the running application.
// It is set at build time.
var gitSHA = "unknown"

// application holds the dependencies for the HTTP handlers, middleware, and helpers.
type application struct {
	config *config.Config
}

func main() {
	cfg := config.Load()

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

	app := &application{
		config: cfg,
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
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}
