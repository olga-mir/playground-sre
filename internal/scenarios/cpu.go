// Package scenarios contains the workload implementations for each perf scenario.
// Each function is pure computation with no HTTP or metrics concerns.
package scenarios

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"
)

// CPUParams controls the CPU-bound scenario.
type CPUParams struct {
	// Duration is how long each goroutine runs the hash loop.
	Duration time.Duration
	// Goroutines is the number of parallel SHA-256 workers.
	Goroutines int
}

// CPUResult is returned by CPU.
type CPUResult struct {
	TotalHashes  int64   `json:"total_hashes"`
	HashesPerSec float64 `json:"hashes_per_second"`
	Goroutines   int     `json:"goroutines"`
	DurationMS   int64   `json:"duration_ms"`
}

// CPU runs a SHA-256 proof-of-work style loop across p.Goroutines goroutines
// for p.Duration, then returns aggregate statistics.
//
// This exercises:
//   - GOMAXPROCS scheduling under CPU saturation
//   - GC pressure from hot allocation paths (minimised here via preallocated bufs)
//   - The effect of goroutine count on throughput vs contention
func CPU(ctx context.Context, p CPUParams) CPUResult {
	clamp(&p.Goroutines, 1, 64)
	clampDuration(&p.Duration, time.Millisecond, 30*time.Second)

	var total atomic.Int64
	start := time.Now()
	deadline := start.Add(p.Duration)

	var wg sync.WaitGroup
	for i := range p.Goroutines {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			// Preallocate to avoid per-iteration heap escapes.
			in := make([]byte, 8)
			out := make([]byte, 0, sha256.Size)
			h := sha256.New()
			nonce := uint64(seed) << 32

			for time.Now().Before(deadline) {
				binary.LittleEndian.PutUint64(in, nonce)
				h.Reset()
				h.Write(in)
				out = h.Sum(out[:0]) // reuse backing array
				nonce++
				total.Add(1)
			}
		}(i)
	}
	wg.Wait()

	elapsed := time.Since(start)
	n := total.Load()
	return CPUResult{
		TotalHashes:  n,
		HashesPerSec: float64(n) / elapsed.Seconds(),
		Goroutines:   p.Goroutines,
		DurationMS:   elapsed.Milliseconds(),
	}
}
