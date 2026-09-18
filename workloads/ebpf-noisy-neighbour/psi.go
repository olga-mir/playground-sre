package main

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"
)

// Linux Pressure Stall Information (PSI) exposes, per resource, how much wall
// time tasks lost waiting on that resource. It is plain-text file parsing in
// userspace — no BPF involved — and is the cheap second signal called for in
// docs/proposal-non-cpu-noisy-neighbours.md §5 and the Track B follow-up in
// docs/latency-anomaly-investigation.md §5.
//
// File format (one file per resource, e.g. /proc/pressure/io):
//
//	some avg10=0.00 avg60=0.00 avg300=0.00 total=12345
//	full avg10=0.00 avg60=0.00 avg300=0.00 total=6789
//
// `some` = at least one task stalled; `full` = every runnable task stalled.
// /proc/pressure/cpu has no `full` line on older kernels — that is handled by
// simply emitting whatever lines are present.

// psiResources are the three pressure files the kernel exposes, both node-wide
// (<PSI_PROC_PATH>/<res>) and per-cgroup (<cgroup>/<res>.pressure).
var psiResources = []string{"cpu", "io", "memory"}

// psiProcPath is the directory holding the node-level PSI files. It is an env
// var (default /proc/pressure) so the DaemonSet can point it at a hostPath
// mount of the host's /proc/pressure (see k8s/ebpf-daemonset.yaml).
func psiProcPath() string {
	if p := os.Getenv("PSI_PROC_PATH"); p != "" {
		return p
	}
	return "/proc/pressure"
}

// psiCollector implements prometheus.Collector for node and per-cgroup PSI.
//
// Cgroup series are held to pod/* cgroups only, mirroring the
// strings.HasPrefix(cgroup, "pod/") cardinality guard runqCollector uses.
// Labels are resolved through the shared cgroupMapper (the same inode->label
// path runqCollector takes), falling back to cgroupLabel() for directories the
// mapper's 30s refresh has not picked up yet.
type psiCollector struct {
	mapper *cgroupMapper

	ratio *prometheus.Desc
	stall *prometheus.Desc
}

func newPSICollector(mapper *cgroupMapper) *psiCollector {
	return &psiCollector{
		mapper: mapper,
		ratio: prometheus.NewDesc(
			"ebpf_psi_pressure_ratio",
			"Linux PSI stall ratio: the kernel's avgN figure, i.e. percent of wall time (0-100) tasks were stalled on the resource over the trailing window",
			[]string{"resource", "kind", "window", "scope", "cgroup"},
			nil,
		),
		stall: prometheus.NewDesc(
			"ebpf_psi_stall_seconds_total",
			"Cumulative time tasks were stalled on the resource, from a PSI file's total= field (microseconds converted to seconds)",
			[]string{"resource", "kind", "scope", "cgroup"},
			nil,
		),
	}
}

func (c *psiCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.ratio
	ch <- c.stall
}

func (c *psiCollector) Collect(ch chan<- prometheus.Metric) {
	c.collectNode(ch)
	c.collectCgroups(ch)
}

// collectNode reads <PSI_PROC_PATH>/{cpu,io,memory}.
func (c *psiCollector) collectNode(ch chan<- prometheus.Metric) {
	dir := psiProcPath()
	for _, res := range psiResources {
		c.emitFile(ch, filepath.Join(dir, res), res, "node", "")
	}
}

// collectCgroups walks /sys/fs/cgroup (already mounted read-only) and reads
// {cpu,io,memory}.pressure for every directory that maps to a pod/* label.
func (c *psiCollector) collectCgroups(ch chan<- prometheus.Metric) {
	seen := make(map[string]bool)
	_ = filepath.WalkDir(cgroupRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		label := c.cgroupLabelFor(path, d)
		if !strings.HasPrefix(label, "pod/") || seen[label] {
			return nil
		}
		seen[label] = true
		for _, res := range psiResources {
			c.emitFile(ch, filepath.Join(path, res+".pressure"), res, "cgroup", label)
		}
		return nil
	})
}

// cgroupLabelFor resolves a cgroup directory to its low-cardinality label the
// same way runqCollector does — through the shared cgroupMapper keyed by the
// directory inode — falling back to a direct cgroupLabel() computation when the
// mapper has not seen this directory yet.
func (c *psiCollector) cgroupLabelFor(path string, d os.DirEntry) string {
	if info, err := d.Info(); err == nil {
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			if label := c.mapper.name(st.Ino); label != "" && label != "unresolved" {
				return label
			}
		}
	}
	return cgroupLabel(path)
}

// emitFile parses one PSI file and streams its metrics. A missing file is
// normal (PSI disabled in the kernel, or no io.pressure on a given cgroup) and
// is silently ignored; a missing "full" line is fine too.
func (c *psiCollector) emitFile(ch chan<- prometheus.Metric, file, resource, scope, cgroup string) {
	f, err := os.Open(file)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		kind := fields[0] // "some" or "full"
		if kind != "some" && kind != "full" {
			continue
		}
		for _, kv := range fields[1:] {
			key, val, ok := strings.Cut(kv, "=")
			if !ok {
				continue
			}
			switch key {
			case "avg10", "avg60", "avg300":
				v, err := strconv.ParseFloat(val, 64)
				if err != nil {
					continue
				}
				ch <- prometheus.MustNewConstMetric(
					c.ratio, prometheus.GaugeValue, v,
					resource, kind, key, scope, cgroup,
				)
			case "total":
				usec, err := strconv.ParseUint(val, 10, 64)
				if err != nil {
					continue
				}
				ch <- prometheus.MustNewConstMetric(
					c.stall, prometheus.CounterValue, float64(usec)/1e6,
					resource, kind, scope, cgroup,
				)
			}
		}
	}
	if err := sc.Err(); err != nil {
		log.Printf("psi: error reading %s: %v", file, err)
	}
}
