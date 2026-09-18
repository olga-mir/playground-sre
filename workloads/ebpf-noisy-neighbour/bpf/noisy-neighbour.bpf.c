#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

/// ###### Implementation idea and explanation:
/// https://netflixtechblog.com/noisy-neighbor-detection-with-ebpf-64b1f4b3bbdd


#define MAX_TASK_ENTRIES 20000
#define MAX_HIST_ENTRIES 50000
#define MIN_RUNQ_LAT_NS 1000000 // 1 ms threshold to record significant delays
#define NUM_BUCKETS 24

typedef __u32 u32;
typedef __u64 u64;

void bpf_rcu_read_lock(void) __ksym;
void bpf_rcu_read_unlock(void) __ksym;

static __u64 get_task_cgroup_id(struct task_struct *task)
{
    struct css_set *cgroups;
    __u64 cgroup_id;

    bpf_rcu_read_lock();
    cgroups = BPF_CORE_READ(task, cgroups);
    cgroup_id = BPF_CORE_READ(cgroups, dfl_cgrp, kn, id);
    bpf_rcu_read_unlock();

    return cgroup_id;
}

struct runq_hist_key {
    u64 cgroup_id;
    u64 prev_cgroup_id;
    u32 bucket;
};

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_HIST_ENTRIES);
    __uint(key_size, sizeof(struct runq_hist_key));
    __uint(value_size, sizeof(u64));
} runq_histograms SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_TASK_ENTRIES);
    __uint(key_size, sizeof(u32));
    __uint(value_size, sizeof(u64));
} runq_enqueued SEC(".maps");

// Raw-max side channel (single u64 at key 0). get_hist_bucket() clamps every
// latency >= 8.388608s into the unbounded overflow bucket 23, so the histogram
// cannot tell 8s from 800s. This map keeps the true maximum runq_lat ever seen
// so userspace (ebpf_runq_latency_max_nanoseconds) can report the real
// magnitude. See docs/latency-anomaly-investigation.md section 1.
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 1);
    __uint(key_size, sizeof(u32));
    __uint(value_size, sizeof(u64));
} runq_lat_max SEC(".maps");

static __always_inline u32 get_hist_bucket(u64 lat_ns)
{
    // Buckets mirror Prometheus ExponentialBuckets(1000, 2, 24)
    // bucket 0: < 2µs, bucket 1: < 4µs, ..., bucket 23: >= ~8.3s
    u64 v = lat_ns / 1000;
    if (v == 0) return 0;

    u32 b = 0;
    while (v > 1 && b < (NUM_BUCKETS - 1)) {
        v >>= 1;
        b++;
    }
    return b;
}

SEC("tp_btf/sched_wakeup")
int tp_sched_wakeup(u64 *ctx)
{
    // tp_btf context is the raw tracepoint args array; sched_wakeup's single
    // arg (struct task_struct *p) is ctx[0], NOT ctx itself. A refactor
    // (03888db) dropped the [0] and read pid from the args-array address plus
    // task_struct's pid offset — garbage. Almost every entry was then inserted
    // under a bogus key that tp_sched_switch (correctly using ctx[2]) could
    // never match or delete, so runq_enqueued grew without bound and the rare
    // coincidental match produced the fabricated multi-second run-queue latency.
    // This is the root cause tracked in docs/latency-anomaly-investigation.md §4.
    struct task_struct *task = (struct task_struct *)ctx[0];
    u32 pid = BPF_CORE_READ(task, pid);
    u64 ts = bpf_ktime_get_ns();

    bpf_map_update_elem(&runq_enqueued, &pid, &ts, BPF_NOEXIST);
    return 0;
}

SEC("tp_btf/sched_switch")
int tp_sched_switch(__u64 *ctx)
{
    struct task_struct *prev = (struct task_struct *)ctx[1];
    struct task_struct *next = (struct task_struct *)ctx[2];
    u32 next_pid = BPF_CORE_READ(next, pid);

    // fetch timestamp of when the next task was enqueued
    u64 *tsp = bpf_map_lookup_elem(&runq_enqueued, &next_pid);
    if (tsp == NULL) {
        return 0; // missed enqueue
    }

    // calculate runq latency before deleting the stored timestamp
    u64 now = bpf_ktime_get_ns();
    u64 runq_lat = now - *tsp;

    // delete pid from enqueued map
    bpf_map_delete_elem(&runq_enqueued, &next_pid);

    // Filter out minor scheduling delays to focus on heavy-hitter noisy neighbor interference
    if (runq_lat < MIN_RUNQ_LAT_NS) {
        return 0;
    }

    // Raw-max side channel: keep the largest runq_lat ever seen (>= MIN_RUNQ_LAT_NS,
    // the same events the histogram bins) so the true magnitude survives the
    // histogram's unbounded overflow bucket 23. Userspace reads this as
    // ebpf_runq_latency_max_nanoseconds.
    //
    // Plain read-compare-write rather than __sync_val_compare_and_swap: the
    // atomic CMPXCHG lowering needs BPF ISA v3, which this project's bpf2go
    // cflags don't enable, and turning it on would change codegen for the whole
    // (working) program. The only race here is two CPUs seeing the same old max
    // and the smaller write landing last, which understates the gauge by one
    // sample until the next larger latency — acceptable for a debug "max seen".
    u32 max_key = 0;
    u64 *max_lat = bpf_map_lookup_elem(&runq_lat_max, &max_key);
    if (max_lat && runq_lat > *max_lat) {
        *max_lat = runq_lat;
    }

    u64 prev_cgroup_id = get_task_cgroup_id(prev);
    u64 cgroup_id = get_task_cgroup_id(next);

    struct runq_hist_key key = {
        .cgroup_id = cgroup_id,
        .prev_cgroup_id = prev_cgroup_id,
        .bucket = get_hist_bucket(runq_lat),
    };

    u64 *count = bpf_map_lookup_elem(&runq_histograms, &key);
    if (count) {
        __sync_fetch_and_add(count, 1);
    } else {
        u64 initial_count = 1;
        bpf_map_update_elem(&runq_histograms, &key, &initial_count, BPF_NOEXIST);
    }

    return 0;
}

// Proactive orphan cleanup for runq_enqueued — see
// docs/latency-anomaly-investigation.md §4.
//
// tp_sched_switch only removes a pid from runq_enqueued when that pid is matched
// as `next`. A task that gets a sched_wakeup timestamp but exits before it is
// ever scheduled (or whose sched_switch is simply missed) leaves its timestamp
// in the map forever. Under PID reuse an unrelated task later inherits that pid,
// hits the stale timestamp in tp_sched_switch, and is charged a fabricated
// multi-second (observed: multi-*minute*) run-queue latency. Short-lived tasks
// exiting between wakeup and first schedule are the dominant source of those
// orphans; deleting the entry on task exit removes them.
//
// sched_process_exit fires from do_exit() for every task (thread), not just the
// thread-group leader, so per-thread orphans are covered too. Like sched_wakeup,
// the task_struct* is ctx[0]. With tp_sched_wakeup's context bug fixed, matched
// entries are already deleted in tp_sched_switch; this hook only mops up the
// genuine orphan — a task woken but never scheduled before it exits.
SEC("tp_btf/sched_process_exit")
int tp_sched_process_exit(u64 *ctx)
{
    struct task_struct *p = (struct task_struct *)ctx[0];
    u32 pid = BPF_CORE_READ(p, pid);
    bpf_map_delete_elem(&runq_enqueued, &pid);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";