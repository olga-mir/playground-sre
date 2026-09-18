# Proposal: Non-CPU Noisy Neighbor Detection Vectors

## Overview
While measuring CPU Run-Queue latency via eBPF tracepoints (`sched_wakeup` / `sched_switch`) accurately reveals CPU scheduling contention on shared Kubernetes nodes, CPU is only one of several resources where workload pods interfere with each other. 

This proposal documents additional **non-CPU noisy neighbor vectors** (Memory, Disk I/O, Network, Kernel locks), their mechanisms, impact, and potential future eBPF / telemetry instrumentation strategies.

---

## 1. Memory Subsystem Contention

### A. Last-Level Cache (LLC) & Memory Bandwidth Saturation
* **Mechanism:** Modern CPU architectures share Last-Level Cache (L3 Cache) and main memory channels across CPU cores on the same NUMA socket. A memory-intensive pod executing large array scans or pointer-chasing workloads will evict cache lines belonging to neighbor pods and saturate interconnect memory bandwidth.
* **Symptom:** Neighbor pods experience significant execution slowdowns (increased CPU cycles per instruction / higher IPC degradation) without any increase in CPU run-queue latency or CPU throttling metrics.
* **Instrumentation Strategy:**
  * Use Linux `perf_event` hardware counters via eBPF (`perf_event_open` attached to `LLC-loads`, `LLC-load-misses`).
  * Integrate Intel RDT (Resource Director Technology) / AMD PQoS metrics to monitor LLC occupancy (`mbm_total_bytes`, `cmt_bytes`) per cgroup.

### B. Memory Reclaim & Page Allocation Delays
* **Mechanism:** When node memory usage approaches limits, kernel background threads (`kswapd`) or direct memory reclaims stall memory allocations (`alloc_pages`). A pod triggering heavy page allocations forces synchronous memory reclaims across the node.
* **Symptom:** Latency spikes in memory allocation paths across all workloads on the node.
* **Instrumentation Strategy:**
  * Attach eBPF kprobes to kernel memory allocation functions: `mm_vmscan_direct_reclaim_begin` and `mm_compaction_begin`.
  * Track duration spent in direct reclaim per cgroup.

---

## 2. Block I/O and Page Cache Contention

### A. Shared Storage & Page Cache Flushing
* **Mechanism:** Pods performing heavy unbuffered disk writes fill the OS page cache. When the kernel dirty memory thresholds (`vm.dirty_ratio`, `vm.dirty_background_ratio`) are hit, kernel threads (`wb_workfn`) or user threads are forced to flush dirty pages synchronously.
* **Symptom:** I/O queue buildup (`iowait`), filesystem lock delays, and latency spikes in applications making simple file reads or log writes.
* **Instrumentation Strategy:**
  * Trace `block_rq_insert` and `block_rq_issue` / `block_rq_complete` tracepoints with eBPF to measure per-cgroup I/O wait times and queue depth.
  * Measure latency in VFS layer (`vfs_read`, `vfs_write`) grouped by cgroup.

---

## 3. Network Stack & SoftIRQ Contention

### A. NIC Rx/Tx Queue & SoftIRQ (`ksoftirqd`) Saturation
* **Mechanism:** High packet rate workloads (e.g., small packet ingress/egress, reverse proxies) generate excessive hardware interrupts handled by SoftIRQ (`NET_RX_SOFTIRQ`). Because SoftIRQs execute with high priority on shared CPU cores, they starve user-space workload threads.
* **Symptom:** Packet drops, TCP retransmissions, and high system CPU usage (`%sys` / `%soft`) without corresponding user CPU usage (`%user`).
* **Instrumentation Strategy:**
  * Trace `irq/softirq_entry` and `irq/softirq_exit` for `vec=3 (NET_RX)` and `vec=2 (NET_TX)`.
  * Measure time spent in SoftIRQ handling per CPU core and attribute network traffic to cgroups via eBPF socket filters (`tc` or `cgroup_skb`).

---

## 4. Kernel Lock & Subsystem Contention

### A. Filesystem and VFS Lock Contention
* **Mechanism:** Heavy metadata operations (e.g., creating/deleting millions of tiny temp files, directory listings) take shared kernel locks (e.g., `dcache` locks, inode locks).
* **Symptom:** Workload threads entering uninterruptible sleep state (`D` state) waiting for kernel mutexes.
* **Instrumentation Strategy:**
  * Monitor task state changes via `sched_switch` where `prev_state == TASK_UNINTERRUPTIBLE`.
  * Measure duration tasks spend in `D` state and record kernel call stacks (`bpf_get_stackid`).

---

## 5. Linux Pressure Stall Information (PSI) Integration

### A. Node & Cgroup PSI Telemetry
* **Mechanism:** Linux kernel PSI tracks the loss of throughput caused by resource shortages across CPU, Memory, and I/O. It categorizes pressure into `some` (at least one task delayed) and `full` (all tasks delayed).
* **Symptom:** Provides an early-warning signal for node/cgroup resource exhaustion before OOMs or severe SLA violations occur.
* **Instrumentation Strategy:**
  * Expose `/proc/pressure/{cpu,memory,io}` and cgroup-level `/sys/fs/cgroup/.../memory.pressure` metrics into Prometheus alongside eBPF scheduling metrics.

### B. Implemented

`psi.go` (`psiCollector`, a `prometheus.Collector` registered from `main()` on the line
right after `runqCollector`) parses the kernel's plain-text pressure files on every
scrape — pure userspace, no BPF:

* **Node** — `${PSI_PROC_PATH}/{cpu,io,memory}`. `PSI_PROC_PATH` defaults to
  `/proc/pressure`; `k8s/ebpf-daemonset.yaml` hostPath-mounts the host's `/proc/pressure`
  at `/host/proc/pressure` and sets the env var (the pod's own `/proc` is the container's,
  not the node's). A missing `full` line on `/proc/pressure/cpu` is tolerated.
* **Per-cgroup** — `<cgroup>/{cpu,io,memory}.pressure` under the already-mounted read-only
  `/sys/fs/cgroup`, restricted to `pod/*` cgroups only (the same
  `strings.HasPrefix(cgroup, "pod/")` guard `runqCollector` uses), labels resolved through
  the shared `cgroupMapper` / `cgroupLabel`.

Metrics:

* `ebpf_psi_pressure_ratio{resource="cpu|io|memory", kind="some|full", window="avg10|avg60|avg300", scope="node|cgroup", cgroup=""|"pod/<uid>/<cid>"}`
  — gauge, the kernel's `avgN` stall percentage (0–100).
* `ebpf_psi_stall_seconds_total{resource, kind, scope, cgroup}` — counter, from the
  `total=` field (µs → s).

Pairs with the Track B stressors `k8s/bully-io.yaml` / `k8s/bully-net.yaml`
(`task deploy-bully-io` / `task deploy-bully-net`): during the disk run, watch
`ebpf_psi_pressure_ratio{resource="io"}` climb on both `scope="node"` and the stressor's
`scope="cgroup"` series.

---

## Summary Roadmap Matrix

| Vector | Driver | Kernel/eBPF Probe Target | Complexity |
|---|---|---|---|
| **CPU Scheduling** *(Current Scope)* | Run-Queue Latency | `tp_btf/sched_wakeup`, `tp_btf/sched_switch` | Implemented |
| **Memory LLC / Bandwidth** | L3 Cache / RAM bus | `perf_event_open` (LLC misses), Intel RDT | High |
| **Memory Reclaim** | `alloc_pages` / `kswapd` | `kprobe/mm_vmscan_direct_reclaim_begin` | Medium |
| **Disk I/O** | Page cache flushes, block queues | `tp/block_rq_issue`, `tp/block_rq_complete` | Medium |
| **Network SoftIRQ** | SoftIRQ packet processing | `tp/softirq_entry`, `tp/softirq_exit` | Medium |
| **PSI Telemetry** | System & Cgroup pressure | `/proc/pressure/*`, `/sys/fs/cgroup/*/*.pressure` | **Implemented** (`psi.go`; was: Low) — `ebpf_psi_pressure_ratio`, `ebpf_psi_stall_seconds_total` |
