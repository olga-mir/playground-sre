# Investigation: the 8.3s p99 run-queue latency reading

Started 2026-09-08, following a review of `ebpf-noisy-neighbor-analysis.md` ahead of the
CloudCon Sydney talk. The headline number in that report — p99 run-queue latency peaking
around 8.3 seconds — doesn't hold up as a precise measurement on inspection. This doc tracks
why, what's been ruled out, and what's still open.

## 1. The 8.3s figure is a histogram artifact, not a measured value

`get_hist_bucket()` in `bpf/noisy-neighbour.bpf.c` buckets latency on a log2 scale in
microseconds, capped at 24 buckets (`NUM_BUCKETS`). Bucket *b* (for b < 23) covers
`[2^b, 2^(b+1))` µs. Bucket 23 is different in kind: it's the overflow catch-all for
**everything ≥ 2^23 µs = 8.388608s, with no upper bound** — confirmed by the code's own
comment (`bucket 23: >= ~8.3s`).

The Go collector (`main.go`, `runqCollector.Collect`) then merges that unbounded overflow
bucket into the same finite-looking Prometheus bucket as the last *real* bounded bucket
(`[4.194304s, 8.388608s)`):

```go
// Absorb BPF overflow bucket (23) into the last Prometheus bucket,
// clamping those events to ≤ bucketBoundsNs[23] (~8.4 s).
cumulBuckets[bucketBoundsNs[numBuckets-1]] += counts[numBuckets-1]
```

Because the merged bucket is given a fake finite `le` of 8.388608s, `histogram_quantile`
has no way to represent "unbounded." If the p99 rank falls in that merged bucket, Prometheus
reports something at or near 8.388608s **whether the real values were 8.4s or 400s.**

**Correct framing for the talk:** the measurement shows run-queue latency of **at least
8.39 seconds, quite possibly much more** — not "8.3 seconds." The current histogram design
cannot distinguish those cases.

**Fix, before trusting or presenting this number:**
- Give the last Prometheus bucket a real `+Inf` `le` so `histogram_quantile` honestly
  returns "can't compute a finite quantile here" instead of a fabricated ceiling, and/or
- Add a small side-channel (a "max raw value seen" per hist-key, or a ring buffer for
  outlier events) that captures the true magnitude instead of clamping it.

**Implemented (branch `86-ebpf/track-a-cpu-rerun`):**
- `runqCollector.Collect` in `main.go` no longer folds BPF overflow bucket 23 into the
  finite `le=8.388608s` Prometheus bucket. Overflow events now count only toward the
  histogram `_count` (the implicit `le="+Inf"` bucket); every finite `le` holds bounded
  observations only. A p99 that ranks past the last finite bucket resolves to the edge of
  the unbounded region (`+Inf`, or ≈8.39s depending on the PromQL engine) instead of a
  fabricated ~8.3s.
- Raw-max side channel: new BPF `BPF_MAP_TYPE_ARRAY` map `runq_lat_max` (1×`u64`),
  `__sync`-CAS max of `runq_lat` in `tp_sched_switch`, exported by `debug.go` as gauge
  **`ebpf_runq_latency_max_nanoseconds`**. (Needs `go generate ./...` via `task lima-build-image`.)
- **What to watch next run:** if `histogram_quantile(0.99, ...)` returns `+Inf`/`NaN` or
  pins to 8.388608s while `ebpf_runq_latency_max_nanoseconds` reads e.g. 40s, the histogram
  was clamping — quote the raw-max value, not the p99, and check whether the raw-max is itself
  a leak artefact (section 4) before trusting it.

## 2. Ruled out: CFS bandwidth throttling

Checked `container_cpu_cfs_throttled_periods_total` (Max, grouped by pod) in Metrics
Explorer for the actual test window, filtered to the `test` node pool (where the
`bully-hog` / victim experiment ran): **flat 0 for every pod** —
`fluentbit-*`, `gke-metrics-agent-*`, `netd-*`, `node-local-dns-*`, `pdcsi-node-*`,
`victim-api-*` — across the whole 7:23–10:32 AM window.

This directly rules out victim-pod CFS quota throttling as the mechanism behind the
anomalous latency reading for this test. (There *is* a large throttling spike visible on
the `default` node pool in the same window, but that pool wasn't used for this experiment —
unrelated background churn from other system pods, not part of this investigation.)

## 3. Untested: hypervisor steal time (E2 dynamic resource management)

`node_cpu_seconds_total{mode="steal"}` is not currently in the active metric set for this
cluster. E2 is GCP's cost-optimized, dynamically-managed machine family (unlike N2/C2's
dedicated cores) and steal time there is a real, documented characteristic — worth checking
before ruling it in or out.

**Action:** next time this setup is spun up, confirm node-level CPU metrics (including
`mode="steal"`) are being scraped by GKE Managed Prometheus — add a PodMonitoring/scrape
config for `node_cpu_seconds_total` (via node_exporter or the GKE Managed Prometheus
node-level collector) if it isn't already, then re-run the load test and check steal time
for the exact window against the anomalous latency events.

**Implemented (branch `86-ebpf/track-a-cpu-rerun`):**
- `k8s/node-exporter.yaml` — `prometheus/node-exporter` DaemonSet (namespace `test-ebpf`,
  scoped to `workload=noisy-node` + `dedicated=noisy-node:NoSchedule`, `hostNetwork`/`hostPID`,
  host `/proc` `/sys` `/` mounts) plus a `PodMonitoring` selecting it.
- Deploy with **`task deploy-node-exporter`** (`kubectl apply -f k8s/node-exporter.yaml`).
- **Check query:** `rate(node_cpu_seconds_total{mode="steal"}[5m])`.
- **What "steal confirmed" looks like:** a non-trivial `mode="steal"` rate (say >0.02 =
  ~2% of a core) on the test node during the exact window of the high-latency events. Flat
  ~0 across the window rules steal time out as the mechanism.

## 4. Leading suspect: orphaned entries in `runq_enqueued` (PID reuse)

The `runq_enqueued` BPF hash map has no expiry and no cleanup path other than the delete
inside `sched_switch` when a PID is matched as `next`. If a task gets a wakeup timestamp
written (`BPF_NOEXIST` insert) but is never subsequently matched by a `sched_switch` event
as `next` — it exits, gets reaped, or its scheduling event is otherwise missed — that
timestamp sits in the map indefinitely. PIDs get reused under load. If an unrelated task
later inherits that recycled PID and *does* get scheduled, the code looks up the map, finds
the stale orphaned timestamp, and reports a "latency" that's actually the wall-clock gap
between some earlier, unrelated task's wakeup and this new task's scheduling — a fabricated
number with no relationship to real run-queue contention.

This isn't hypothetical for this repo: `outcomes/step01-exploring-loaded-ebpf-program.md`
(the original Feb 2025 exploration notes) already observed *"This map constantly growing as
it collects more and more data"* — the exact symptom of this leak, noted over a year ago and
never resolved.

**Verification (no new code needed):** during/after the next load test, dump
`runq_enqueued` (`bpftool map dump id <id>`, or extend `list-cgroup-labels.sh`) and check
how many entries persist and how old their timestamps are relative to "now." A tail of
entries with old stale timestamps confirms the leak.

**Fix, if confirmed:** key on PID + task start-time (not PID alone) to prevent recycled-PID
collisions, or hook `sched_process_exit` to clean up orphaned entries proactively.

**Implemented (branch `86-ebpf/track-a-cpu-rerun`):**
- `debug.go` — a goroutine (`watchRunqEnqueued`, wired from `main()` with one line:
  `go watchRunqEnqueued(&objs)`) walks `objs.RunqEnqueued` every 5s and exports gauges
  **`ebpf_runq_enqueued_entries`**, **`ebpf_runq_enqueued_max_age_seconds`**, and
  **`ebpf_runq_enqueued_stale_entries`** (entries older than `staleAgeThreshold = 10s`),
  computing age against `CLOCK_MONOTONIC` — the same clock as `bpf_ktime_get_ns`. It also
  logs a one-line summary each tick, visible in `kubectl logs`.
- `dump-runq-enqueued.sh` + **`task dump-runq-enqueued`** — shells into the `bpftool`
  DaemonSet pod on the test node and runs `bpftool map dump name runq_enqueued` /
  `runq_histograms` for manual inspection.
- **What "leak confirmed" looks like:** `ebpf_runq_enqueued_entries` climbs steadily
  through the run and never drops back; `ebpf_runq_enqueued_stale_entries` goes and stays
  non-zero; `ebpf_runq_enqueued_max_age_seconds` grows without bound (tens/hundreds of
  seconds). In the raw dump, a tail of entries whose `u64` timestamps are many seconds
  behind `/proc/uptime`. If confirmed, the 8.3s figure is an orphan+PID-reuse artefact and
  the histogram/steal-time findings are moot for this run.

**Confirmed by the 2026-09-09 run** (`run-results-2026-09-09.md`): `runq_enqueued` grew
404 → 3 746+ entries with ~99.5 % of them stale, `max_age_seconds` climbed 1:1 with
wall-clock (oldest entry predated every stressor), and the raw-max side channel
`ebpf_runq_latency_max_nanoseconds` read **73 minutes** by end of run. The 8.3 s p99 is
this artefact.

### Root cause found (2026-09-09): `tp_sched_wakeup` lost its `ctx[0]` index

It is not primarily PID reuse. `tp_btf` programs get the raw tracepoint args array as
context; `sched_wakeup`'s single arg (`struct task_struct *p`) is `ctx[0]`. The **original**
handler had this right:

```c
int tp_sched_wakeup(u64 *ctx) {
    struct task_struct *task = (void *)ctx[0];   // correct
```

A later refactor (`03888db`, "replace ringbuf streaming with in-kernel histogram") rewrote it
to:

```c
int tp_sched_wakeup(void *ctx) {
    task = (struct task_struct *)ctx;            // BUG: ctx is the args array, not ctx[0]
    __u32 pid = BPF_CORE_READ(task, pid);        // reads pid at (args-array addr + pid offset) = garbage
```

So nearly every `sched_wakeup` inserted `runq_enqueued[<garbage/constant>] = ts`.
`tp_sched_switch` kept the correct `ctx[2]` for `next`, so its lookups by real `next_pid`
almost never matched — entries were never deleted (unbounded growth, ~99 % stale) and the
occasional coincidental match against a long-dead key produced the multi-second /
multi-minute "latency" that filled the histogram overflow bucket. The `prev→next` cgroup
*edges* in `ebpf-noisy-neighbor-analysis.md` come straight from `sched_switch` and may still
be real; the latency *magnitudes* attached to them were not.

**Fix in source (2026-09-09) — committed `eadd5bf`, NOT yet built/deployed/verified:**
- `bpf/noisy-neighbour.bpf.c`: `tp_sched_wakeup` restored to `u64 *ctx` /
  `(struct task_struct *)ctx[0]` (matches `tp_sched_switch`'s idiom and the pre-`03888db`
  version).
- New `tp_sched_process_exit` (`SEC("tp_btf/sched_process_exit")`, also `ctx[0]`) does
  `bpf_map_delete_elem(&runq_enqueued, &pid)` on task exit — a cheap mop-up for the genuine
  orphan (task woken but never scheduled before it exits). Attached in `main.go` next to the
  `sched_wakeup` / `sched_switch` links.
- No map-layout change → `debug.go` / `dump-runq-enqueued.sh` untouched; the
  `ebpf_runq_enqueued_*` gauges are the regression check.

**Deployment status (2026-09-09):**
- `52b9b2d` (the `sched_process_exit` hook *alone*, before the `ctx[0]` fix) was built,
  pushed to GAR, and rolled onto `ebpf-ds`. On an **idle** node it still leaked
  (`entries` 55 → 400+ in 4 min, `max_age` tracking wall-clock) — proof the orphans are
  not "tasks that exited", which is what sent the investigation to `git log` and the
  `ctx[0]` bug.
- `eadd5bf` (the actual `ctx[0]` fix) is **committed but not built** — the Docker build
  base images (`golang:1.25`, `ubuntu:24.10`) come from Docker Hub and Docker Desktop's
  VM could not reach `registry-1.docker.io` at the time (`DeadlineExceeded`). The
  collector running on the cluster is still `52b9b2d` and still leaking.
- **To finish:** `task local-build-image && task deploy-k8s` then
  `kubectl -n test-ebpf rollout restart ds/ebpf-ds` (image tag is `:latest`, so a restart
  is needed to re-pull). If Docker Hub is still unreachable, build inside Lima
  (`task lima-start` → `task docker-auth` → `limactl shell ebpf-dev` →
  `task lima-build-image`) — separate VM, separate network path. Longer-term, mirror the
  base images into Artifact Registry so builds don't depend on Docker Hub (see README
  "Build").

- **What "verified fixed" looks like:** `ebpf_runq_enqueued_entries` tracks only
  genuinely-runnable tasks (single/double digits) and stays flat; `_stale_entries` ~0;
  `_max_age_seconds` sub-second; `histogram_quantile(0.99, …)` and
  `ebpf_runq_latency_max_nanoseconds` drop to plausible (low-ms) values.
- **Escalation still available** if a real re-run under load shows residual growth: key the
  map on `pid + task start_time` to defend against genuinely-missed `sched_switch` events +
  PID reuse. Held back (changes the key struct / `debug.go` iterate loop) — the context-bug
  fix should make the map behave.

## Priority order for next test run

1. Dump `runq_enqueued` during/after the load test — cheapest check, no new code, and the
   most likely root cause given the step01 precedent.
2. Confirm `node_cpu_seconds_total{mode="steal"}` is scraped via GKE Managed Prometheus,
   then re-run and cross-check against steal time for the same window.
3. Fix the histogram's top bucket (`+Inf` `le`, or a raw-max side channel) so future runs
   report a trustworthy ceiling instead of a clamped one.
4. Only after 1–3: decide whether the finding is presentable as-is, or needs a corrected
   number/story for the talk.

## 5. Follow-up plan (2026-09-08)

Pushback on treating the "system processes preempt due to I/O noise" theory as an
alternative explanation for the original spike: it doesn't fit `bully-hog`'s actual
workload. `stress-ng --cpu 2 --cpu-method matrixprod` is pure in-cache compute — no
syscalls, no I/O, nothing to trigger a softirq/kworker/kswapd surge. That mechanism needs
an I/O- or network-heavy neighbor to be a fair test; the CPU-only run never exercised it
either way, so its absence there isn't counter-evidence against the theory, just a
mismatched experiment.

Next steps, as two separate, deliberate tracks:

**Track A — re-run the CPU experiment with the PID-reuse checks from section 4** (today).
Dump `runq_enqueued` during/after the load test as planned; 4-8s of CFS run-queue latency
on 2 vCPUs under proportional scheduling doesn't have a sound mechanism behind it (CFS's
own fairness bounds — `sched_min_granularity_ns` × runnable-task count — don't get you
anywhere near seconds with a normal handful of runnable tasks), so the leak theory remains
the leading suspect until the map dump says otherwise.

**Track B — a genuinely separate I/O-stress experiment**, to properly test the
kernel-thread-preemption theory instead of retrofitting it onto Track A's data:
- New stressor pod, analogous to `bully-hog.yaml`: disk I/O (`stress-ng --hdd` / `fio`)
  and/or network I/O (`iperf3`, `fortio`, or `stress-ng --sock`) — pick disk vs. network
  deliberately, they stress different kernel subsystems.
- Run the *existing* sched_wakeup/sched_switch tool during this test too. If the theory is
  right, `prev_cgroup` should start showing `root`/`system.slice/*` for victim starvation
  events here — that's the actual valid test, which Track A could never have provided.
- Natural pairing: implement the PSI probe (`/proc/pressure/io`) from
  `docs/proposal-non-cpu-noisy-neighbours.md` (rated Low complexity there) alongside this
  run, as a second independent signal for the same test.

**Caveat for interpreting any of this:** system-process/root-cgroup series were
deliberately stripped down in earlier dashboard views because they produced too much noise
to sift through. Before concluding anything from historical data about how often `root`/
`system.slice/*` appears as `prev_cgroup`, confirm whether that stripping happened at the
dashboard/query level (recoverable — widen the filter) or before data left the collector
(not recoverable — only future runs will have it).

### Track B — Implemented (2026-09-09)

Code / manifests / docs only; nothing has been run against GKE yet.

**Stressors** (both mirror `bully-hog.yaml` — `noisy-node` selector + toleration, a CPU
*request* only and **no CPU limit**, so Burstable QoS keeps them in the shared pool):

- `k8s/bully-io.yaml` — disk: `stress-ng --hdd 2 --hdd-bytes 512m --iomix 2`, writing into
  an `emptyDir` (node-disk backed) via `--temp-path`, so the writes actually reach the
  page cache → `wb_workfn` / kworker writeback and the block queue (proposal §2A).
  `task deploy-bully-io`.
- `k8s/bully-net.yaml` — network: `stress-ng --sock 4 --udp 2`, high packet rate over
  loopback → `NET_RX` / `ksoftirqd` (proposal §3A). `task deploy-bully-net`.

Run them **one at a time** — disk vs. network stress different kernel subsystems; disk is
the cleaner first test for the writeback-preemption story.

**PSI probe** — `psi.go` adds a second `prometheus.Collector` next to `runqCollector`
(one added line in `main()`). Node pressure from `/proc/pressure/{cpu,io,memory}` (hostPath
`/host/proc/pressure`, `PSI_PROC_PATH` env, added to `k8s/ebpf-daemonset.yaml`) and
per-pod cgroup pressure from `/sys/fs/cgroup/<pod>/{cpu,io,memory}.pressure`, emitted as:

- `ebpf_psi_pressure_ratio{resource, kind, window, scope, cgroup}` — gauge, the kernel's
  `avgN` stall percentage (0–100).
- `ebpf_psi_stall_seconds_total{resource, kind, scope, cgroup}` — counter, from `total=`
  (µs → s).

**Verification procedure** — while one stressor runs:

1. Watch `ebpf_psi_pressure_ratio{resource="io"}` (disk run) / `{resource="cpu"}`
   (network run — softirq stealing CPU) rise, at both `scope="node"` and the victim's
   `scope="cgroup"` series; `ebpf_psi_stall_seconds_total` gives the monotonic view.
2. In the same window, check whether `ebpf_runq_latency_nanoseconds`'s `prev_cgroup` label
   starts carrying `root` / `system.slice/*` (kworker, `ksoftirqd`, writeback) on the
   victim's starvation events — the signature the kernel-thread-preemption theory predicts,
   which Track A's pure-compute bully could never produce. **No collector change is
   needed:** `runqCollector` already emits `prev_cgroup` raw and unfiltered — only the
   `cgroup` (victim) side gets the `strings.HasPrefix(cgroup, "pod/")` filter; the
   `prev_cgroup` side is passed straight through (`root`, `system.slice/*`, `unresolved`
   and all).
3. Cross-reference: I/O PSI up **and** `prev_cgroup` = system/root on the latency events
   ⇒ the theory holds for this workload.

**On the stripped-series caveat above, now made actionable:** the raw
`ebpf_runq_latency_nanoseconds` metric is **unfiltered on `prev_cgroup`**, so any stripping
of `root` / `system.slice/*` was at the dashboard/query layer, not in the collector.
Widening the dashboard's `prev_cgroup` filter (or querying Prometheus directly) is
sufficient to see those series — no collector change and no new data collection required;
scrapes from any run with this collector already contain them.
