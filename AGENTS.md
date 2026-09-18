# playground-sre

A multi-workload SRE repository. Each workload lives under `workloads/<name>/` with its own `Dockerfile`, `go.mod`, `k8s/`, and `Taskfile.yaml`. The root `Taskfile.yaml` dispatches to workloads via `task <workload>:<task>`.

## Workloads

### perf-lab

A Go web server skeleton for exploring HTTP performance under different workload profiles.

```
workloads/perf-lab/
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
│   ├── deployment.yaml   # 1-replica deployment
│   ├── service.yaml
│   ├── podmonitoring.yaml
│   ├── serviceaccount.yaml
│   └── kustomization.yaml
├── Dockerfile
└── Taskfile.yaml
```

**Module:** `github.com/olga-mir/playground-sre/perf-lab`
**Image:** `index.docker.io/olmigar/perf-lab`
**Namespace:** `sre`

| Variable | Description | Default |
|----------|-------------|----------|
| SERVER_ADDRESS | Listen address | :8080 |
| ENABLE_CLOUDPROFILER | Enable GCP Cloud Profiler | false |
| GCP_PROJECT_ID | GCP project for profiler | (empty) |

### ebpf-noisy-neighbour

eBPF-based noisy-neighbour detection tool. Monitors CPU scheduler run-queue latency and PSI metrics via a DaemonSet. Requires privileged pods and `workload: noisy-node` tainted nodes.

```
workloads/ebpf-noisy-neighbour/
├── bpf/                  # eBPF C source (compiled with clang/bpf2go)
├── k8s/
│   ├── namespace.yaml
│   ├── daemonset.yaml    # Privileged DaemonSet, noisy-node taint
│   ├── pod-monitoring.yaml
│   ├── node-exporter.yaml
│   ├── bpftool-daemonset.yaml
│   └── kustomization.yaml
├── main.go
├── debug.go
├── psi.go
├── Dockerfile            # Multi-stage: golang+clang builder → ubuntu runtime
└── Taskfile.yaml
```

**Module:** `github.com/olga-mir/playground-sre/ebpf-noisy-neighbour`
**Image:** `index.docker.io/olmigar/experiment-ebpf`
**Namespace:** `ebpf-noisy-neighbour`

Experiment-time workloads (bully, ballast, victim) are in `k8s/` but deployed ad-hoc with `task ebpf:deploy-bully` — not managed by GitOps Kustomization.

## Task dispatch

```bash
task perf-lab:build
task perf-lab:docker-build-push
task ebpf:deploy-k8s
task ebpf:deploy-monitoring
task ebpf:deploy-bully
```

## GitOps

Deployed to the `apps-dev` cluster via Flux. The `playground` repo ([github.com/olga-mir/playground](https://github.com/olga-mir/playground)) holds the Flux wiring:

| Workload | Flux Kustomization path | Namespace | Image |
|---|---|---|---|
| perf-lab | `./workloads/perf-lab/k8s` | `sre` | `olmigar/perf-lab` |
| ebpf-noisy-neighbour | `./workloads/ebpf-noisy-neighbour/k8s` | `ebpf-noisy-neighbour` | `olmigar/experiment-ebpf` |

## Architecture notes

- **Chi router** (perf-lab) — lightweight, stdlib-compatible
- **Rate limiting** (perf-lab) — per-endpoint via `golang.org/x/time/rate`
- **Distroless image** (perf-lab) — minimal attack surface
- **eBPF via cilium/ebpf** — `bpf2go` generates Go bindings from C source at build time (requires clang in builder)
- **Graceful shutdown** (perf-lab) — 10-second drain window on SIGINT/SIGTERM
