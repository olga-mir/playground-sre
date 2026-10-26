# EEVDF Scheduler Verification and Impact Analysis

**Cluster:** `experiment-ebpf-cluster` (GKE standard, `australia-southeast1-a`)  
**OS:** Container-Optimized OS (COS)  
**Kernel:** Linux `6.12.105+ (amd64)`  
**Date Verified:** October 2026  

---

## 1. Executive Summary

- **EEVDF is enabled and active:** In Linux kernel 6.6+, Peter Zijlstra's **EEVDF** (*Earliest Eligible Virtual Deadline First*) completely replaced the legacy CFS (*Completely Fair Scheduler*) algorithm inside `kernel/sched/fair.c`. There is no separate config switch to toggle back to legacy CFS; it is the default and only fair scheduling class engine in Linux 6.12.
- **Direct Kernel Evidence:** Verification directly on the live GKE node confirms the presence of core EEVDF symbols in `/proc/kallsyms` (`pick_eevdf`, `entity_eligible`) and the removal of legacy CFS sysctl tunables.
- **Impact on the Noisy-Neighbour Experiment & Talk:** **EEVDF does NOT undermine the talk or experiment.** In fact, it significantly strengthens it. EEVDF does not eliminate run-queue contention under saturated CPU conditions, does not eliminate latency spikes, and provides zero attribution. Our live test results on this 6.12 kernel empirically prove that a noisy neighbour (`bully-hog`) still induces severe run-queue starvation on `victim-api`.

---

## 2. In-Cluster Kernel Evidence

The following verification steps were performed inside the privileged debug daemonset pod (`bpftool-ds`) running on node `gke-experiment-ebpf-cluster-test-pool-8126aa3e-jmhd`:

### A. Node Kernel Version
```bash
$ uname -r
6.12.105+
```
Linux 6.12 is well past Linux 6.6 (where EEVDF was merged into mainline).

### B. Kernel Symbols (`/proc/kallsyms`)
Grepping `/proc/kallsyms` confirms the compiled and active EEVDF scheduling functions:

```text
ffffffffb712eaa0 T entity_eligible
ffffffffb7139a60 t pick_eevdf
ffffffffb7138cd0 t place_entity
ffffffffb71378f0 t update_curr_fair
```

- **`entity_eligible`**: Evaluates whether a scheduling entity's virtual runtime is eligible to execute ($v_i \le V$, where $V$ is runqueue virtual time `cfs_rq->min_vruntime`).
- **`pick_eevdf`**: Iterates candidate eligible entities to pick the one with the earliest virtual deadline ($d_i = v_i + \frac{q_i}{w_i}$).

### C. Sysctl Entries (`/proc/sys/kernel/`)
Under CFS, the kernel exposed tunables for target scheduling latency:
- `sched_latency_ns`
- `sched_min_granularity_ns`
- `sched_wakeup_granularity_ns`

Inspecting `/proc/sys/kernel/sched*` on the node shows these have been removed as part of the upstream CFS retirement:
```text
/proc/sys/kernel/sched_cfs_bandwidth_slice_us
/proc/sys/kernel/sched_deadline_period_max_us
/proc/sys/kernel/sched_deadline_period_min_us
/proc/sys/kernel/sched_rr_timeslice_ms
/proc/sys/kernel/sched_rt_period_us
/proc/sys/kernel/sched_rt_runtime_us
/proc/sys/kernel/sched_schedstats
```
*(Note: `sched_cfs_bandwidth_slice_us` belongs to cgroup bandwidth throttling quota, which is still active and unchanged).*

### D. Kernel Configuration (`/proc/config.gz`)
```text
CONFIG_FAIR_GROUP_SCHED=y
CONFIG_CGROUP_SCHED=y
CONFIG_SCHED_CLASS_EXT=y
# CONFIG_SCHED_DEBUG is not set
CONFIG_SCHED_INFO=y
CONFIG_SCHEDSTATS=y
```

---

## 3. Does EEVDF Undermine the Talk or Experiment?

**No.** A common misunderstanding arises from early coverage of EEVDF (e.g. LWN's article: *"Some processes may not need a lot of CPU time but, when they do need that time, they need it quickly"*). Some assume that because EEVDF considers latency sensitivity and deadlines, it somehow prevents noisy neighbour starvation.

Here is why that assumption is wrong:

### 1. Physics: Contention Cannot Be Scheduled Away
When two tasks both want to execute on a single physical/logical CPU core simultaneously (e.g., `bully-hog` computing burn loops while `victim-api` wakes up to process an incoming HTTP request):
- The physical core can only execute **one instruction stream at a time**.
- One task **must** wait on the run queue while the other runs.
- The time spent waiting between `sched_wakeup` and `sched_switch` is the definition of **run-queue latency**.

### 2. Standard Containers Run with Default Base Slices
EEVDF calculates deadline as:
$$d_i = v_i + \frac{q_i}{w_i}$$
Where:
- $v_i$ is virtual runtime.
- $q_i$ is the time slice (`base_slice_ns`, typically 0.7ms–3ms).
- $w_i$ is task weight (derived from CPU shares/requests).

In standard Kubernetes, containers do not call `sched_setattr()` to request custom latency slices ($q_i$). Every container task runs with the standard time slice. Therefore, scheduling priority between eligible tasks is still largely governed by relative CPU weights ($w_i$). A bully with equal or higher shares is just as eligible as the victim.

### 3. Preemption Is Not Zero-Latency
Even when a waking task has an earlier deadline than the running task:
- Preemption is evaluated at tick boundaries or preemption points; it is not instantaneous zero-nanosecond preemption.
- Under heavy core saturation and thread churn, context switching overhead and run-queue delay still delay the victim.

### 4. Empirical Evidence from This Exact Setup
Our experiment (`stash/2026.10.14-experiment-events.md`) was conducted directly on this GKE 6.12 cluster. During the experiment:
- `victim-api` experienced severe run-queue latency (seconds of delay).
- `cpu.pressure` (PSI) spiked significantly during the bully run and dropped immediately when the bully was scaled to 0.
- If EEVDF solved noisy neighbours, the victim would have experienced zero starvation. The graphs proved the exact opposite.

### 5. What EEVDF Still Cannot Do: Attribution
EEVDF is an in-kernel scheduling algorithm. It does not:
- Export metrics showing who preempted whom.
- Tell an SRE which neighbor container is causing tail latency.
- Provide per-cgroup run-queue latency distribution histograms.

PSI gives a signal that starvation is occurring, but cannot provide attribution. **Only eBPF instrumentation (`sched_wakeup` / `sched_switch`) bridges this gap.**

---

## 4. Talking Point for CloudCon Sydney

This verification transforms a potential audience question into a technical highlight:

> *"You might ask: didn't Linux 6.6 replace CFS with EEVDF to improve latency handling? We ran our demo cluster on Google Cloud's latest Container-Optimized OS with Linux kernel 6.12, where `pick_eevdf` is active.  
> The reality: EEVDF balances virtual deadlines, but it cannot bend the laws of physics. When a core is overcommitted, threads wait in the run queue. More importantly, EEVDF gives you zero visibility into who is causing the queue delay. eBPF remains the only way to get granular, per-cgroup attribution."*
