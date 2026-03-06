[![Go](https://github.com/olga-mir/playground-sre/actions/workflows/go.yml/badge.svg)](https://github.com/olga-mir/playground-sre/actions/workflows/go.yml/badge.svg)
[![Go Report Card](https://goreportcard.com/badge/github.com/olga-mir/playground-sre)](https://goreportcard.com/report/github.com/olga-mir/playground-sre)

# perf-lab

A Go web server for exploring HTTP server performance under load when workloads exhibit different characteristics: CPU-bound work, blocking I/O, goroutine fan-out, upstream service calls, and more.

Each scenario is a dedicated endpoint with tunable parameters. Hit it with a load generator (Fortio, `hey`, `wrk`) and watch the metrics.

## Platform Integration

This service is deployed to the `sre` namespace on the `apps-dev` GKE cluster, managed by FluxCD from the platform repo [`github.com/olga-mir/playground`](https://github.com/olga-mir/playground).

- The platform watches `k8s/` in this repo directly — no manual manifest copying needed.
- Image promotion is fully automatic: CI pushes a new tag → Flux detects it → Flux commits the updated tag to `k8s/deployment.yaml` → Flux deploys the new version.
- Image tag format: `main-<YYYYMMDDHHMMSS>-<short-sha>`. Flux selects the latest by sorting numerically on the timestamp.
- **To deploy:** merge a PR to `main`. CI builds and pushes; Flux promotes within ~5 minutes.
- **One manual prerequisite:** the platform cluster needs an SSH deploy key secret named `playground-sre-deploy-key` in the `flux-system` namespace with push access to this repo (required for Flux image automation to commit tag updates back).

## Deploying Your Fork

Two paths: standalone `kubectl` or GitOps with FluxCD.

### Prerequisites (both paths)

1. **Docker Hub account** — or any registry; substitute `docker.io/you/perf-lab` throughout.
2. **Kubernetes cluster** — GKE recommended; any cluster with `kubectl` access works.
3. **Update image references** in two files to use your Docker Hub username:
   - `k8s/deployment.yaml` — change `olmigar/perf-lab` on the `image:` line
   - `.github/workflows/build-push.yml` — change `olmigar/perf-lab` in the `tags:` field
4. **GitHub Actions secrets** — add these in your fork's Settings → Secrets → Actions:
   - `DOCKERHUB_USERNAME` — your Docker Hub username
   - `DOCKERHUB_TOKEN` — a Docker Hub access token (hub.docker.com → Account Settings → Security)

### Option A: Standalone (kubectl only)

Build and push the image, then apply the manifests directly:

```bash
# Build and push (or let CI do it after a push to main)
docker build -t you/perf-lab:latest .
docker push you/perf-lab:latest

# Deploy (update the image tag in deployment.yaml first)
kubectl create namespace sre
kubectl apply -f k8s/
```

Flux image automation is not involved — you update the image tag in `k8s/deployment.yaml` manually or via CI.

### Option B: GitOps with FluxCD

Assumes FluxCD is already bootstrapped on your cluster. You need three things:

**1. Flux resources pointing at your fork**

Apply these in the `flux-system` namespace (or wherever your Flux config lives):

```yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: perf-lab
  namespace: flux-system
spec:
  interval: 1m
  url: https://github.com/you/playground-sre   # your fork
  ref:
    branch: main
---
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: perf-lab
  namespace: flux-system
spec:
  interval: 5m
  sourceRef:
    kind: GitRepository
    name: perf-lab
  path: ./k8s
  prune: true
  targetNamespace: sre
```

**2. Image automation resources** (so Flux updates the tag on each CI push)

```yaml
apiVersion: image.toolkit.fluxcd.io/v1beta2
kind: ImageRepository
metadata:
  name: perf-lab
  namespace: flux-system
spec:
  image: docker.io/you/perf-lab
  interval: 1m
---
apiVersion: image.toolkit.fluxcd.io/v1beta2
kind: ImagePolicy
metadata:
  name: perf-lab
  namespace: flux-system
spec:
  imageRepositoryRef:
    name: perf-lab
  filterTags:
    pattern: '^main-(?P<ts>[0-9]+)-[a-f0-9]+$'
    extract: '$ts'
  policy:
    numerical:
      order: asc
---
apiVersion: image.toolkit.fluxcd.io/v1beta1
kind: ImageUpdateAutomation
metadata:
  name: perf-lab
  namespace: flux-system
spec:
  interval: 5m
  sourceRef:
    kind: GitRepository
    name: perf-lab
  git:
    checkout:
      ref:
        branch: main
    commit:
      author:
        name: fluxcdbot
        email: fluxcdbot@users.noreply.github.com
      messageTemplate: 'chore: update perf-lab image to {{range .Updated.Images}}{{.}}{{end}}'
    push:
      branch: main
  update:
    strategy: Setters
    path: ./k8s
```

**3. SSH deploy key** — Flux image automation needs push access to your fork to commit tag updates back:

```bash
# Generate a key pair
ssh-keygen -t ed25519 -C "flux-image-automation" -f /tmp/perf-lab-deploy-key -N ""

# Add the public key to your fork: Settings → Deploy keys → Add (check "Allow write access")
cat /tmp/perf-lab-deploy-key.pub

# Store the private key as a secret in flux-system
kubectl create secret generic playground-sre-deploy-key \
  --from-file=identity=/tmp/perf-lab-deploy-key \
  --from-file=identity.pub=/tmp/perf-lab-deploy-key.pub \
  --from-literal=known_hosts="$(ssh-keyscan github.com)" \
  -n flux-system

# Reference the key in the GitRepository (add under spec:)
# secretRef:
#   name: playground-sre-deploy-key
```

Once everything is applied, the loop is: push to `main` → CI builds and pushes image → Flux detects new tag → Flux commits updated tag to `k8s/deployment.yaml` → Flux reconciles the Kustomization → pods roll.

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
