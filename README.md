[![Go](https://github.com/olga-mir/playground-sre/actions/workflows/go.yml/badge.svg)](https://github.com/olga-mir/playground-sre/actions/workflows/go.yml)

# playground-sre

A playground for SRE experiments on Kubernetes (GKE): workloads that produce interesting performance behaviour, and tooling to observe it.

## Repository layout

Each workload is a self-contained Go module under `workloads/<name>/` with its own `Dockerfile`, Kubernetes manifests (`k8s/`) and `Taskfile.yaml`. The root `Taskfile.yaml` includes them all, so everything is run as `task <workload>:<task>` (`task --list` shows what's available).

- `workloads/` — the workloads described below.
- `infra/` — supporting monitoring assets (e.g. Cloud Monitoring dashboards).
- `docs/` — cross-cutting docs, e.g. [GitOps with Flux](docs/gitops-flux.md).
- `tutorials/` — hands-on performance tutorials; start at [tutorials/README.md](tutorials/README.md).
- `demo/` — loose write-ups not yet turned into tutorials.
- `stash/` — scratch output from experiment runs (cgroup maps, CPU pinning snapshots, screenshots).

## Workloads

### perf-lab

A Go HTTP server for exploring how a service behaves under load when requests have different characteristics: CPU-bound work, blocking, goroutine fan-out, upstream calls, disk I/O. Each scenario is an endpoint with tunable parameters; point a load generator (Fortio, `hey`, `wrk`) at it and watch the metrics.

Image: `olmigar/perf-lab` · Namespace: `sre`

| Endpoint | What it exercises | Key parameters |
|---|---|---|
| `GET /v1/scenarios/cpu` | SHA-256 hash loop across N goroutines | `duration` (`2s`), `goroutines` (`1`) |
| `GET /v1/scenarios/sleep` | Blocking wait; baseline overhead | `duration` (`100ms`) |
| `GET /v1/scenarios/upstream` | HTTP fan-out to a target URL (concurrent or `sequential=true`) | `n` (`5`), `target`, `timeout` (`5s`), `sequential` |
| `GET /v1/scenarios/disk` | Temp file write + read | `size` (`1mb`), `sync` (`false`) |
| `GET /v1/scenarios/fanout` | Goroutine worker pool with CPU tasks | `workers` (`10`), `task_duration` (`100ms`) |
| `GET /v1/health` | Health check (probes) | — |
| `GET /metrics` | Prometheus metrics | — |

```bash
curl 'http://localhost:8080/v1/scenarios/cpu?duration=3s&goroutines=4'
curl 'http://localhost:8080/v1/scenarios/upstream?n=20'
curl 'http://localhost:8080/v1/scenarios/disk?size=10mb&sync=true'
```

Configuration (environment):

| Variable | Description | Default |
|---|---|---|
| `SERVER_ADDRESS` | Listen address | `:8080` |
| `ENABLE_CLOUDPROFILER` | Enable GCP Cloud Profiler | `false` |
| `GCP_PROJECT_ID` | GCP project (required for profiler) | — |

Run:

```bash
task perf-lab:run     # run locally on :8080
task perf-lab:build
task perf-lab:test
```

**Observability.** Metrics use the OpenTelemetry metric API and are exposed in Prometheus text format on `/metrics`, scraped by GKE Managed Prometheus via the `PodMonitoring` in `k8s/`. Key series: `http_request_duration_seconds`, `http_requests_total`, `http_requests_in_flight`, plus per-scenario counters (`scenario_cpu_hashes_total`, `scenario_upstream_calls_total`, `scenario_disk_bytes_*_total`, `scenario_fanout_hashes_total`). To add traces, wire an OTLP trace exporter in `cmd/api/main.go`; the instrumentation API stays the same. See [tutorials/04-profiling](tutorials/04-profiling/README.md) for Cloud Profiler.

### ebpf-noisy-neighbour

An eBPF-based noisy-neighbour detector. A privileged DaemonSet measures CPU scheduler run-queue latency per cgroup (and PSI pressure) so latency can be attributed to the neighbour causing it. The repo also holds the experiment rig around it: a CPU-hogging "bully", a latency-sensitive "victim" (running the perf-lab image), a load generator, and a debugger pod with bpftrace.

Image: `olmigar/experiment-ebpf` · Namespaces: `ebpf-noisy-neighbour` (collector), `noisy-test` (experiment workloads)

Needs nodes tainted/labelled `workload: noisy-node` and the static CPU manager policy. Write-ups of the experiments are in [workloads/ebpf-noisy-neighbour/docs/](workloads/ebpf-noisy-neighbour/docs/).

More details about this experiment can be found in ["Who is stealing my CPU?" talk](https://olga-mir.github.io/Public-Speaking/2026.11.09__CloudCon-Sydney__Catching-Noisy-Neighbours-with-eBPF/).

## Running the eBPF experiment on your own cluster

No GitOps involved; everything is driven by `task` and `gcloud`.

```bash
export PROJECT_ID=<your-project>
export REGION=<region>                 # Artifact Registry, e.g. australia-southeast1
export CLUSTER_LOCATION=<zone>         # e.g. australia-southeast1-a (needed on first provision)

task ebpf:provision-cluster            # zonal GKE cluster + tainted noisy-node pool
task ebpf:local-build-image            # build and push to your Artifact Registry
task ebpf:deploy-k8s                   # namespace + collector DaemonSet (daemonset-manual.yaml)

task ebpf:deploy-victim DIR=stash      # test workloads go to noisy-test. DIR is temp folder to keep text dumps and cgroup labels
task ebpf:deploy-victim-load DIR=stash
task ebpf:deploy-bully DIR=stash       # or deploy-bully-io / deploy-bully-net

task ebpf:debug:cgroups                # map pods to eBPF cgroup labels
task ebpf:debug:list_cpus DIR=stash    # containers grouped by pinned CPU
task ebpf:deploy-bpftool && task ebpf:debug:dump_runqueue

task ebpf:delete-test-workloads
task ebpf:delete-cluster
```

## Deployment

Merging to `main` builds and pushes images to Docker Hub via GitHub Actions. The shared `apps-dev` cluster picks them up through Flux; see [docs/gitops-flux.md](docs/gitops-flux.md).
For a standalone cluster, build and push your own image and `kubectl apply -k workloads/<name>/k8s` (after updating the `image:` reference).
