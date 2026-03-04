# perf-lab

A Go web server skeleton for exploring HTTP performance under different workload profiles.

## Project Structure

```
.
├── cmd/api/
│   ├── main.go           # Entry point, profiler init, server setup, graceful shutdown
│   ├── routes.go         # Chi router with middleware groups
│   ├── health.go         # Health endpoint (/v1/health)
│   ├── errors.go         # Standard JSON error responses
│   └── helpers.go        # JSON write helpers
├── internal/
│   ├── config/           # Environment configuration
│   └── middleware/       # Rate limiter middleware
├── k8s/
│   ├── deployment.yaml   # 2-replica deployment with pod anti-affinity
│   ├── service.yaml      # ClusterIP service
│   └── pdb.yaml          # PodDisruptionBudget (minAvailable: 1)
├── Dockerfile            # Multi-stage build: golang → distroless
├── Taskfile.yaml
└── README.md
```

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| SERVER_ADDRESS | Listen address | :8080 |
| ENABLE_CLOUDPROFILER | Enable GCP Cloud Profiler | false |
| GCP_PROJECT_ID | GCP project for profiler | (empty) |

## Architecture Notes

- **Chi router** - Lightweight, stdlib-compatible, good middleware support
- **Rate limiting** - Per-endpoint via `golang.org/x/time/rate`
- **Distroless image** - Minimal attack surface, compatible with Cloud Profiler
- **HA k8s config** - PodDisruptionBudget + pod anti-affinity + rolling update strategy
- **Graceful shutdown** - 10-second drain window on SIGINT/SIGTERM
