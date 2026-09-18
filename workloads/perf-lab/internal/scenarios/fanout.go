package scenarios

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"sync"
	"time"
)

// FanoutParams controls the goroutine fan-out scenario.
type FanoutParams struct {
	// Workers is the number of goroutines spawned.
	Workers int
	// TaskDuration is how long each worker runs its CPU loop.
	TaskDuration time.Duration
}

// FanoutResult is returned by Fanout.
type FanoutResult struct {
	Workers        int     `json:"workers"`
	TotalHashes    int64   `json:"total_hashes"`
	WallDurationMS int64   `json:"wall_duration_ms"`
	// EfficiencyPct measures how close the wall time was to the ideal
	// single-task duration. 100% = perfect parallelism (wall == TaskDuration).
	// Values below 100% indicate scheduling overhead or resource contention.
	EfficiencyPct float64 `json:"efficiency_pct"`
}

// Fanout spawns p.Workers goroutines, each running a SHA-256 loop for
// p.TaskDuration, then waits for all to complete.
//
// This exercises:
//   - Goroutine scheduling overhead at high worker counts
//   - GOMAXPROCS work-stealing under concurrent CPU-bound tasks
//   - The delta between ideal wall time (== TaskDuration) and actual wall time
//   - Memory usage growth with goroutine count (stack allocation)
func Fanout(ctx context.Context, p FanoutParams) FanoutResult {
	clamp(&p.Workers, 1, 10000)
	clampDuration(&p.TaskDuration, time.Millisecond, 10*time.Second)

	type workerResult struct {
		hashes int64
	}
	results := make([]workerResult, p.Workers)

	wall := time.Now()
	var wg sync.WaitGroup

	for i := range p.Workers {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			in := make([]byte, 8)
			out := make([]byte, 0, sha256.Size)
			h := sha256.New()
			nonce := uint64(idx) << 32
			deadline := time.Now().Add(p.TaskDuration)

			var count int64
			for time.Now().Before(deadline) {
				binary.LittleEndian.PutUint64(in, nonce)
				h.Reset()
				h.Write(in)
				out = h.Sum(out[:0])
				nonce++
				count++
			}
			results[idx] = workerResult{hashes: count}
		}(i)
	}
	wg.Wait()

	wallDuration := time.Since(wall)

	var totalHashes int64
	for _, r := range results {
		totalHashes += r.hashes
	}

	// Perfect efficiency: all workers finish exactly at TaskDuration, so
	// wall == TaskDuration. Overhead pushes wall > TaskDuration, dropping
	// efficiency below 100%.
	efficiency := float64(p.TaskDuration) / float64(wallDuration) * 100
	if efficiency > 100 {
		efficiency = 100
	}

	return FanoutResult{
		Workers:        p.Workers,
		TotalHashes:    totalHashes,
		WallDurationMS: wallDuration.Milliseconds(),
		EfficiencyPct:  efficiency,
	}
}
