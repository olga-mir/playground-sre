package main

// debug.go — instrumentation for the "8.3 s p99" investigation
// (docs/latency-anomaly-investigation.md). None of this changes what the main
// collector exports; it only adds observability that lets the next load-test
// run confirm or rule out the two leading suspects:
//
//	§4  orphaned entries in the runq_enqueued BPF map + PID reuse
//	§1  the histogram overflow bucket hiding the true latency magnitude
//
// Wired in from main() with a single line:  go watchRunqEnqueued(&objs)

import (
	"log"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/sys/unix"
)

// staleAgeThreshold is the age past which a runq_enqueued entry is almost
// certainly orphaned rather than a task genuinely waiting on the run queue.
// Real run-queue waits are microseconds-to-milliseconds; anything older than
// this is a leaked sched_wakeup timestamp whose task exited (or whose
// sched_switch was missed) before it could be matched and deleted.
const staleAgeThreshold = 10 * time.Second

// runqEnqueuedPollInterval matches the eventsTotal poll cadence already used in
// main() so the two probes tick together in the logs.
const runqEnqueuedPollInterval = 5 * time.Second

var (
	// ebpf_runq_enqueued_entries — live size of the runq_enqueued map. This is
	// the metric that "This map constantly growing" in
	// outcomes/step01-exploring-loaded-ebpf-program.md was describing.
	runqEnqueuedEntries = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ebpf_runq_enqueued_entries",
		Help: "Current number of entries in the runq_enqueued BPF map (pid -> wakeup timestamp).",
	})

	// ebpf_runq_enqueued_max_age_seconds — age of the oldest surviving entry,
	// on the same clock as bpf_ktime_get_ns(). A value that climbs without
	// bound is the orphaned-entry leak.
	runqEnqueuedMaxAgeSeconds = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ebpf_runq_enqueued_max_age_seconds",
		Help: "Age in seconds of the oldest entry in runq_enqueued, measured on CLOCK_MONOTONIC (same clock as bpf_ktime_get_ns).",
	})

	// ebpf_runq_enqueued_stale_entries — entries older than staleAgeThreshold.
	// Leak confirmed = this ratchets upward across a run and never returns to 0.
	runqEnqueuedStaleEntries = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ebpf_runq_enqueued_stale_entries",
		Help: "Number of runq_enqueued entries older than staleAgeThreshold; a monotonically growing value confirms the orphaned-entry / PID-reuse leak.",
	})

	// ebpf_runq_latency_max_nanoseconds — raw-max side channel (deliverable 4).
	//
	// REQUIRES `go generate ./...` (bpf2go, runs only inside Lima/Docker — see
	// the Dockerfile, which always regenerates before `go build`). objs.RunqLatMax
	// does not exist until the BPF objects are regenerated from the updated
	// bpf/noisy-neighbour.bpf.c that now defines the runq_lat_max ARRAY map.
	//
	// get_hist_bucket() clamps everything ≥ 8.388608 s into overflow bucket 23,
	// so the histogram cannot tell 8 s from 800 s. runq_lat_max keeps the true
	// peak latency the BPF program has seen since boot, unclamped.
	runqLatencyMaxNanoseconds = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ebpf_runq_latency_max_nanoseconds",
		Help: "Largest run-queue latency (ns) the BPF program has recorded since boot, read from the runq_lat_max array map. Captures the true magnitude that the histogram overflow bucket clamps.",
	})
)

// monotonicNowNs returns nanoseconds on CLOCK_MONOTONIC — the same clock
// bpf_ktime_get_ns() uses in noisy-neighbour.bpf.c — so it can be subtracted
// directly from the timestamps stored in runq_enqueued.
//
// (Stdlib-only fallback if x/sys were unavailable: read field 1 of
// /proc/uptime, seconds since boot. That is CLOCK_BOOTTIME, which unlike
// CLOCK_MONOTONIC also counts time spent suspended; on a GKE node that never
// suspends the two are equal, but CLOCK_MONOTONIC is the exact match.)
func monotonicNowNs() (uint64, error) {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
		return 0, err
	}
	return uint64(ts.Sec)*1_000_000_000 + uint64(ts.Nsec), nil
}

// watchRunqEnqueued periodically walks the runq_enqueued BPF map and reports how
// many entries it holds, how old the oldest is, and how many are stale.
//
// runq_enqueued has no expiry: tp_sched_switch only deletes a pid when it
// matches that pid as `next`. A task that gets a sched_wakeup timestamp but
// exits (or whose sched_switch is missed) before being scheduled leaves its
// timestamp behind forever. Under PID reuse an unrelated task can later inherit
// that pid, hit the stale timestamp at sched_switch, and be charged a fabricated
// multi-second run-queue latency with no relationship to real contention. A
// steadily rising entries / max-age / stale count during a load test is that
// leak — see docs/latency-anomaly-investigation.md §4.
func watchRunqEnqueued(objs *bpfObjects) {
	ticker := time.NewTicker(runqEnqueuedPollInterval)
	defer ticker.Stop()

	for range ticker.C {
		nowNs, err := monotonicNowNs()
		if err != nil {
			log.Printf("runq_enqueued probe: clock_gettime(CLOCK_MONOTONIC) failed: %v", err)
			continue
		}

		var (
			pid      uint32
			tsNs     uint64
			entries  uint64
			stale    uint64
			maxAgeNs uint64
		)
		iter := objs.RunqEnqueued.Iterate()
		for iter.Next(&pid, &tsNs) {
			entries++
			// Guard against an entry timestamped after our "now" read
			// (clock read and map walk are not atomic); treat as age 0.
			var ageNs uint64
			if nowNs > tsNs {
				ageNs = nowNs - tsNs
			}
			if ageNs > maxAgeNs {
				maxAgeNs = ageNs
			}
			if time.Duration(ageNs) >= staleAgeThreshold {
				stale++
			}
		}
		if err := iter.Err(); err != nil {
			log.Printf("runq_enqueued probe: map iteration error: %v", err)
			continue
		}

		maxAgeSeconds := time.Duration(maxAgeNs).Seconds()
		runqEnqueuedEntries.Set(float64(entries))
		runqEnqueuedMaxAgeSeconds.Set(maxAgeSeconds)
		runqEnqueuedStaleEntries.Set(float64(stale))

		// Raw-max side channel (deliverable 4). runq_lat_max is a single-element
		// ARRAY map, so key 0 is the only key. Compiles only after `go generate`
		// regenerates objs from the updated BPF C — see the comment on
		// runqLatencyMaxNanoseconds above.
		maxLatMsg := ""
		var maxLatNs uint64
		if err := objs.RunqLatMax.Lookup(uint32(0), &maxLatNs); err != nil {
			log.Printf("runq_lat_max probe: lookup failed: %v", err)
		} else {
			runqLatencyMaxNanoseconds.Set(float64(maxLatNs))
			maxLatMsg = " raw_max=" + time.Duration(maxLatNs).String()
		}

		log.Printf("runq_enqueued probe: entries=%d max_age=%.1fs stale(>%s)=%d%s",
			entries, maxAgeSeconds, staleAgeThreshold, stale, maxLatMsg)
	}
}
