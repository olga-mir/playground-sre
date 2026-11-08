# 00 — Prerequisites

Every tutorial states which of these tiers it needs. Set up only as far as you intend to follow.

| Tier | Needed for | What you need |
|---|---|---|
| **Local** | 01 | Go, [Task](https://taskfile.dev), [Fortio](https://github.com/fortio/fortio) |
| **GCP project** | 04 (Cloud Profiler) | A GCP project with the Cloud Profiler API enabled and credentials that can write to it |
| **GKE** | Kubernetes-level tutorials | A GKE cluster with Managed Prometheus, `kubectl`, `gcloud` |
| **GKE + node pool** | eBPF tutorials | The tainted `workload: noisy-node` node pool with the static CPU manager policy; privileged pods allowed |

## Local tools

```bash
go version          # see workloads/perf-lab/go.mod for the minimum
task --version
fortio version      # brew install fortio
```

## GCP

```bash
export PROJECT_ID=<your-project>
export REGION=<region>                  # e.g. australia-southeast1
export CLUSTER_LOCATION=<zone>          # e.g. australia-southeast1-a

gcloud auth login
gcloud auth application-default login  # used by Cloud Profiler when running locally
gcloud config set project "$PROJECT_ID"
```

## GKE cluster

The eBPF experiment ships a provisioning task that creates a zonal cluster plus the tainted node
pool. It is also fine as a general-purpose cluster for the other tutorials:

```bash
task ebpf:provision-cluster
task ebpf:get-credentials      # only if the kubeconfig context is missing
```

Tear down when you are finished; a cluster left running is the only real cost in this repo:

```bash
task ebpf:delete-cluster
```

## Images

Tutorials use the published `olmigar/perf-lab` image by default. To run your own build, see
[`docs/gitops-flux.md`](../docs/gitops-flux.md) (forks) or `task ebpf:local-build-image`.

## Conventions used in tutorials

- Commands run from the repository root unless stated.
- `task <workload>:<task>` is preferred over raw `kubectl`; `task --list` shows what exists.
- Scratch output goes to `stash/` (gitignored). Output worth keeping is copied into the tutorial's
  `results/` directory.
