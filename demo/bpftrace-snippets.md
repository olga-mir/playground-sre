# Why?

OTEL trace is completely blind to scheduler contention. It only captures what happens from the application's perspective. OTEL trace can show that a call to a database took 8.9 ms. Why was it slow? Is it the network? Is the database struggling? Standard OTel has no way of telling that an app spent 7 ms of that time on a local CPU run-queue wait, just trying to process the incoming database payload.

To capture the physical kernel reality, low-level ebpf instrumentation like `bpftool` or custom instrumentation maybe required.

---

### The Contrast: OpenTelemetry vs. eBPF Tracing

This split illustrates what users see versus what is actually happening:

```text
WHAT OPENTELEMETRY (OTEL) SEES:
[HTTP POST /checkout] --------------------------------------------------------------------> (Total: 28ms)
  ├── [Read Socket] -> 0.1ms
  ├── [SQL SELECT] -----------------------------------------------------> (Total: 8.9ms)  <-- "Why so slow?!"
  │     (App registers start ... Network I/O ... App registers finished)
  └── [JSON Render] -> 19ms


WHAT eBPF & THE KERNEL ACTUALLY DO (The scheduler timeline):
SQL SELECT: Network packet arrives. Kernel wakes thread.
 [sched_wakeup]
       │
       ▼
▒▒▒▒▒▒▒▒▒▒▒▒▒▒ RUN-QUEUE DELAY (7.1ms) ▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒  <-- A noisy neighbor is on-CPU
       │ (sched_switch in)
       ▼
█ CPU (0.3ms)  <-- Thread finally parses DB Response
       │ (Network call / wait)
       ▼
[Await Next DB Frame] -> [sched_wakeup] -> ▒▒ RUN-QUEUE DELAY (1.5ms) ▒▒ -> █ CPU (0.0ms)

```

**Why OTel can't see this:**

* OTel operates inside the userspace application runtime.
* It measures span durations using timestamps (`t_end - t_start`).
* If a thread is preempted or sits waiting on the CPU run-queue, it is still "inside" that span. Time just keeps ticking, and the span's duration inflates.
* As a result, standard instrumentation often misattributes scheduling delays to the external dependency (like the database or network).

---

### BPFTOOL

To capture this scheduler events, `bpftool` can be used to observe it from the command line.

Because `sched_switch` fires tens of thousands of times per second, tracing should be restricted to target either Pod A or the busy neighbor Pod D. To target Pod A, find the PID of its primary thread by running `pidof <process_name>` on the node.

#### Step 1: Exec into `bpftool` Pod with Host Privileges

The `bpftool` pod must run in the host's PID and network namespace (with privileged access) to interface with tracing and kernel events.

```bash
kubectl exec -it <bpftool-pod-name> -n <namespace> -- /bin/bash

```

#### Step 2: Use `bpftool` to Probe Scheduler Hook Points

To confirm the hook points `sched_wakeup` and `sched_switch` are available, use `bpftool` to check the kernel's tracepoints:

```bash
# Verify that sched tracepoints are available
bpftool link show | grep -E "sched_wakeup|sched_switch"

```

#### Step 3: Run the Scheduler Latency Tracer

While `bpftool` is great for loaded BPF program verification, `bcc-tools` or `bpftrace` are generally preferred for dumping and analyzing the output. If `bpftrace` or bcc tools available in debug pod, the events can be observed with:

```bash
# Run bpftrace to log scheduling wait times for Pod A (using its PID on the node)
bpftrace -e '
tracepoint:sched:sched_wakeup /args->pid == <POD_A_PID>/ {
    @wake[args->pid] = nsecs;
}
tracepoint:sched:sched_switch /@wake[args->next_pid]/ {
    $delay = (nsecs - @wake[args->next_pid]) / 1000000;
    printf("Task %s (PID %d) waited %d ms to run on CPU\n", args->next_comm, args->next_pid, $delay);
    delete(@wake[args->next_pid]);
}'

```

*Expected output while an app experiences the delay:*

```text
Task worker (PID 21013) waited 6 ms to run on CPU
Task worker (PID 21013) waited 8 ms to run on CPU

```

This trace captures the specific duration when the scheduler wakes Pod A and places it in run-queue, but it is forced to wait while the noisy neighbor uses up its time slice. This provides the exact trace data needed to confirm the cause of the latency spikes.
