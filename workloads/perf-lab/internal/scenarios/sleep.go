package scenarios

import (
	"context"
	"time"
)

// SleepResult is returned by Sleep.
type SleepResult struct {
	RequestedMS int64 `json:"requested_ms"`
	ActualMS    int64 `json:"actual_ms"`
}

// Sleep blocks for d (or until ctx is cancelled) and reports the actual elapsed time.
//
// Use this as a baseline to measure server overhead and scheduling jitter, or to
// simulate long-running requests without burning CPU.
func Sleep(ctx context.Context, d time.Duration) SleepResult {
	clampDuration(&d, time.Millisecond, 60*time.Second)

	requested := d.Milliseconds()
	start := time.Now()

	select {
	case <-time.After(d):
	case <-ctx.Done():
	}

	return SleepResult{
		RequestedMS: requested,
		ActualMS:    time.Since(start).Milliseconds(),
	}
}
