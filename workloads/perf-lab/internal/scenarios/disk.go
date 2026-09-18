package scenarios

import (
	"context"
	"crypto/rand"
	"os"
	"time"
)

// DiskParams controls the disk I/O scenario.
type DiskParams struct {
	// SizeBytes is the size of the temp file to write and read back.
	SizeBytes int64
	// Sync forces an fsync after the write, making the latency reflect durable storage.
	Sync bool
}

// DiskResult is returned by Disk.
type DiskResult struct {
	SizeBytes      int64   `json:"size_bytes"`
	WriteDurationMS int64  `json:"write_duration_ms"`
	ReadDurationMS  int64  `json:"read_duration_ms"`
	WriteMBps      float64 `json:"write_mbps"`
	ReadMBps       float64 `json:"read_mbps"`
	Synced         bool    `json:"synced"`
}

// Disk writes a random temporary file of p.SizeBytes, optionally fsyncs it,
// then reads it back sequentially, and reports throughput for each phase.
//
// This exercises:
//   - Page-cache vs durable write latency (sync=false vs sync=true)
//   - Read-after-write latency from the OS page cache
//   - The effect of file size on throughput and GC (allocation of large slices)
func Disk(_ context.Context, p DiskParams) (DiskResult, error) {
	clamp64(&p.SizeBytes, 1, 100*1024*1024) // 1 B – 100 MiB

	// Fill with random bytes so the OS cannot silently skip the write.
	data := make([]byte, p.SizeBytes)
	rand.Read(data)

	f, err := os.CreateTemp("", "perf-lab-disk-*")
	if err != nil {
		return DiskResult{}, err
	}
	name := f.Name()
	defer func() {
		f.Close()
		os.Remove(name)
	}()

	// --- Write phase ---
	writeStart := time.Now()
	if _, err := f.Write(data); err != nil {
		return DiskResult{}, err
	}
	if p.Sync {
		if err := f.Sync(); err != nil {
			return DiskResult{}, err
		}
	}
	writeDuration := time.Since(writeStart)

	// --- Read phase ---
	if _, err := f.Seek(0, 0); err != nil {
		return DiskResult{}, err
	}
	readBuf := make([]byte, p.SizeBytes)
	readStart := time.Now()
	if _, err := f.Read(readBuf); err != nil {
		return DiskResult{}, err
	}
	readDuration := time.Since(readStart)

	mb := float64(p.SizeBytes) / (1024 * 1024)
	return DiskResult{
		SizeBytes:       p.SizeBytes,
		WriteDurationMS: writeDuration.Milliseconds(),
		ReadDurationMS:  readDuration.Milliseconds(),
		WriteMBps:       mb / writeDuration.Seconds(),
		ReadMBps:        mb / readDuration.Seconds(),
		Synced:          p.Sync,
	}, nil
}
