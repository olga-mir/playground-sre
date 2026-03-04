package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/olga-mir/playground-sre/internal/scenarios"
	"github.com/olga-mir/playground-sre/internal/telemetry"
)

// scenarioMetrics holds OTEL instruments for scenario-specific observability.
// These are created after telemetry.New() sets the global MeterProvider.
type scenarioMetrics struct {
	cpuHashesTotal     metric.Int64Counter
	upstreamCallsTotal metric.Int64Counter
	diskBytesWritten   metric.Int64Counter
	diskBytesRead      metric.Int64Counter
	fanoutTasksTotal   metric.Int64Counter
}

func newScenarioMetrics() (*scenarioMetrics, error) {
	m := telemetry.Meter("perf-lab/scenarios")

	cpuHashes, err := m.Int64Counter("scenario_cpu_hashes_total",
		metric.WithDescription("SHA-256 hashes computed across all CPU scenario calls"))
	if err != nil {
		return nil, err
	}

	upstreamCalls, err := m.Int64Counter("scenario_upstream_calls_total",
		metric.WithDescription("Upstream HTTP calls made, labelled by status=ok|error"))
	if err != nil {
		return nil, err
	}

	diskWritten, err := m.Int64Counter("scenario_disk_bytes_written_total",
		metric.WithDescription("Bytes written in disk scenario calls"),
		metric.WithUnit("By"))
	if err != nil {
		return nil, err
	}

	diskRead, err := m.Int64Counter("scenario_disk_bytes_read_total",
		metric.WithDescription("Bytes read in disk scenario calls"),
		metric.WithUnit("By"))
	if err != nil {
		return nil, err
	}

	fanoutTasks, err := m.Int64Counter("scenario_fanout_hashes_total",
		metric.WithDescription("SHA-256 hashes completed across all fanout scenario calls"))
	if err != nil {
		return nil, err
	}

	return &scenarioMetrics{
		cpuHashesTotal:     cpuHashes,
		upstreamCallsTotal: upstreamCalls,
		diskBytesWritten:   diskWritten,
		diskBytesRead:      diskRead,
		fanoutTasksTotal:   fanoutTasks,
	}, nil
}

// --- CPU handler ---

func (app *application) cpuHandler(w http.ResponseWriter, r *http.Request) {
	p := scenarios.CPUParams{
		Duration:   parseDuration(r, "duration", 2*time.Second),
		Goroutines: parseInt(r, "goroutines", 1),
	}

	result := scenarios.CPU(r.Context(), p)

	app.scenMetrics.cpuHashesTotal.Add(r.Context(), result.TotalHashes)

	app.writeJSON(w, http.StatusOK, envelope{
		"scenario": "cpu",
		"params": map[string]any{
			"duration_ms": p.Duration.Milliseconds(),
			"goroutines":  p.Goroutines,
		},
		"result": result,
	}, nil)
}

// --- Sleep handler ---

func (app *application) sleepHandler(w http.ResponseWriter, r *http.Request) {
	d := parseDuration(r, "duration", 100*time.Millisecond)

	result := scenarios.Sleep(r.Context(), d)

	app.writeJSON(w, http.StatusOK, envelope{
		"scenario": "sleep",
		"params":   map[string]any{"duration_ms": d.Milliseconds()},
		"result":   result,
	}, nil)
}

// --- Upstream handler ---

func (app *application) upstreamHandler(w http.ResponseWriter, r *http.Request) {
	// Default target: call our own sleep endpoint (self-contained by default).
	defaultTarget := fmt.Sprintf("http://localhost%s/v1/scenarios/sleep?duration=50ms",
		app.config.ServerAddress)

	p := scenarios.UpstreamParams{
		TargetURL: r.URL.Query().Get("target"),
		N:         parseInt(r, "n", 5),
		Timeout:   parseDuration(r, "timeout", 5*time.Second),
	}
	if p.TargetURL == "" {
		p.TargetURL = defaultTarget
	}

	result := scenarios.Upstream(r.Context(), p)

	ctx := r.Context()
	app.scenMetrics.upstreamCallsTotal.Add(ctx, int64(result.Successful),
		metric.WithAttributes(attribute.String("status", "ok")))
	if result.Failed > 0 {
		app.scenMetrics.upstreamCallsTotal.Add(ctx, int64(result.Failed),
			metric.WithAttributes(attribute.String("status", "error")))
	}

	app.writeJSON(w, http.StatusOK, envelope{
		"scenario": "upstream",
		"params": map[string]any{
			"target":     p.TargetURL,
			"n":          p.N,
			"timeout_ms": p.Timeout.Milliseconds(),
		},
		"result": result,
	}, nil)
}

// --- Disk handler ---

func (app *application) diskHandler(w http.ResponseWriter, r *http.Request) {
	p := scenarios.DiskParams{
		SizeBytes: parseSize(r, "size", 1*1024*1024), // default 1 MiB
		Sync:      r.URL.Query().Get("sync") == "true",
	}

	result, err := scenarios.Disk(r.Context(), p)
	if err != nil {
		app.serverErrorResponse(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	ctx := r.Context()
	app.scenMetrics.diskBytesWritten.Add(ctx, result.SizeBytes)
	app.scenMetrics.diskBytesRead.Add(ctx, result.SizeBytes)

	app.writeJSON(w, http.StatusOK, envelope{
		"scenario": "disk",
		"params": map[string]any{
			"size_bytes": p.SizeBytes,
			"sync":       p.Sync,
		},
		"result": result,
	}, nil)
}

// --- Fanout handler ---

func (app *application) fanoutHandler(w http.ResponseWriter, r *http.Request) {
	p := scenarios.FanoutParams{
		Workers:      parseInt(r, "workers", 10),
		TaskDuration: parseDuration(r, "task_duration", 100*time.Millisecond),
	}

	result := scenarios.Fanout(r.Context(), p)

	app.scenMetrics.fanoutTasksTotal.Add(r.Context(), result.TotalHashes)

	app.writeJSON(w, http.StatusOK, envelope{
		"scenario": "fanout",
		"params": map[string]any{
			"workers":        p.Workers,
			"task_duration_ms": p.TaskDuration.Milliseconds(),
		},
		"result": result,
	}, nil)
}

// --- Query param helpers ---

func parseDuration(r *http.Request, key string, def time.Duration) time.Duration {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func parseInt(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// parseSize parses a human-readable byte size (e.g. "4mb", "512kb", "1024").
func parseSize(r *http.Request, key string, def int64) int64 {
	v := strings.ToLower(strings.TrimSpace(r.URL.Query().Get(key)))
	if v == "" {
		return def
	}
	units := map[string]int64{
		"gb": 1 << 30,
		"mb": 1 << 20,
		"kb": 1 << 10,
		"b":  1,
	}
	for suffix, mult := range units {
		if strings.HasSuffix(v, suffix) {
			n, err := strconv.ParseInt(strings.TrimSuffix(v, suffix), 10, 64)
			if err != nil {
				return def
			}
			return n * mult
		}
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}
