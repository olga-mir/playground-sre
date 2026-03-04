[![Go](https://github.com/olga-mir/playground-sre/actions/workflows/go.yml/badge.svg)](https://github.com/olga-mir/playground-sre/actions/workflows/go.yml/badge.svg)
[![Go Report Card](https://goreportcard.com/badge/github.com/olga-mir/playground-sre)](https://goreportcard.com/report/github.com/olga-mir/playground-sre)
[![codecov](https://codecov.io/gh/olga-mir/playground-sre/branch/main/graph/badge.svg)](https://codecov.io/gh/olga-mir/playground-sre)

# perf-lab

A Go web server for exploring HTTP server performance under load when workloads exhibit different characteristics: CPU-bound work, blocking I/O, goroutine fan-out, upstream service calls, and more.

Each scenario is a dedicated endpoint with tunable parameters. Hit it with a load generator (Fortio, `hey`, `wrk`) and watch the metrics.

## Scenarios

| Endpoint | What it exercises | Key parameters |
|---|---|---|
| `GET /v1/scenarios/cpu` | SHA-256 hash loop across N goroutines | `duration` (default `2s`), `goroutines` (default `1`) |
| `GET /v1/scenarios/sleep` | Blocking wait — baseline overhead | `duration` (default `100ms`) |
| `GET /v1/scenarios/upstream` | Concurrent HTTP fan-out | `n` (default `5`), `target` URL, `timeout` (default `5s`) |
| `GET /v1/scenarios/disk` | Temp file write + read | `size` (default `1mb`), `sync` (default `false`) |
| `GET /v1/scenarios/fanout` | Goroutine worker pool with CPU tasks | `workers` (default `10`), `task_duration` (default `100ms`) |
| `GET /v1/health` | Health check (k8s probes) | — |
| `GET /metrics` | Prometheus metrics (OTEL-instrumented) | — |

### Example calls

```bash
# SHA-256 loop, 4 goroutines, 3 seconds
curl 'http://localhost:8080/v1/scenarios/cpu?duration=3s&goroutines=4'

# Sleep 500ms — baseline latency floor
curl 'http://localhost:8080/v1/scenarios/sleep?duration=500ms'

# Fan out 20 concurrent calls to itself
curl 'http://localhost:8080/v1/scenarios/upstream?n=20'

# Write and read a 10 MiB file, with fsync
curl 'http://localhost:8080/v1/scenarios/disk?size=10mb&sync=true'

# 100 goroutines, each running a 200ms CPU task
curl 'http://localhost:8080/v1/scenarios/fanout?workers=100&task_duration=200ms'
```

## Observability

Metrics are instrumented using the **OpenTelemetry metric API** and exported in **Prometheus text format** via the OTEL Prometheus exporter.

```
Application (OTEL Counter/Histogram/Gauge)
    │
    ▼
OTEL MeterProvider  (go.opentelemetry.io/otel/sdk/metric)
    │
    ▼
Prometheus Exporter  (go.opentelemetry.io/otel/exporters/prometheus)
    │
    ▼
GET /metrics  ←── scraped by GKE Managed Prometheus (PodMonitoring CRD)
```

**Key metrics exposed:**

| Metric | Type | Labels |
|---|---|---|
| `http_request_duration_seconds` | histogram | method, route |
| `http_requests_total` | counter | method, route, status |
| `http_requests_in_flight` | updowncounter | method, route |
| `scenario_cpu_hashes_total` | counter | — |
| `scenario_upstream_calls_total` | counter | status=ok\|error |
| `scenario_disk_bytes_written_total` | counter | — |
| `scenario_disk_bytes_read_total` | counter | — |
| `scenario_fanout_hashes_total` | counter | — |

### OTEL and Prometheus

- **Prometheus** is a scrape-based data model and wire format. GKE Managed Prometheus runs at target environment.
- **OpenTelemetry** is a vendor-neutral instrumentation API + SDK. Metrics written with OTEL API are emitted as Prometheus metrics and scraped by a prom collector (GKE managed prometheus in the initial stages of this project)
- From GMP's perspective there is no difference — it just scrapes `/metrics`.
- To also export **traces**, add an OTLP trace exporter in `main.go` pointing at the GKE OpenTelemetry Collector (or Cloud Trace directly). The instrumentation API stays the same.

## Project Structure

```
.
├── cmd/api/
│   ├── main.go           # Entry point, telemetry init, graceful shutdown
│   ├── routes.go         # Chi router, middleware, rate limits per scenario
│   ├── scenarios.go      # HTTP handlers + OTEL metric instruments
│   ├── health.go         # /v1/health
│   ├── errors.go         # Standard JSON error responses
│   └── helpers.go        # writeJSON helper
├── internal/
│   ├── config/           # Environment config
│   ├── middleware/       # Rate limiter
│   ├── telemetry/        # OTEL setup, Prometheus exporter, HTTP middleware
│   └── scenarios/        # Workload implementations (pure computation, no HTTP)
│       ├── cpu.go
│       ├── sleep.go
│       ├── upstream.go
│       ├── disk.go
│       └── fanout.go
└── k8s/
    ├──  ... # deployment will be hooked to `olga-mir/playground` clusters.
```

## Quick Start

```bash
# Run locally
go run ./cmd/api

# Build
task build
```

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `SERVER_ADDRESS` | Listen address | `:8080` |
| `ENABLE_CLOUDPROFILER` | Enable GCP Cloud Profiler | `false` |
| `GCP_PROJECT_ID` | GCP project (required for profiler) | — |
