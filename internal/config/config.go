// Package config provides configuration loading for the application.
package config

import (
	"os"
	"strconv"
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
	// Symbol is the default stock symbol to query.
	Symbol string
	// NDays is the default number of days of stock data to return.
	NDays int
	// APIKey is the key for accessing the AlphaVantage API.
	APIKey string

	// Extra (optional)
	// StaticFallbackURL is the URL for the static fallback data source.
	StaticFallbackURL string
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

	symbol := os.Getenv("SYMBOL")
	if symbol == "" {
		symbol = "MSFT"
	}

	ndays := 7
	if v := os.Getenv("NDAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			ndays = n
		}
	}

	apiKey := os.Getenv("APIKEY")

	staticFallbackURL := os.Getenv("STATIC_FALLBACK_URL")
	gcpProjectID := os.Getenv("GCP_PROJECT_ID")
	enableCloudProfiler := os.Getenv("ENABLE_CLOUDPROFILER") == "true"

	return &Config{
		ServerAddress:       addr,
		ReadTimeout:         15 * time.Second,
		WriteTimeout:        15 * time.Second,
		IdleTimeout:         60 * time.Second,
		Symbol:              symbol,
		NDays:               ndays,
		APIKey:              apiKey,
		StaticFallbackURL:   staticFallbackURL,
		GCPProjectID:        gcpProjectID,
		EnableCloudProfiler: enableCloudProfiler,
	}
}