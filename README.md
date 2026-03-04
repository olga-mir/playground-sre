[![Go](https://github.com/olga-mir/playground-sre/actions/workflows/go.yml/badge.svg)](https://github.com/olga-mir/playground-sre/actions/workflows/go.yml/badge.svg)
[![Go Report Card](https://goreportcard.com/badge/github.com/olga-mir/playground-sre)](https://goreportcard.com/report/github.com/olga-mir/playground-sre)
[![codecov](https://codecov.io/gh/olga-mir/playground-sre/branch/main/graph/badge.svg)](https://codecov.io/gh/olga-mir/playground-sre)

# perf-lab

A Go web server for exploring HTTP server performance under different workload profiles.

The goal is to observe how a Go server behaves under load when workloads exhibit different characteristics: CPU-bound work, upstream I/O, disk I/O, goroutine fan-out, long vs short requests, etc.

## Project Structure

```
.
├── cmd/api/
│   ├── main.go       # Entry point, server setup, graceful shutdown
│   ├── routes.go     # Chi router with middleware
│   ├── health.go     # Health endpoint
│   ├── errors.go     # Standard error responses
│   └── helpers.go    # JSON helpers
├── internal/
│   ├── config/       # Environment configuration
│   └── middleware/   # Rate limiter middleware
├── k8s/
│   ├── deployment.yaml
│   ├── service.yaml
│   └── pdb.yaml
├── Dockerfile
├── Taskfile.yaml
└── README.md
```

## Quick Commands

```bash
# Run locally
go run ./cmd/api

# Run tests
go test -v ./...

# Build binary
task build

# Deploy to k8s
kubectl apply -f k8s/
```

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| SERVER_ADDRESS | Listen address | :8080 |
| ENABLE_CLOUDPROFILER | Enable GCP Cloud Profiler | false |
| GCP_PROJECT_ID | GCP project for profiler | (empty) |
