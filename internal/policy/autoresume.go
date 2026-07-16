package policy

import "time"

// autoresume.go — the pure auto-resume backoff computation for handler-pause.
//
// Moved out of internal/daemon/handlerpause_autoresume_0otqs.go (hk-0otqs)
// backoffDurationLocked, de-"Locked"d: the daemon controller still owns the
// timer goroutine, the flap-window detection, the per-entry attempt counter,
// the Diagnose call, and Resume. Only the exponential-backoff arithmetic — the
// decision "given a base delay and an attempt count, how long until the next
// auto-resume?" — lives here.
//
// Spec ref: specs/handler-pause.md §1.2 (post-MVH auto-resume).

// DefaultAutoResumeMaxBackoff is the maximum per-attempt backoff duration when
// AutoResumeParams.MaxBackoff is zero or negative.
const DefaultAutoResumeMaxBackoff = 30 * time.Minute

// AutoResumeParams are the inputs to BackoffDuration.
type AutoResumeParams struct {
	// Base is the caller-supplied delay before backoff is applied
	// (the provider's retry_after window in the daemon).
	Base time.Duration
	// Attempts is the number of consecutive flap attempts observed for this
	// handler type. Zero means no flapping (no backoff applied).
	Attempts int
	// MaxBackoff caps the returned duration. When zero or negative,
	// DefaultAutoResumeMaxBackoff applies.
	MaxBackoff time.Duration
}

// BackoffDuration computes the effective auto-resume delay.
//
// Formula (preserved exactly from backoffDurationLocked): effective =
// Base * 2^Attempts, capped at the effective MaxBackoff. With Attempts <= 0 the
// Base is returned unchanged (and is NOT capped, matching the original guard).
// The doubling loop stops early once the cap is reached and guards against
// overflow (a non-positive or over-cap doubling snaps to the cap).
func BackoffDuration(p AutoResumeParams) time.Duration {
	if p.Attempts <= 0 {
		return p.Base
	}
	maxBackoff := p.MaxBackoff
	if maxBackoff <= 0 {
		maxBackoff = DefaultAutoResumeMaxBackoff
	}
	// Shift left by attempts, but cap to avoid overflow.
	shifted := p.Base
	for i := 0; i < p.Attempts && shifted < maxBackoff; i++ {
		next := shifted * 2
		if next <= 0 || next > maxBackoff { // overflow guard
			shifted = maxBackoff
			break
		}
		shifted = next
	}
	if shifted > maxBackoff {
		shifted = maxBackoff
	}
	return shifted
}
