# Tracing Scheduler Events: What OpenTelemetry Cannot See

Application tracing records what a request did from inside the process. It cannot show time
spent runnable but not running. This page explains the gap and shows how to observe it with
`bpftrace` on a Kubernetes node.

## The gap

An OpenTelemetry trace can show that a database call took 8.9 ms. It cannot say whether the
network was slow, the database was struggling, or the application spent 7 ms of that time waiting
on a local CPU run queue before it could process the response. Observing the run queue needs
kernel-level instrumentation (eBPF).

OTel measures span durations from timestamps (`t_end - t_start`) taken in the application
runtime. A thread that is preempted, or that sits on the run queue waiting for a CPU, is still
inside its span, so the span's duration inflates. Standard instrumentation therefore tends to
attribute scheduling delay to the external dependency (database, network).

### The same request, two views

Illustrative numbers.

```text
WHAT OPENTELEMETRY SEES:
[HTTP POST /checkout] -----------------------------------------------------> (total: 28 ms)
  ├── [Read Socket] -> 0.1 ms
  ├── [SQL SELECT] ---------------------------------------------> (total: 8.9 ms)
  │     (app records start ... network I/O ... app records finish)
  └── [JSON Render] -> 19 ms

WHAT THE KERNEL SCHEDULER DOES DURING SQL SELECT:
 [sched_wakeup]   network packet arrives, kernel wakes the thread
       │
       ▼
▒▒▒▒▒▒▒▒ RUN-QUEUE DELAY (7.1 ms) ▒▒▒▒▒▒▒▒   a noisy neighbour is on the CPU
       │ [sched_switch in]
       ▼
█ CPU (0.3 ms)   thread parses the DB response
       │ (waits for the next frame)
       ▼
[sched_wakeup] -> ▒▒ RUN-QUEUE DELAY (1.5 ms) ▒▒ -> █ CPU (0.0 ms)
```

## Tracing run-queue wait with bpftrace

`sched_switch` fires tens of thousands of times per second on a busy node, so restrict tracing
to one task (the victim) or one CPU.

### 1. Open a shell in the debugger pod

The debugger DaemonSet is privileged, uses the host PID namespace and has `bpftrace` installed
(see [DEBUGGER-POD.md](DEBUGGER-POD.md)). The `bpftool` DaemonSet does not include `bpftrace`.

```bash
task ebpf:deploy-debugger
task ebpf:debug:debugger-shell
```

### 2. Confirm the scheduler tracepoints exist

```bash
bpftrace -l 'tracepoint:sched:sched_wakeup'
bpftrace -l 'tracepoint:sched:sched_switch'
```

### 3. Find the victim's PID on the node

```bash
pidof <process_name>
```

### 4. Log how long the victim waited between wake-up and running

```bash
bpftrace -e '
tracepoint:sched:sched_wakeup /args->pid == <VICTIM_PID>/ {
    @wake[args->pid] = nsecs;
}
tracepoint:sched:sched_switch /@wake[args->next_pid]/ {
    $delay = (nsecs - @wake[args->next_pid]) / 1000000;
    printf("Task %s (PID %d) waited %d ms to run on CPU\n", args->next_comm, args->next_pid, $delay);
    delete(@wake[args->next_pid]);
}'
```

Example output while the application experiences the delay:

```text
Task worker (PID 21013) waited 6 ms to run on CPU
Task worker (PID 21013) waited 8 ms to run on CPU
```

Each line is a time the scheduler woke the task and placed it on the run queue, and then made it
wait while another task (typically the noisy neighbour) used its time slice. That is the direct
evidence for the cause of the latency spike.

## Recording all events on one CPU

To record every wake-up and switch on a single CPU as JSON lines and visualise it, use
[`scripts/sched-events.bt`](../scripts/sched-events.bt) and
[`scripts/viz-scheduler-events.py`](../scripts/viz-scheduler-events.py). The full workflow
(copying the output off the node, generating the HTML timeline) is in
[DEBUGGER-POD.md](DEBUGGER-POD.md). Results from a real run, including a sample trace and
timeline, are in the [scheduler contention report](scheduler-contention-report.md).
