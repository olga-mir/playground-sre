package scenarios

import "time"

func clamp(v *int, min, max int) {
	if *v < min {
		*v = min
	}
	if *v > max {
		*v = max
	}
}

func clamp64(v *int64, min, max int64) {
	if *v < min {
		*v = min
	}
	if *v > max {
		*v = max
	}
}

func clampDuration(v *time.Duration, min, max time.Duration) {
	if *v < min {
		*v = min
	}
	if *v > max {
		*v = max
	}
}
