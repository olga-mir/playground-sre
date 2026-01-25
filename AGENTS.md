# Stock Ticker API

A RESTful web server in Go that returns closing stock prices from AlphaVantage.

## Implementation Status

### Core Requirements ✅

| Requirement | Status | Notes |
|-------------|--------|-------|
| GET endpoint for stock prices | ✅ | `/v1/stock` returns NDAYS closing prices + average |
| Environment variables | ✅ | SYMBOL, NDAYS, APIKEY via ConfigMap/Secret |
| Docker image | ✅ | golang:1.25-bookworm → distroless |
| K8s manifests | ✅ | Deployment, Service, PodDisruptionBudget, ConfigMap. Ingress not included as vanilla cluster infra is assumed. |
| Error handling with fallback hint | ✅ | Relays upstream payload + suggests `/v1/stock-fallback` |
| Health endpoint | ✅ | `/v1/health` with git SHA version |

### Extras ✅

| Feature | Status | Notes |
|---------|--------|-------|
| Static fallback endpoint | ✅ | `/v1/stock-fallback` using STATIC_FALLBACK_URL |
| Cloud Profiler | ✅ | Conditional: ENABLE_CLOUDPROFILER + GCP_PROJECT_ID |
| Separate extra ConfigMap | ✅ | `k8s/configmap-extra.yaml` (optional mount) |
| Separate extra Taskfile | ✅ | `Taskfile.extra.yaml` with `extra:` prefixed tasks |
| DockerHub publishing | ✅ | `olmigar/stock-ticker` with git SHA |
| Business logic tests | ✅ | `internal/stock/stock_test.go` |

## Project Structure

```
.
├── cmd/api/
│   ├── main.go           # Entry point, profiler init, server setup
│   ├── routes.go         # Chi router with groups and rate limiting
│   ├── health.go         # Health endpoint handler
│   ├── stock.go          # Stock endpoint handler
│   ├── stock_fallback.go # Fallback endpoint handler
│   ├── errors.go         # Standard error responses
│   └── helpers.go        # JSON helpers
├── internal/
│   ├── config/           # Environment configuration
│   ├── middleware/       # Rate limiter middleware
│   └── stock/            # Business logic + tests
├── k8s/
│   ├── deployment.yaml
│   ├── service.yaml
│   ├── pdb.yaml
│   ├── configmap.yaml
│   └── configmap-extra.yaml
├── Dockerfile
├── Taskfile.yaml
├── Taskfile.extra.yaml
└── README.md
```

## Configuration

### Core (required for `/v1/stock`)

| Variable | Description | Default |
|----------|-------------|---------|
| SYMBOL | Stock symbol | MSFT |
| NDAYS | Days to return | 7 |
| APIKEY | AlphaVantage key | (required) |

### Extras (optional)

| Variable | Description | Default |
|----------|-------------|---------|
| STATIC_FALLBACK_URL | Fallback data URL | (empty) |
| ENABLE_CLOUDPROFILER | Enable profiler | false |
| GCP_PROJECT_ID | GCP project | (empty) |

## Quick Commands

```bash
# Run locally
go run ./cmd/api

# Run with fallback
STATIC_FALLBACK_URL=https://head-in-the-cloudz.com/experiments/73/static-fallback go run ./cmd/api

# Run tests
go test -v ./...

# Build and push to DockerHub
task docker-build-push

# Deploy to k8s
kubectl create secret generic stock-ticker-secret --from-literal=APIKEY=$APIKEY
kubectl apply -f k8s/
```

## Architecture Decisions

1. **Chi router** - Lightweight, stdlib-compatible, good middleware support
2. **Router groups** - Health outside logging group, API endpoints with rate limiting
3. **Rate limiting** - Per-endpoint using golang.org/x/time/rate
4. **Distroless image** - Minimal attack surface, required for Cloud Profiler
5. **Business logic separation** - `internal/stock/` package with testable functions
6. **Optional extras** - ConfigMap with `optional: true`, code handles empty values
7. **High-Availability Kubernetes Configuration** - The deployment is configured for high availability using a `PodDisruptionBudget` to prevent simultaneous pod termination, `podAntiAffinity` to spread pods across nodes, and a fine-tuned `rollingUpdate` strategy for zero-downtime deployments.

## Future Considerations

- Metrics endpoint for Prometheus
- Structured logging with zerolog
- Circuit breaker for upstream calls
- Cache layer for stock data
- Graceful degradation patterns

---

## Original Requirements Reference

The original requirements from `../aux/requirements-description.md`:

- Part 1: Stock ticker web service returning NDAYS closing prices with average
- Part 2: Kubernetes manifests with ConfigMap and Secret
- Part 3: Resilience considerations (implemented via rate limiting, fallback endpoint)

Reference skeleton: `${HOME}/repos/experiments/73-2026.01-rust-go-webserver/go`
