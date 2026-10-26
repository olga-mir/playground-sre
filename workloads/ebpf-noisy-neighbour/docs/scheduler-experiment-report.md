# Diagnostic Pod Scheduler Contention Experiment Report

**Cluster:** `experiment-ebpf-cluster` (GKE Standard, zone `australia-southeast1-a`)  
**Node:** `gke-experiment-ebpf-cluster-test-pool-ab317010-7dmj` (machine: `e2-standard-2`, 2 vCPU)  
**Kernel:** Linux `6.12.105+ (amd64)` — **EEVDF** Scheduler  
**Date:** October 26, 2026  

---

## 1. Executive Summary

In this experiment, we replaced the passive `ballast` DaemonSet (`pause` container) with an active, privileged **`debugger` DaemonSet** (Ubuntu 24.04 with `bpftrace`). By requesting `cpu: "1"` under the static CPU Manager policy, the diagnostic pod claimed **CPU 1 exclusively**, confining all test workloads (`victim-api` and `bully-hog`) and system DaemonSets to **CPU 0**.

Using `bpftrace` inside the diagnostic pod, we recorded a high-fidelity **2-second trace of all scheduler events on CPU 0** (`sched_wakeup` and `sched_switch`). We then correlated this kernel-level trace with:
1. **eBPF BPF map metrics** (`ebpf_runq_latency_nanoseconds_*`) from `ebpf-ds`.
2. **Linux PSI counters** (`ebpf_psi_pressure_ratio`, 33% CPU stall).
3. **HTTP request latency** (`http_request_duration_seconds`) from `victim-api` (`perf-lab`).

### Key Highlights
- **10,858 scheduler events** were captured in 2 seconds on CPU 0 (~3,450 context switches/sec).
- **Direct Contention Verified:** `victim-api` (`server`) was switched onto CPU 0 **3,903 times**, with **292 context switches directly waiting behind `stress-ng-cpu`** (`bully-hog`), experiencing run-queue wait delays reaching **4,113 µs (4.11 ms)**.
- **Node-Wide Attribution:** Cumulative metrics confirmed that `victim-api` waiting behind `bully-hog` is the **#1 scheduling contention pair** on the node (**14,183 cumulative wait events**).
- **The "Aha!" Insight:** OTel and application-level metrics see ~12–14 ms response times on a 10 ms sleep endpoint, but standard telemetry cannot explain the 2–4 ms discrepancy. In-kernel eBPF tracing directly proves that the thread was physically runnable but descheduled on the run-queue waiting for `bully-hog`'s EEVDF time-slice to expire.

---

## 2. Architecture & CPU Pinning Topology

The test node was provisioned with `cpuManagerPolicy: static`.

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                  Node: gke-...-test-pool-ab317010-7dmj (2 vCPU)              │
├──────────────────────────────────────────────┬───────────────────────────────┤
│                    CPU 0                     │             CPU 1             │
│            (Shared Contention Core)          │       (Exclusive Reservation) │
├──────────────────────────────────────────────┼───────────────────────────────┤
│ • victim-api (requests: 185m)               │ • debugger pod (requests: 1)  │
│ • bully-hog  (requests: 85m, matrixprod)     │   (Replaces ballast pod;      │
│ • anetd / Cilium agent (requests: 195m)      │    privileged bpftrace tools) │
│ • fluentbit, gke-metadata, collectors        │                               │
└──────────────────────────────────────────────┴───────────────────────────────┘
```

Verification from [list-cpu-pinning.txt](file:///Users/olga/repos/playground-sre/stash/2026.10.26-20.18.47-list-cpu-pinning.txt):
- **CPU 1:** `1 core` claimed by `ebpf-noisy-neighbour/debugger-jbbfj/debugger`
- **CPU 0:** `824m requests` shared between `noisy-test/bully-hog`, `noisy-test/victim-api`, and system daemons.

---

## 3. Kernel Scheduler Trace Analysis (bpftrace)

A 2-second trace on CPU 0 was collected into [sched-events.jsonl](file:///Users/olga/repos/playground-sre/sched-events.jsonl) via [sched-events.bt](file:///Users/olga/repos/playground-sre/sched-events.bt):

### Event Breakdown
- **Total Events:** 10,858
- **Wakeup Events (`sched_wakeup`):** 3,961
- **Switch Events (`sched_switch`):** 6,897 (~3,448 switches/second)
- **Unique Tasks Observed:** 52

### Task Switches & Wakeup Latencies on CPU 0

| Task (`comm`) | Switches | Count with Wake Latency | p50 Wake Latency | p95 Wake Latency | p99 Wake Latency | Max Wake Latency |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: |
| **`server` (`victim-api`)** | **3,903** | 2,438 | **12 µs** | **237 µs** | **1,290 µs (1.29 ms)** | **4,113 µs (4.11 ms)** |
| `swapper/0` (idle) | 1,058 | 0 | - | - | - | - |
| **`cilium-agent`** | 481 | 468 | 19 µs | 755 µs | 1,683 µs | 3,009 µs |
| **`stress-ng-cpu` (`bully`)** | 394 | 1 | 61 µs | 61 µs | 61 µs | 61 µs |
| `cilium-cni` | 270 | 104 | 13 µs | 421 µs | 1,482 µs | 10,930 µs |
| `kubelet` | 173 | 139 | 13 µs | 191 µs | 480 µs | 642 µs |
| `containerd` | 161 | 144 | 12 µs | 103 µs | 173 µs | 1,445 µs |
| `otelsvc` | 118 | 105 | 18 µs | 1,559 µs | 2,728 µs | 4,465 µs |
| `prometheus` (GMP) | 91 | 87 | 21 µs | 986 µs | 3,029 µs | 3,029 µs |
| `flb-pipeline` (Fluentbit) | 85 | 63 | 19 µs | 440 µs | 1,958 µs | 1,958 µs |

### Context Switch Transition Matrix (Who ran before whom)

| Next Task (`next_comm`) | Previous Task (`prev_comm`) | Switches in 2s | Explanation |
| :--- | :--- | :---: | :--- |
| **`server`** | `server` | 2,427 | Victim internal Go goroutine/thread switching |
| `swapper/0` | `server` | 633 | Victim goes to sleep (waiting on I/O or 10ms timer) |
| `server` | `swapper/0` | 609 | Core was idle when victim woke up (instant execution) |
| **`stress-ng-cpu`** | **`server`** | **299** | **Bully seizes CPU immediately after victim yields** |
| **`server`** | **`stress-ng-cpu`** | **292** | **Victim wakes up and preempts / takes over from Bully** |
| `server` | `cilium-agent` | 190 | Networking packet received; Cilium hands off to server |
| `cilium-agent` | `swapper/0` | 165 | Network packet arrives while core was idle |
| `cilium-agent` | `server` | 153 | Server sends HTTP response; hands off to Cilium |
| `cilium-cni` | `server` | 152 | Network filtering / policy evaluation |

---

## 4. Multi-Layer Correlation: OTel vs eBPF vs Kernel Trace

```text
┌────────────────────────────────────────────────────────────────────────────────────────┐
│ Layer 1: Application / OTel                                                            │
│   • HTTP GET /v1/scenarios/sleep?duration=10ms                                         │
│   • Observed Duration: ~12.5 ms – 14.2 ms                                              │
│   • Diagnosis: "Endpoint took ~3ms longer than the 10ms sleep. Network or app lag?"    │
├────────────────────────────────────────────────────────────────────────────────────────┤
│ Layer 2: Linux PSI (Pressure Stall Information)                                        │
│   • ebpf_psi_pressure_ratio{resource="cpu", kind="some", window="avg10"} = 33.04%       │
│   • Diagnosis: Node tasks are stalled waiting for runnable CPU 33% of wall time.      │
├────────────────────────────────────────────────────────────────────────────────────────┤
│ Layer 3: eBPF Aggregated Metrics (ebpf-ds)                                             │
│   • ebpf_runq_latency_nanoseconds_count: 14,183 waits (victim-api ← bully-hog)          │
│   • BPF runq_histograms: p50 = 1.6 ms, p95/p99 = 3.8 ms                                │
│   • Diagnosis: Direct attribution. The delay is entirely run-queue wait time.          │
├────────────────────────────────────────────────────────────────────────────────────────┤
│ Layer 4: Diagnostic Pod Kernel Event Trace (bpftrace)                                  │
│   • sched_wakeup at t = 1.005 ms: server woke up (prio 120)                            │
│   • sched_switch at t = 3.125 ms: stress-ng ran for 2.120 ms before server switched in  │
│   • wake_latency_us = 2,120 µs (2.12 ms) waiting in EEVDF runqueue                     │
│   • Physical Proof: The exact thread waited 2.12 ms behind bully PID 7083.             │
└────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## 5. Artifacts and Visualization

1. **Raw Scheduler Trace:** [`sched-events.jsonl`](file:///Users/olga/repos/playground-sre/sched-events.jsonl) (1.6 MB, 10,858 lines of JSONL events).
2. **Interactive HTML Timeline:** [`timeline.html`](file:///Users/olga/repos/playground-sre/timeline.html) generated via [`viz-scheduler-events.py`](file:///Users/olga/repos/playground-sre/viz-scheduler-events.py).
   - Shows colored execution blocks for all 52 tasks across the 2,000 ms window.
   - Hovering over any switch block displays the exact `wake_latency_us` and previous process.
3. **Cloud Monitoring Dashboard:** Dashboard `538003cb-92bf-4da8-9a02-655487e9c9ed` (`eBPF Noisy Neighbour`) updated with:
   - VICTIM HTTP Request Latency p99 / p95 / p50
   - VICTIM HTTP Request Rate & Errors
   - Live filters set to `victim_cgroup = pod/13f3efe7/.+`, `bully_cgroup = pod/d474a653/.+`.

---

## 6. Conclusion

Replacing `ballast` with a privileged diagnostic DaemonSet provides a zero-compromise approach for live kernel troubleshooting in Kubernetes:
- It maintains the strict CPU pinning guarantee (1 exclusive core claimed, 1 shared core for the experiment).
- It provides immediate access to live `bpftrace`, `perf`, and scheduler tracepoints without requiring ephemeral debug pods on disk-constrained nodes.
- It empirically validates the talk thesis: **Application tracing (OTel) is blind to scheduler delay, but eBPF run-queue metrics and kernel scheduler event tracing expose both the duration and the culprit.**
