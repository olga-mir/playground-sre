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

#### Manual testing (no GitOps)

The GitOps path deploys to the shared `apps-dev` cluster wired up in the `playground` repo. For a throwaway cluster you own, spin one up directly with gcloud:

```bash
export PROJECT_ID=<your-project>
export REGION=<your-region>            # e.g. australia-southeast1, used for Artifact Registry
export CLUSTER_LOCATION=<your-zone>    # e.g. australia-southeast1-a, required on first provision

task ebpf:provision-cluster            # zonal GKE cluster + tainted/labelled noisy-node pool
task ebpf:local-build-image            # cross-compiles and pushes to your own Artifact Registry repo
task ebpf:deploy-k8s                   # applies namespace.yaml + k8s/daemonset-manual.yaml (envsubst'd image, not the Flux-pinned one)
task ebpf:deploy-monitoring            # optional: PodMonitoring
task ebpf:deploy-node-exporter         # optional: node-exporter for steal-time metrics
task ebpf:deploy-ballast               # optional: pin 1 exclusive core per test node
task ebpf:deploy-bully                 # or deploy-bully-io / deploy-bully-net
task ebpf:deploy-victim                # optional: test workload to observe from the bully

task ebpf:list-pod-cgroups             # map pods -> eBPF cgroup labels
task ebpf:dump-runq-enqueued           # dump runq_enqueued/runq_histograms BPF maps (needs bpftool-daemonset)

task ebpf:delete-cluster
```

`k8s/daemonset-manual.yaml` is the standalone counterpart to `k8s/daemonset.yaml` — same pod spec, but templated with `${IMAGE}` instead of the Flux-pinned Docker Hub image, and deliberately excluded from `kustomization.yaml` so it never leaks into GitOps.

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

CI pushes to Docker Hub using a stored `DOCKERHUB_USERNAME`/`DOCKERHUB_TOKEN` (GitHub repo secrets) — see `.github/workflows/build-push.yml` and `build-push-ebpf.yml`. Flux's `image-reflector-controller` polls the public Docker Hub API for new tags, no in-cluster credential needed. Docker Hub was chosen over Artifact Registry specifically because it keeps Flux's `ImageUpdateAutomation` working as designed: AR image paths always embed the GCP project ID (`<region>-docker.pkg.dev/<project-id>/...`), and `ImageUpdateAutomation` writes the fully-resolved image string straight into `k8s/*.yaml` on every tag bump — which would have leaked the project ID into this public repo on the first automated promotion. See `docs/flux-image-automation-tradeoff.md` for the full trade-off writeup. The cost of this choice is a manually-rotated Docker Hub token in GitHub secrets instead of keyless WIF.

## Architecture notes

- **Chi router** (perf-lab) — lightweight, stdlib-compatible
- **Rate limiting** (perf-lab) — per-endpoint via `golang.org/x/time/rate`
- **Distroless image** (perf-lab) — minimal attack surface
- **eBPF via cilium/ebpf** — `bpf2go` generates Go bindings from C source at build time (requires clang in builder)
- **Graceful shutdown** (perf-lab) — 10-second drain window on SIGINT/SIGTERM
