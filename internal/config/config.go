// Package config provides configuration loading for the application.
package config

import (
	"os"
	"time"
)

// Config holds the configuration for the application.
type Config struct {
	// ServerAddress is the address the HTTP server listens on.
	ServerAddress string

	// ReadTimeout is the maximum duration for reading the entire request, including the body.
	ReadTimeout time.Duration

	// WriteTimeout is the maximum duration before timing out writes of the response.
	WriteTimeout time.Duration

	// IdleTimeout is the maximum amount of time to wait for the next request when keep-alives are enabled.
	IdleTimeout time.Duration

	// GCPProjectID is the Google Cloud project ID for Cloud Profiler.
	GCPProjectID string

	// EnableCloudProfiler enables the Google Cloud Profiler.
	EnableCloudProfiler bool
}

// Load reads configuration from environment variables and returns a Config struct.
func Load() *Config {
	addr := os.Getenv("SERVER_ADDRESS")
	if addr == "" {
		addr = ":8080"
	}

	gcpProjectID := os.Getenv("GCP_PROJECT_ID")
	enableCloudProfiler := os.Getenv("ENABLE_CLOUDPROFILER") == "true"

	return &Config{
		ServerAddress:       addr,
		ReadTimeout:         15 * time.Second,
		WriteTimeout:        15 * time.Second,
		IdleTimeout:         60 * time.Second,
		GCPProjectID:        gcpProjectID,
		EnableCloudProfiler: enableCloudProfiler,
	}
}
