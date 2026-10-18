# Debugger Pod — Scheduler Tracing & Kernel Debugging

## Overview

The **debugger pod** replaces the `ballast` DaemonSet with a privileged Ubuntu container that provides kernel tracing tools, most notably **bpftrace** for collecting scheduler events.

### Key Differences from Ballast

| Feature | Ballast | Debugger |
|---------|---------|----------|
| **Purpose** | Reserve 1 exclusive CPU core (pin it, leave it idle) | Reserve 1 exclusive CPU core + provide tracing tools |
| **Image** | `registry.k8s.io/pause:3.10` (minimal) | `ubuntu:24.04` (full OS) |
| **Disk overhead** | ~1 MB | ~100 MB |
| **Privileged** | No | Yes (required for bpftrace/BPF) |
| **Tools** | None | bpftrace, linux-tools, procps |
| **Use case** | General isolation | Active debugging/tracing |

### Trade-off

- **Pro:** No need for separate debug pods; tools always available on the test node
- **Con:** Slightly larger image footprint, privileged pod on test node

---

## Deployment

Replace `deploy-ballast` with `deploy-debugger`:

```bash
# Old way:
task ebpf:deploy-ballast

# New way:
task ebpf:deploy-debugger
```

The debugger pod will be scheduled on all `workload=noisy-node` labeled nodes (same as ballast).

### First Access (Auto-Install)

On first access, the pod runs an inline installation script:

```bash
apt-get update && apt-get install -y bpftrace linux-tools-generic-hwe procps
```

This takes **~10-15 seconds** on first access, then is cached. Subsequent access is instant.

---

## Quick Start: Collecting Scheduler Events

### 1. Deploy the debugger pod

```bash
task ebpf:deploy-debugger
```

### 2. Open an interactive shell

```bash
task ebpf:debug:debugger-shell
```

Or manually:

```bash
kubectl -n ebpf-noisy-neighbour exec -it <debugger-pod-name> -- /bin/bash
```

### 3. Run bpftrace (first time will install tools)

Inside the pod:

```bash
# Record 2 seconds of scheduler events on CPU 0
bpftrace - << 'SCRIPT'
BEGIN { @trace_start = nsecs; @target_cpu = 0; }
tracepoint:sched:sched_wakeup {
    if (cpu == @target_cpu) {
        @wakeup_time[args->pid] = nsecs;
        $elapsed_ms = (nsecs - @trace_start) / 1000000.0;
        printf("{\"event\":\"wakeup\",\"ts_ms\":%.3f,\"pid\":%d,\"comm\":\"%s\",\"prio\":%d}\n",
            $elapsed_ms, args->pid, args->comm, args->prio);
    }
}
tracepoint:sched:sched_switch {
    if (cpu == @target_cpu) {
        $elapsed_ms = (nsecs - @trace_start) / 1000000.0;
        $wake_latency_us = 0;
        if (@wakeup_time[args->next_pid]) {
            $wake_latency_us = (nsecs - @wakeup_time[args->next_pid]) / 1000.0;
            delete(@wakeup_time[args->next_pid]);
        }
        printf("{\"event\":\"switch\",\"ts_ms\":%.3f,\"pid\":%d,\"comm\":\"%s\",\"prev_comm\":\"%s\",\"wake_latency_us\":%.2f}\n",
            $elapsed_ms, args->next_pid, args->next_comm, args->prev_comm, $wake_latency_us);
    }
}
END { printf("# Complete\n"); }
SCRIPT
```

Let it run for ~2 seconds, then **Ctrl+C**.

### 4. Copy output to your local machine

From the debugger shell:

```bash
# If output was to stdout, save inside the pod:
bpftrace /path/to/script > /tmp/events.jsonl
```

Then from your local machine:

```bash
kubectl -n ebpf-noisy-neighbour exec <debugger-pod> -- cat /tmp/events.jsonl > ~/sched-events.jsonl
```

### 5. Visualize

Use the visualization script from the scratchpad:

```bash
python3 viz-scheduler-events.py ~/sched-events.jsonl ~/timeline.html
open ~/timeline.html
```

---

## Manual Alternatives

If you want more control, you can also:

### Run bpftrace directly from your local machine (via kubectl exec)

```bash
kubectl -n ebpf-noisy-neighbour exec <debugger-pod> -- bpftrace - 0 << 'SCRIPT'
BEGIN { @trace_start = nsecs; @target_cpu = $1; }
tracepoint:sched:sched_switch { ... }
SCRIPT
```

### Use perf inside the debugger pod

```bash
# Inside the pod
perf record -e sched:sched_switch,sched:sched_wakeup -C 0 -o /tmp/perf.data sleep 2
perf script -i /tmp/perf.data > /tmp/sched-events.txt
```

### Use ftrace directly

```bash
# Inside the pod
trace-cmd record -e sched:sched_switch -e sched:sched_wakeup -c 0 sleep 2
trace-cmd report > /tmp/sched-events.txt
```

---

## Troubleshooting

### Pod won't start

Check pod events:

```bash
kubectl -n ebpf-noisy-neighbour describe pod <debugger-pod-name>
```

Common issues:
- **Node full on disk:** The debugger pod needs space for Ubuntu base image (~100MB). If the node is out of disk, you may need to clean it up first.
- **Not scheduled to test node:** Verify nodes have the `workload=noisy-node` label and tolerate the `dedicated=noisy-node` taint.

### bpftrace command not found on first access

The pod is installing tools. Wait 10-15 seconds and try again:

```bash
sleep 15
bpftrace --help
```

### High CPU from the debugger pod

When idling (not tracing), the debugger pod uses negligible CPU — it's just a sleeping shell. If you see high CPU, a tracing session is running. Kill it:

```bash
pkill -f bpftrace
```

---

## CPU Pinning Verification

To verify the debugger pod is pinned to an exclusive core:

```bash
# Inside the debugger pod
cat /proc/self/cgroup | grep cpuset
```

You should see a cpuset with a single CPU ID (e.g., `cpuset=/cpuset/kubepods.slice/...`).

From your local machine:

```bash
task ebpf:debug:list-cpu-pinning
```

This shows which containers are pinned to which CPUs. You should see the debugger pod pinned to one core, and the test workloads (bully, victim) on the remaining cores.

---

## Migration from Ballast

If you previously used `deploy-ballast`:

1. Delete the old ballast DaemonSet:
   ```bash
   kubectl -n ebpf-noisy-neighbour delete ds ballast
   ```

2. Deploy the debugger pod:
   ```bash
   task ebpf:deploy-debugger
   ```

3. Verify it's scheduled:
   ```bash
   kubectl -n ebpf-noisy-neighbour get pods -l app=debugger
   ```

The debugger pod will immediately pin the same exclusive core(s) that ballast was using.

---

## See Also

- `sched-events.bt` — Standalone bpftrace script for scheduler tracing
- `viz-scheduler-events.py` — Visualization tool for JSONL output
- AGENTS.md — Full task documentation
