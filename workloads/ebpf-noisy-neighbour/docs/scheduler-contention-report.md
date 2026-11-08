# CPU Scheduler Contention: Experiment Report

Results of an experiment that attributes request latency on a shared CPU core to a noisy
neighbour, using application metrics, eBPF run-queue metrics, PSI and a raw kernel scheduler
trace. It also records the checks showing that the EEVDF scheduler (Linux 6.6+) does not remove
the effect.

| | |
|---|---|
| **Cluster** | GKE Standard, zone `australia-southeast1-a` |
| **Node** | `e2-standard-2` (2 vCPU), `cpuManagerPolicy: static` |
| **OS / kernel** | Container-Optimized OS, Linux `6.12.105+` (EEVDF) |
| **Date** | October 2026 |

Related: [tracing scheduler events](tracing-scheduler-events.md) (how to reproduce the trace),
[latency anomaly investigation](latency-anomaly-investigation.md) (measurement pitfalls in the
collector), [debugger pod](DEBUGGER-POD.md).

---

## 1. Summary

- A privileged **`debugger` DaemonSet** (Ubuntu 24.04 with `bpftrace`) replaced the `ballast`
  DaemonSet (`pause` container). Requesting `cpu: "1"` under the static CPU Manager policy gave
  it CPU 1 exclusively, which confined the test workloads (`victim-api`, `bully-hog`) and system
  DaemonSets to CPU 0.
- A 2-second `bpftrace` capture of all `sched_wakeup` / `sched_switch` events on CPU 0 was
  correlated with eBPF run-queue metrics (`ebpf_runq_latency_nanoseconds_*`), PSI
  (`ebpf_psi_pressure_ratio`) and HTTP latency (`http_request_duration_seconds`) from
  `victim-api` (the `perf-lab` image).
- **Findings**
  - 10,858 scheduler events in 2 s on CPU 0 (about 3,450 context switches per second).
  - `victim-api` (`server`) was switched onto CPU 0 3,903 times; 292 of those switches directly
    followed `stress-ng-cpu` (`bully-hog`), with run-queue waits up to 4,113 µs.
  - Cumulatively, `victim-api` waiting behind `bully-hog` was the largest contention pair on the
    node (14,183 wait events).
  - Application metrics showed 12-14 ms responses from a 10 ms sleep endpoint and could not
    explain the extra 2-4 ms. The kernel trace shows the thread was runnable but waiting on the
    run queue while `bully-hog` held the CPU.
- **EEVDF does not remove this contention** (section 5). It does not remove run-queue waits on a
  saturated core and it does not report who caused them.

---

## 2. Topology

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                       Node (2 vCPU, static CPU manager)                      │
├──────────────────────────────────────────────┬───────────────────────────────┤
│                    CPU 0                     │             CPU 1             │
│            (shared contention core)          │       (exclusive reservation) │
├──────────────────────────────────────────────┼───────────────────────────────┤
│ • victim-api (requests: 185m)                │ • debugger pod (requests: 1)  │
│ • bully-hog  (requests: 85m, matrixprod)     │   (replaces ballast;          │
│ • anetd / Cilium agent (requests: 195m)      │    privileged bpftrace tools) │
│ • fluentbit, gke-metadata, collectors        │                               │
└──────────────────────────────────────────────┴───────────────────────────────┘
```

CPU pinning was checked with `task ebpf:debug:list_cpus`:

- CPU 1: 1 core claimed by `ebpf-noisy-neighbour/debugger`.
- CPU 0: 824m of requests shared between `noisy-test/bully-hog`, `noisy-test/victim-api` and
  system daemons.

---

## 3. Kernel scheduler trace (bpftrace)

A 2-second trace of CPU 0 was captured with [`scripts/sched-events.bt`](../scripts/sched-events.bt)
into [`assets/sched-events.jsonl`](../assets/sched-events.jsonl) (10,858 events, 1.6 MB).

| Measure | Count |
|---|---|
| Total events | 10,858 |
| `sched_wakeup` | 3,961 |
| `sched_switch` | 6,897 (about 3,448 per second) |
| Unique tasks | 52 |

### Switches and wake-up latency by task on CPU 0

| Task (`comm`) | Switches | With wake latency | p50 | p95 | p99 | Max |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: |
| `server` (`victim-api`) | 3,903 | 2,438 | 12 µs | 237 µs | 1,290 µs | 4,113 µs |
| `swapper/0` (idle) | 1,058 | 0 | - | - | - | - |
| `cilium-agent` | 481 | 468 | 19 µs | 755 µs | 1,683 µs | 3,009 µs |
| `stress-ng-cpu` (`bully-hog`) | 394 | 1 | 61 µs | 61 µs | 61 µs | 61 µs |
| `cilium-cni` | 270 | 104 | 13 µs | 421 µs | 1,482 µs | 10,930 µs |
| `kubelet` | 173 | 139 | 13 µs | 191 µs | 480 µs | 642 µs |
| `containerd` | 161 | 144 | 12 µs | 103 µs | 173 µs | 1,445 µs |
| `otelsvc` | 118 | 105 | 18 µs | 1,559 µs | 2,728 µs | 4,465 µs |
| `prometheus` (GMP) | 91 | 87 | 21 µs | 986 µs | 3,029 µs | 3,029 µs |
| `flb-pipeline` (Fluent Bit) | 85 | 63 | 19 µs | 440 µs | 1,958 µs | 1,958 µs |

### Context-switch transitions (who ran before whom)

| Next task | Previous task | Switches in 2 s | Interpretation |
| :--- | :--- | :---: | :--- |
| `server` | `server` | 2,427 | Switching between the victim's own threads |
| `swapper/0` | `server` | 633 | Victim goes to sleep (waiting on I/O or the 10 ms timer) |
| `server` | `swapper/0` | 609 | Core was idle when the victim woke, so it ran immediately |
| `stress-ng-cpu` | `server` | 299 | Bully takes the CPU as soon as the victim yields |
| `server` | `stress-ng-cpu` | 292 | Victim runs after waiting behind the bully |
| `server` | `cilium-agent` | 190 | Packet received; Cilium hands off to the server |
| `cilium-agent` | `swapper/0` | 165 | Packet arrives while the core was idle |
| `cilium-agent` | `server` | 153 | Server sends the response; hands off to Cilium |
| `cilium-cni` | `server` | 152 | Network filtering / policy evaluation |

---

## 4. Correlation across layers

```text
Layer 1: Application / OTel
  • GET /v1/scenarios/sleep?duration=10ms
  • Observed duration: ~12.5-14.2 ms
  • Reading: the endpoint took ~3 ms longer than the sleep. Network or application?

Layer 2: Linux PSI
  • ebpf_psi_pressure_ratio{resource="cpu", kind="some", window="avg10"} = 33.04%
  • Reading: tasks on the node were stalled waiting for a CPU 33% of wall time.

Layer 3: eBPF aggregated metrics (ebpf-ds)
  • ebpf_runq_latency_nanoseconds_count: 14,183 waits (victim-api waiting behind bully-hog)
  • runq_histograms: p50 = 1.6 ms, p95 / p99 = 3.8 ms
  • Reading: attributed to a specific neighbour; the delay is run-queue wait time.

Layer 4: kernel event trace (bpftrace)
  • sched_wakeup at t = 1.005 ms: server woke up (prio 120)
  • sched_switch at t = 3.125 ms: stress-ng ran for 2.120 ms before server switched in
  • wake_latency_us = 2,120: the thread waited 2.12 ms on the EEVDF run queue
  • Reading: this specific thread waited 2.12 ms behind bully PID 7083.
```

Layers 1 and 2 show that latency exists and that the node is CPU-starved. Only layers 3 and 4
say which workload caused the wait.

### Artifacts

1. **Raw trace:** [`assets/sched-events.jsonl`](../assets/sched-events.jsonl).
2. **Interactive timeline:** [`assets/timeline.html`](../assets/timeline.html), generated by
   [`scripts/viz-scheduler-events.py`](../scripts/viz-scheduler-events.py). It shows execution
   blocks for all 52 tasks across the 2,000 ms window; hovering a block shows `wake_latency_us`
   and the previous process.
3. **Dashboard:** [`infra/monitoring/ebpf-dashboard.json`](../../../infra/monitoring/ebpf-dashboard.json)
   (starved-workload HTTP latency p50/p95/p99, request rate and errors, run-queue latency and
   wait counts by waiter and previous cgroup, PSI, steal time and CFS throttling).

---

## 5. Does EEVDF change the result?

Linux 6.6 replaced the CFS fair-scheduling core with EEVDF (Earliest Eligible Virtual Deadline
First). Because EEVDF takes latency sensitivity into account, it is sometimes assumed to prevent
noisy-neighbour starvation. The checks and arguments below show that it does not.

### 5.1 EEVDF is active on the test node

Verified from a privileged pod (`bpftool-ds`) on a test-pool node.

**Kernel version**

```bash
$ uname -r
6.12.105+
```

**Symbols** (`/proc/kallsyms`)

```text
ffffffffb712eaa0 T entity_eligible
ffffffffb7139a60 t pick_eevdf
ffffffffb7138cd0 t place_entity
ffffffffb71378f0 t update_curr_fair
```

- `entity_eligible` checks whether an entity's virtual runtime is eligible to run
  ($v_i \le V$, where $V$ is the run queue's virtual time).
- `pick_eevdf` selects, among eligible entities, the one with the earliest virtual deadline
  ($d_i = v_i + \frac{q_i}{w_i}$).

**Sysctls** (`/proc/sys/kernel/sched*`). The CFS latency tunables (`sched_latency_ns`,
`sched_min_granularity_ns`, `sched_wakeup_granularity_ns`) are absent. Remaining entries:

```text
sched_cfs_bandwidth_slice_us   sched_deadline_period_max_us   sched_deadline_period_min_us
sched_rr_timeslice_ms          sched_rt_period_us             sched_rt_runtime_us
sched_schedstats
```

`sched_cfs_bandwidth_slice_us` belongs to cgroup bandwidth throttling, which is unchanged.

**Kernel config** (`/proc/config.gz`)

```text
CONFIG_FAIR_GROUP_SCHED=y
CONFIG_CGROUP_SCHED=y
CONFIG_SCHED_CLASS_EXT=y
# CONFIG_SCHED_DEBUG is not set
CONFIG_SCHED_INFO=y
CONFIG_SCHEDSTATS=y
```

Linux 6.12 is well past 6.6, where EEVDF was merged, and there is no option to switch back to
CFS.

### 5.2 Why EEVDF does not remove the contention

1. **Contention cannot be scheduled away.** A core executes one instruction stream at a time.
   If `bully-hog` is running when `victim-api` wakes, one of them waits. The time between
   `sched_wakeup` and `sched_switch` is the run-queue latency.
2. **Containers use the default time slice.** The deadline is $d_i = v_i + q_i / w_i$, with
   $v_i$ the virtual runtime, $q_i$ the slice (`base_slice_ns`, typically 0.7-3 ms) and $w_i$ the
   task weight (derived from CPU shares/requests). Kubernetes containers do not call
   `sched_setattr()` to request a custom slice, so all tasks run with the same $q_i$ and priority
   between eligible tasks is governed mainly by weight. A bully with equal or higher weight is as
   eligible as the victim.
3. **Preemption is not instantaneous.** Even when a waking task has an earlier deadline,
   preemption takes effect at scheduling points (wake-up checks, ticks). On a saturated core with
   frequent thread churn, context-switch overhead and queueing still delay the victim.
4. **Observed on this setup.** In an earlier run on the same 6.12 cluster (output not committed
   to the repository), `cpu.pressure` rose during the bully run and fell as soon as the bully was
   scaled to zero. The trace in section 3 shows the same effect at the level of individual
   wake-ups.
5. **Attribution is out of scope for the scheduler.** EEVDF does not export who preempted whom,
   which neighbour causes tail latency, or per-cgroup run-queue latency histograms. PSI signals
   that starvation is happening but not who causes it. Attribution requires instrumentation of
   `sched_wakeup` / `sched_switch`, which is what the eBPF collector in this repository does.

---

## 6. Conclusions

- Application-level tracing (OTel) cannot see scheduler delay: a thread waiting on the run queue
  is still inside its span. eBPF run-queue metrics and kernel scheduler traces report both the
  duration of the wait and the task that held the CPU.
- A privileged diagnostic DaemonSet that holds one exclusive core keeps the CPU pinning layout
  intact (one exclusive core, one shared core) and provides `bpftrace`, `perf` and scheduler
  tracepoints without needing ephemeral debug pods on disk-constrained nodes. The trade-offs are
  in [DEBUGGER-POD.md](DEBUGGER-POD.md).
- The effect holds on a current kernel (6.12, EEVDF). A newer scheduler changes how the wait is
  distributed between tasks, not whether it exists.
- Magnitudes in this report are milliseconds. Earlier multi-second readings were measurement
  artifacts (histogram clamping and a collector bug) and are covered in the
  [latency anomaly investigation](latency-anomaly-investigation.md).
