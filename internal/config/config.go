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

	// LogLevel controls verbosity. Set to "debug" to enable per-request access
	// logs. Any other value (including the default empty string) disables them.
	// Request-level logging is too noisy during load tests.
	LogLevel string

	// DisableRateLimit bypasses all per-route rate limiting when true.
	// Set to true during load tests where the rate limiter would cap QPS.
	DisableRateLimit bool
}

// Load reads configuration from environment variables and returns a Config struct.
func Load() *Config {
	addr := os.Getenv("SERVER_ADDRESS")
	if addr == "" {
		addr = ":8080"
	}

	gcpProjectID := os.Getenv("GCP_PROJECT_ID")
	enableCloudProfiler := os.Getenv("ENABLE_CLOUDPROFILER") == "true"
	logLevel := os.Getenv("LOG_LEVEL")
	disableRateLimit := os.Getenv("DISABLE_RATE_LIMIT") == "true"

	return &Config{
		ServerAddress: addr,
		// ReadTimeout guards against slow/malicious clients sending requests.
		ReadTimeout: 15 * time.Second,
		// WriteTimeout is intentionally disabled (0 = no limit). Scenario handlers
		// run for durations controlled by the caller via the ?duration= param.
		// A fixed server-level WriteTimeout would silently kill any scenario longer
		// than that value, making results look like errors rather than timeouts.
		WriteTimeout:        0,
		IdleTimeout:         60 * time.Second,
		GCPProjectID:        gcpProjectID,
		EnableCloudProfiler: enableCloudProfiler,
		LogLevel:            logLevel,
		DisableRateLimit:    disableRateLimit,
	}
}
