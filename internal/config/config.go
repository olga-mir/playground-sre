package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	ServerAddress string
	ReadTimeout   time.Duration
	WriteTimeout  time.Duration
	IdleTimeout   time.Duration
	Symbol        string
	NDays         int
	APIKey        string

	// Extra (optional)
	StaticFallbackURL  string
	GCPProjectID       string
	EnableCloudProfiler bool
}

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
