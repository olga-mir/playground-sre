# playground-sre

A multi-workload SRE repository. Each workload lives under `workloads/<name>/` with its own `Dockerfile`, `go.mod`, `k8s/`, and `Taskfile.yaml`. The root `Taskfile.yaml` includes each workload's Taskfile under a namespace: `task <workload>:<task>` (`perf-lab:`, `ebpf:`). Run `task --list` for the full set and `tree` for layout. Other top-level dirs: `docs/` (cross-cutting write-ups), `infra/monitoring/` (eBPF dashboard JSON), `tutorials/`, `stash/` (gitignored scratch for debug snapshots and screenshots). Root Taskfile also has `kguardian:*` tasks.

## Workloads

### perf-lab

Go web server (chi router) for exploring HTTP performance under different workload profiles. Exposes `/v1/health`, `/metrics` (Prometheus, with `http_request_duration_seconds` histogram, 1ms–30s buckets), and `/v1/scenarios/*` (cpu, disk, fanout, sleep, upstream). Per-route rate limiting via `golang.org/x/time/rate`; distroless image; 10s graceful shutdown.

**Module:** `github.com/olga-mir/playground-sre/perf-lab`
**Image:** `index.docker.io/olmigar/perf-lab`
**Namespace:** `sre`

| Variable | Description | Default |
|----------|-------------|---------|
| SERVER_ADDRESS | Listen address | :8080 |
| ENABLE_CLOUDPROFILER | Enable GCP Cloud Profiler | false |
| GCP_PROJECT_ID | Profiler project override (auto-detected from metadata server if empty) | (empty) |
| LOG_LEVEL | `debug` enables per-request access logs (too noisy for load tests) | (empty) |
| DISABLE_RATE_LIMIT | Bypass rate limiting; set `true` for load tests | false |

WriteTimeout is deliberately 0: scenario handlers run for caller-controlled `?duration=`.

### ebpf-noisy-neighbour

eBPF-based noisy-neighbour detection tool. Monitors CPU scheduler run-queue latency and PSI metrics via a DaemonSet. Requires privileged pods, `workload: noisy-node` tainted/labelled nodes, and the static CPU Manager policy. `bpf2go` generates Go bindings from `bpf/` C source at build time (clang in the builder stage).

**Module:** `github.com/olga-mir/playground-sre/ebpf-noisy-neighbour`
**Image:** `index.docker.io/olmigar/experiment-ebpf`
**Namespace:** `ebpf-noisy-neighbour` (collector DaemonSet, bpftool, debugger), `noisy-test` (experiment workloads)

Experiment workloads (`bully-*`, `victim-api`, `victim-load`) are deployed ad-hoc to `noisy-test` and are not in `kustomization.yaml`. `psi-collector.yaml` is also excluded (cluster-scoped; applied by `scripts/provision-stand-alone-cluster.sh`).

`victim-api` runs the **perf-lab image** (pinned tag, port 8080; Service :5678 → 8080, `DISABLE_RATE_LIMIT=true`), and `victim-load` (fortio) hits `/v1/scenarios/sleep?duration=10ms`, so request latency can be correlated with runq waits and PSI. Bump the pinned perf-lab tag in `k8s/victim-api.yaml` manually when perf-lab changes.

The **debugger** DaemonSet (`k8s/debugger-pod.yaml`) replaces the old ballast: still pins one exclusive core per test node, and adds bpftrace/tracing tools (`scripts/sched-events.bt`, `viz-scheduler-events.py`). `task ebpf:deploy-debugger`, `task ebpf:debug:debugger-shell`. The debugger is in the Kustomization and `deploy-stack`; `ballast.yaml` and the internal `deploy-ballast` task are now unused leftovers. Experiment write-ups live in `workloads/ebpf-noisy-neighbour/docs/`.

#### Manual testing (no GitOps)

The GitOps path deploys to the shared `apps-dev` cluster wired up in the `playground` repo. For a throwaway cluster you own:

```bash
export PROJECT_ID=<your-project>
export REGION=<your-region>            # e.g. australia-southeast1, used for Artifact Registry
export CLUSTER_LOCATION=<your-zone>    # e.g. australia-southeast1-a, required on first provision

task ebpf:provision-cluster            # zonal GKE cluster + tainted/labelled noisy-node pool (runs get-credentials)
task ebpf:get-credentials              # if the kubeconfig context is missing (auto-detects cluster)

task ebpf:local-build-image            # cross-compile and push to your own Artifact Registry
task ebpf:deploy-stack DIR=stash       # daemonset + node-exporter + monitoring + debugger + victim + bpftool
# or piecemeal:
task ebpf:deploy-monitoring            # PodMonitoring
task ebpf:deploy-node-exporter         # steal-time metrics
task ebpf:deploy-debugger              # exclusive-core pin + bpftrace tooling
task ebpf:deploy-bully DIR=stash       # or deploy-bully-io / deploy-bully-net
task ebpf:deploy-victim DIR=stash
task ebpf:deploy-victim-load DIR=stash
task ebpf:delete-test-workloads        # tear down noisy-test

task ebpf:debug:cgroups                # pods -> eBPF cgroup labels (alias of debug:list_cgroups_labels)
task ebpf:debug:list_cpus DIR=stash    # containers grouped by pinned CPU
task ebpf:deploy-bpftool               # required by debug:dump_runqueue
task ebpf:debug:dump_runqueue          # dump runq_enqueued/runq_histograms BPF maps
task ebpf:debug:bpftool                # interactive shell in bpftool pod

task ebpf:delete-cluster
```

- Every task that deploys pods into `noisy-test` (`deploy-victim`, `deploy-victim-load`, `deploy-bully*`) requires `DIR`; after rollout it snapshots CPU pinning and cgroup labels into `DIR`. `DIR` defaults to `$SRE_REPO_STASH` or `<repo>/stash` for the debug tasks.
- `delete-cluster`/`get-credentials` take the cluster name, `CLUSTER_LOCATION` and `KUBE_CONTEXT` from the live GKE cluster. Export `CLUSTER_LOCATION` only when provisioning fresh or overriding. An exported `CLUSTER_NAME` is ignored on purpose: Task injects that variable into later shells and a stale value would make delete target the wrong cluster.
- `k8s/daemonset-manual.yaml` is the standalone counterpart to `k8s/daemonset.yaml` (templated `${IMAGE}` instead of the Flux-pinned image) and is deliberately excluded from `kustomization.yaml` so it never reaches GitOps.

## GitOps

Deployed to `apps-dev` via Flux; wiring lives in the `playground` repo ([github.com/olga-mir/playground](https://github.com/olga-mir/playground)). Flux Kustomization paths: `./workloads/perf-lab/k8s` (ns `sre`) and `./workloads/ebpf-noisy-neighbour/k8s` (ns `ebpf-noisy-neighbour`).

CI (`.github/workflows/`): `go.yml` builds/tests perf-lab on PRs; `build-push.yml` / `build-push-ebpf.yml` push to Docker Hub on merge to main (stored `DOCKERHUB_USERNAME`/`DOCKERHUB_TOKEN` secrets); `release.yml` is a manual `workflow_dispatch` that tests, tags and creates a GitHub release (it does not build images). Flux's image-reflector-controller polls public Docker Hub, so no in-cluster credential is needed.

Docker Hub rather than Artifact Registry because `ImageUpdateAutomation` writes the resolved image string into `k8s/*.yaml`, and AR paths embed the GCP project ID, which would leak into this public repo. Cost: a manually rotated Docker Hub token. See `docs/flux-image-automation-tradeoff.md` and `docs/gitops-flux.md`.
