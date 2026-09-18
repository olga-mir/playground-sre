package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/rlimit"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

//go:generate bpf2go -cc clang -cflags "-O2 -g -Wall -Werror" bpf bpf/noisy-neighbour.bpf.c -- -I/usr/include/bpf -I/usr/include

const (
	cgroupRoot = "/sys/fs/cgroup"
	numBuckets = 24
)

// bucketBoundsNs[b] = 1000 * 2^b nanoseconds, matching ExponentialBuckets(1000, 2, 24).
var bucketBoundsNs = func() [numBuckets]float64 {
	var b [numBuckets]float64
	v := 1000.0
	for i := range b {
		b[i] = v
		v *= 2
	}
	return b
}()

var eventsTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "ebpf_events_total",
	Help: "Total scheduling events observed in BPF run-queue histogram",
})

// runqHistKey matches the memory layout of C struct runq_hist_key in noisy-neighbour.bpf.c.
type runqHistKey struct {
	CgroupID     uint64
	PrevCgroupID uint64
	Bucket       uint32
	_            [4]byte
}

// runqCollector implements prometheus.Collector for the run-queue latency histogram.
// It reads BPF bucket counts directly and emits MustNewConstHistogram, avoiding
// the O(count) overhead of calling Observe() for every accumulated event.
type runqCollector struct {
	desc   *prometheus.Desc
	objs   *bpfObjects
	mapper *cgroupMapper
}

func newRunqCollector(objs *bpfObjects, mapper *cgroupMapper) *runqCollector {
	return &runqCollector{
		desc: prometheus.NewDesc(
			"ebpf_runq_latency_nanoseconds",
			"Run queue latency in nanoseconds, labeled by the scheduled cgroup and the cgroup it preempted",
			[]string{"cgroup", "prev_cgroup"},
			nil,
		),
		objs:   objs,
		mapper: mapper,
	}
}

func (c *runqCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.desc
}

func (c *runqCollector) Collect(ch chan<- prometheus.Metric) {
	type pair struct{ cgroup, prev string }
	type bucketArr [numBuckets]uint64
	grouped := make(map[pair]*bucketArr)

	var key runqHistKey
	var val uint64
	iter := c.objs.RunqHistograms.Iterate()
	for iter.Next(&key, &val) {
		if val == 0 || key.Bucket >= numBuckets {
			continue
		}
		cgroup := c.mapper.name(key.CgroupID)
		if !strings.HasPrefix(cgroup, "pod/") {
			continue
		}
		p := pair{cgroup, c.mapper.name(key.PrevCgroupID)}
		if grouped[p] == nil {
			arr := bucketArr{}
			grouped[p] = &arr
		}
		grouped[p][key.Bucket] += val
	}
	if err := iter.Err(); err != nil {
		log.Printf("Error iterating BPF histogram map: %v", err)
	}

	for p, counts := range grouped {
		// BPF bucket b covers [bucketBoundsNs[b], bucketBoundsNs[b+1]).
		// Events in bucket b have latency ≥ bucketBoundsNs[b], so they belong
		// in Prometheus le=bucketBoundsNs[b+1] (one level up), not le=bucketBoundsNs[b].
		//
		// Cumulative Prometheus count for le=bucketBoundsNs[b] is therefore the
		// sum of BPF bucket counts for k ∈ [0, b-1].
		cumulBuckets := make(map[float64]uint64, numBuckets)

		// le[0]: no BPF bucket has events with latency ≤ bucketBoundsNs[0].
		cumulBuckets[bucketBoundsNs[0]] = 0

		var cumul uint64
		for b := 1; b < numBuckets; b++ {
			cumul += counts[b-1]
			cumulBuckets[bucketBoundsNs[b]] = cumul
		}
		// BPF bucket 23 is the overflow catch-all: latency ≥ bucketBoundsNs[23]
		// (≈ 8.388608 s) with NO upper bound. It must NOT be folded into the
		// finite le=bucketBoundsNs[23] key — doing so fabricates an 8.39 s
		// ceiling for events that might really be 8 s or 800 s. Every finite le
		// key is therefore left holding only bounded (BPF bucket ≤ 22)
		// observations; the overflow events reach the series solely through the
		// implicit le="+Inf" bucket that MustNewConstHistogram derives from
		// totalCount. Consequences downstream:
		//   - histogram_quantile for a p99 that ranks past the last finite
		//     bucket resolves to the edge of the unbounded region (+Inf, or
		//     ≈ 8.39 s depending on the PromQL engine) instead of a fabricated
		//     interpolated value below it.
		//   - _count minus the le=bucketBoundsNs[23] bucket == the number of
		//     unbounded (bucket-23) events; a non-zero gap means "true latency
		//     ceiling unknown — read ebpf_runq_latency_max_nanoseconds".
		totalCount := cumul + counts[numBuckets-1]

		// Approximate _sum from bucket midpoints. Bucket 23 has no upper bound,
		// so each overflow event can only be charged at its lower bound
		// (bucketBoundsNs[23]); whenever counts[23] > 0 this makes _sum a hard
		// underestimate and any rate(_sum)/rate(_count) mean a lower bound only.
		// The raw-max side channel (ebpf_runq_latency_max_nanoseconds) carries
		// the true magnitude in that regime.
		var sum float64
		for b := 0; b < numBuckets-1; b++ {
			mid := (bucketBoundsNs[b] + bucketBoundsNs[b+1]) / 2
			sum += float64(counts[b]) * mid
		}
		sum += float64(counts[numBuckets-1]) * bucketBoundsNs[numBuckets-1]

		ch <- prometheus.MustNewConstHistogram(c.desc, totalCount, sum, cumulBuckets, p.cgroup, p.prev)
	}
}

// cgroupMapper resolves kernel cgroup IDs to human-readable labels.
// In cgroupv2 the cgroup ID equals the inode of the cgroup directory; in GKE
// the inode is the lower 32 bits of kernfs_node.id so we try both.
type cgroupMapper struct {
	mu    sync.RWMutex
	byIno map[uint64]string
}

func newCgroupMapper() *cgroupMapper {
	m := &cgroupMapper{byIno: make(map[uint64]string)}
	m.refresh()
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for range t.C {
			m.refresh()
		}
	}()
	return m
}

func (m *cgroupMapper) refresh() {
	next := make(map[uint64]string)
	_ = filepath.WalkDir(cgroupRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return nil
		}
		next[st.Ino] = cgroupLabel(path)
		return nil
	})
	m.mu.Lock()
	m.byIno = next
	m.mu.Unlock()
}

func (m *cgroupMapper) name(id uint64) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if label, ok := m.byIno[id]; ok {
		return label
	}
	// kernfs_node.id packs generation in upper 32 bits; inode lives in lower 32.
	if label, ok := m.byIno[id&0xFFFFFFFF]; ok {
		return label
	}
	return "unresolved"
}

// cgroupLabel derives a concise Prometheus-friendly label from a cgroup path.
//
// GKE cgroupv2 layout (containerd):
//
//	kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod<uid>.slice/cri-containerd-<cid>.scope
//
// We extract pod-uid prefix and container-id prefix to keep cardinality low.
func cgroupLabel(path string) string {
	rel := strings.TrimPrefix(strings.TrimSuffix(path, "/"), cgroupRoot)
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		return "root"
	}
	parts := strings.Split(rel, "/")
	for i, p := range parts {
		for _, qos := range []string{"kubepods-burstable-pod", "kubepods-besteffort-pod", "kubepods-guaranteed-pod", "kubepods-pod", "pod"} {
			if strings.HasPrefix(p, qos) {
				uid := shortID(strings.TrimSuffix(strings.TrimPrefix(p, qos), ".slice"))
				if i+1 < len(parts) {
					cid := shortContainerID(parts[i+1])
					return "pod/" + uid + "/" + cid
				}
				return "pod/" + uid
			}
		}
	}
	// Fallback: last two path components.
	if len(parts) > 2 {
		return strings.Join(parts[len(parts)-2:], "/")
	}
	return rel
}

func shortID(s string) string {
	s = strings.ReplaceAll(s, "_", "-")
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func shortContainerID(s string) string {
	s = strings.TrimSuffix(s, ".scope")
	for _, pfx := range []string{"cri-containerd-", "docker-"} {
		if strings.HasPrefix(s, pfx) {
			s = strings.TrimPrefix(s, pfx)
			break
		}
	}
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func main() {
	if err := rlimit.RemoveMemlock(); err != nil {
		log.Fatalf("Failed to remove rlimit: %v", err)
	}

	objs := bpfObjects{}
	if err := loadBpfObjects(&objs, nil); err != nil {
		log.Fatalf("Failed to load BPF program: %v", err)
	}
	defer objs.Close()

	wakeupLink, err := link.AttachTracing(link.TracingOptions{
		Program:    objs.TpSchedWakeup,
		AttachType: ebpf.AttachTraceRawTp,
	})
	if err != nil {
		log.Fatalf("Failed to attach sched_wakeup: %v", err)
	}
	defer wakeupLink.Close()

	switchLink, err := link.AttachTracing(link.TracingOptions{
		Program:    objs.TpSchedSwitch,
		AttachType: ebpf.AttachTraceRawTp,
	})
	if err != nil {
		log.Fatalf("Failed to attach sched_switch: %v", err)
	}
	defer switchLink.Close()

	// sched_process_exit deletes a task's runq_enqueued entry when it exits,
	// so a wakeup timestamp for a task that never gets scheduled can't linger
	// and be misread as multi-second run-queue latency after PID reuse.
	// See docs/latency-anomaly-investigation.md §4.
	exitLink, err := link.AttachTracing(link.TracingOptions{
		Program:    objs.TpSchedProcessExit,
		AttachType: ebpf.AttachTraceRawTp,
	})
	if err != nil {
		log.Fatalf("Failed to attach sched_process_exit: %v", err)
	}
	defer exitLink.Close()

	mapper := newCgroupMapper()

	prometheus.MustRegister(newRunqCollector(&objs, mapper))
	prometheus.MustRegister(newPSICollector(mapper))

	go watchRunqEnqueued(&objs) // debug.go: runq_enqueued orphan / PID-reuse leak probe

	go func() {
		http.Handle("/metrics", promhttp.Handler())
		log.Println("Metrics available at :9090/metrics")
		log.Fatal(http.ListenAndServe(":9090", nil))
	}()

	// Poll BPF map periodically to keep eventsTotal up to date.
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		var prevTotal uint64
		for range ticker.C {
			var k runqHistKey
			var v, total uint64
			iter := objs.RunqHistograms.Iterate()
			for iter.Next(&k, &v) {
				total += v
			}
			if total > prevTotal {
				eventsTotal.Add(float64(total - prevTotal))
				prevTotal = total
			}
		}
	}()

	stopper := make(chan os.Signal, 1)
	signal.Notify(stopper, os.Interrupt, syscall.SIGTERM)
	log.Println("Collecting run-queue latency from eBPF map...")
	<-stopper
	log.Println("Shutting down")
}
