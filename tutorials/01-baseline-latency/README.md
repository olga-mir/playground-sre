# 01 — Is my latency the app's fault, or is it queueing?

> **Time:** ~15 min · **Needs:** local only (the same steps work against a cluster) · **Cost:** none
>
> **Prerequisites:** [00 — Prerequisites](../00-prerequisites.md), "Local tools"

## The question

A service reports p99 latency well above what a single request should take. Is the handler slow,
or are requests waiting for something (a CPU, a worker, a connection) before the handler even runs?

The `perf-lab` server lets you separate the two, because you can choose a request whose *own*
cost is known exactly.

## Hypothesis

- `sleep?duration=50ms` costs 50 ms and uses no CPU. Its latency should stay at ~50 ms plus a
  couple of ms of overhead no matter how many clients you add.
- `cpu?duration=50ms&goroutines=1` also takes 50 ms of wall time, but it burns a core while it
  does so. With no more concurrent requests than cores it should look the same as `sleep`. Once
  concurrency exceeds the core count, requests compete for CPU and latency should climb well
  above 50 ms, even though no individual request got more expensive.

## Setup

`perf-lab` rate-limits each scenario by default (`/cpu` allows 5 requests/s), which would cap the
load test and answer with `429`. Disable the limiter for load tests:

```bash
DISABLE_RATE_LIMIT=true SERVER_ADDRESS=:8080 task perf-lab:run
```

Check the machine you are testing on. The interesting threshold is the number of cores:

```bash
sysctl -n hw.ncpu      # macOS
nproc                  # Linux
```

The numbers below were taken on an 8-core laptop.

## Run

In a second terminal. Each run is 10 seconds.

**1. Baseline: a request that costs nothing but time** (open loop at 50 req/s, 4 connections)

```bash
fortio load -qps 50 -c 4 -t 10s -quiet \
  'http://localhost:8080/v1/scenarios/sleep?duration=50ms'
```

**2. CPU-bound, concurrency at or below the core count** (as fast as possible, 8 connections)

```bash
fortio load -qps 0 -c 8 -t 10s -quiet \
  'http://localhost:8080/v1/scenarios/cpu?duration=50ms&goroutines=1'
```

**3. CPU-bound, concurrency at 4x the core count** (32 connections)

```bash
fortio load -qps 0 -c 32 -t 10s -quiet \
  'http://localhost:8080/v1/scenarios/cpu?duration=50ms&goroutines=1'
```

Keep `-c` the only thing that changes between runs 2 and 3.

## What to look at

Fortio prints the latency percentiles. The server records the same thing as a histogram:

```bash
curl -s localhost:8080/metrics | grep http_request_duration_seconds_bucket | grep 'route="/v1/scenarios/cpu"'
```

On a cluster with Managed Prometheus (the `PodMonitoring` in `workloads/perf-lab/k8s/` scrapes it
every 15 s), use PromQL:

```promql
# p99 per route
histogram_quantile(0.99,
  sum by (le, route) (rate(http_request_duration_seconds_bucket[1m])))

# requests being handled at once
sum(http_requests_in_flight)
```

## What you should see

Raw output is in [`results/`](results/). Summary:

| Run | Concurrency | p50 | p90 | p99 | Throughput |
|---|---|---|---|---|---|
| `sleep` 50 ms | 4 | 55 ms | 59 ms | 63 ms | 49.7 qps (target 50) |
| `cpu` 50 ms | 8 (= cores) | 55 ms | 59 ms | 68 ms | 153 qps |
| `cpu` 50 ms | 32 (4x cores) | 101 ms | 186 ms | 366 ms | 271 qps |

Fortio estimates percentiles by interpolating inside its histogram buckets, so p50 reads slightly
above the 50 ms floor even when the minimum observed was 50.1 ms. Read the *shape*, not the third
digit. Absolute values depend on your machine and what else is running on it.

The interesting row is the last one: the requests are identical to the previous row, but p99 is
about 5x higher and p50 nearly doubled.

## Why

Two things changed between runs 2 and 3, and only one of them is the handler:

1. **Nothing about the handler's cost changed.** It is a time-bounded loop (`internal/scenarios/cpu.go`):
   it runs until its deadline whichever CPU share it gets, so under contention it does less
   work in the same 50 ms rather than taking longer. (Each response reports `total_hashes`, which
   you can compare between a quiet and a loaded server.)
2. **Queueing in front of the CPU.** 32 runnable goroutines on 8 cores means the Go and OS
   schedulers have to time-slice. A request that was runnable but not running adds latency that
   no application timer sees. The extra latency over 50 ms is time spent *waiting*, not working.

This is the same shape as production incidents where "CPU looks fine" (average utilisation) yet
tail latency is high: utilisation says how busy the CPU is, not how long work waited for it. The
later tutorials make the waiting measurable: run-queue latency from the kernel's point of view, and
the effect of a neighbouring pod taking the CPU away.

Interpretation caveats worth stating:

- The `sleep` row shows that the server and load generator themselves add ~2-3 ms. Your baseline is
  never zero.
- `-qps 0` is a closed loop: clients wait for each response before sending the next, so extra
  latency lowers the request rate instead of building an unbounded queue. Setting a target rate
  makes it open-loop, which is closer to real traffic. With `-qps 400 -c 32` fortio warned that
  it was "falling behind" (achieved 255 qps) and the tail got worse (p99 ~575 ms, max 2.6 s). Fortio
  still caps in-flight requests at `-c`, so raise `-c` too if you want to see the backlog grow
  unbounded.

## Clean up

Stop the server with Ctrl-C. Nothing else was created.

## Tested on

macOS (Darwin 25.5), 8 logical CPUs, Go as pinned in `workloads/perf-lab/go.mod`, Fortio 1.73.0,
2026-11-08.

## Next

- [04 — Where does the CPU time go?](../04-profiling/README.md): take the slow case from run 3 and look at it in a flame graph.
- Later in the series: the same experiment in a pod with a CPU limit, then with a noisy neighbour.
