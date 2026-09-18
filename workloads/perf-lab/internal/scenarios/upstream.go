package scenarios

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"
)

// UpstreamParams controls the upstream fan-out scenario.
type UpstreamParams struct {
	// TargetURL is the URL each goroutine calls. Defaults to the server's own
	// sleep endpoint so the scenario is self-contained.
	TargetURL string
	// N is the number of concurrent calls.
	N int
	// Timeout is the per-call HTTP timeout.
	Timeout time.Duration
}

// UpstreamResult is returned by Upstream.
type UpstreamResult struct {
	N               int     `json:"n"`
	Successful      int     `json:"successful"`
	Failed          int     `json:"failed"`
	P50MS           float64 `json:"p50_ms"`
	P95MS           float64 `json:"p95_ms"`
	P99MS           float64 `json:"p99_ms"`
	WallDurationMS  int64   `json:"wall_duration_ms"`
}

// Upstream fires p.N concurrent HTTP GET requests to p.TargetURL and returns
// aggregate latency percentiles.
//
// This exercises:
//   - Connection pool behaviour under concurrent load
//   - Goroutine scheduling with blocking I/O
//   - The effect of upstream latency on server tail latency
//   - Error propagation from upstream services
func Upstream(ctx context.Context, p UpstreamParams) UpstreamResult {
	clamp(&p.N, 1, 500)
	clampDuration(&p.Timeout, 100*time.Millisecond, 60*time.Second)

	type callResult struct {
		durationMS float64
		err        error
	}

	results := make([]callResult, p.N)
	var wg sync.WaitGroup
	client := &http.Client{Timeout: p.Timeout}
	wall := time.Now()

	for i := range p.N {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			start := time.Now()

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.TargetURL, nil)
			if err != nil {
				results[idx] = callResult{err: err}
				return
			}

			resp, err := client.Do(req)
			ms := float64(time.Since(start).Microseconds()) / 1000.0
			if err != nil {
				results[idx] = callResult{durationMS: ms, err: err}
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()

			if resp.StatusCode >= 400 {
				results[idx] = callResult{durationMS: ms, err: fmt.Errorf("HTTP %d", resp.StatusCode)}
				return
			}
			results[idx] = callResult{durationMS: ms}
		}(i)
	}
	wg.Wait()

	var durations []float64
	var failed int
	for _, r := range results {
		if r.err != nil {
			failed++
		} else {
			durations = append(durations, r.durationMS)
		}
	}
	sort.Float64s(durations)

	return UpstreamResult{
		N:              p.N,
		Successful:     p.N - failed,
		Failed:         failed,
		P50MS:          percentile(durations, 50),
		P95MS:          percentile(durations, 95),
		P99MS:          percentile(durations, 99),
		WallDurationMS: time.Since(wall).Milliseconds(),
	}
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p / 100 * float64(len(sorted)))
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
