# Tutorials

A hands-on series on application performance in Kubernetes. Each tutorial asks one question,
makes a prediction, runs an experiment, and explains the result. They use the workloads in
[`../workloads/`](../workloads/) and follow the structure in [TEMPLATE.md](TEMPLATE.md).

Start with [00 — Prerequisites](00-prerequisites.md); it says which tier of setup each tutorial needs.

| # | Tutorial | Needs | Status |
|---|---|---|---|
| 00 | [Prerequisites](00-prerequisites.md) | — | ready |
| 01 | [Is my latency the app's fault, or is it queueing?](01-baseline-latency/README.md) | local | ready |
| 02 | CPU limits and throttling | GKE | planned |
| 03 | Fan-out and tail latency (sequential vs concurrent upstreams) | local or GKE | planned |
| 04 | [Where does the CPU time go?](04-profiling/README.md) | local + GCP project | ready (screenshots to be replaced with perf-lab captures) |
| 05 | Noisy neighbours (eBPF run-queue latency) | GKE + node pool | planned; source material in [`workloads/ebpf-noisy-neighbour/docs/`](../workloads/ebpf-noisy-neighbour/docs/) |
| 06 | Tracing the scheduler (bpftrace, EEVDF) | GKE + node pool | planned; see [`demo/bpftrace-snippets.md`](../demo/bpftrace-snippets.md) |

Writing a new one: copy [TEMPLATE.md](TEMPLATE.md) into `NN-short-name/README.md`, keep sample
output in `NN-short-name/results/`, and add a row above.
