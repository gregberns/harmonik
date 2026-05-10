package handlercontract

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// provisioningRetry — per-bead helper prefix for test helpers in
// provisioningretry_hc048a_test.go (implementer-protocol.md §Helper-prefix
// discipline; bead hk-8i31.57).

// ─────────────────────────────────────────────────────────────────────────────
// HC-048a — Retry with backoff on transient provisioning failure
// ─────────────────────────────────────────────────────────────────────────────

// ProvisioningBackoffConfig holds the exponential-backoff parameters for
// transient provisioning retries per HC-048a.
//
// The default values match the spec: base 1s, cap 16s, max 4 attempts.
//
// Spec: specs/handler-contract.md §4.11.HC-048a.
type ProvisioningBackoffConfig struct {
	// Base is the initial backoff interval.  Spec default: 1s.
	Base time.Duration

	// Cap is the maximum per-attempt backoff interval.  Spec default: 16s.
	Cap time.Duration

	// MaxAttempts is the maximum number of provisioning attempts (including the
	// initial attempt).  Spec default: 4.
	MaxAttempts int
}

// DefaultProvisioningBackoffConfig is the backoff configuration mandated by
// HC-048a: base 1s, cap 16s, max 4 attempts.
//
// Spec: specs/handler-contract.md §4.11.HC-048a.
var DefaultProvisioningBackoffConfig = ProvisioningBackoffConfig{
	Base:        1 * time.Second,
	Cap:         16 * time.Second,
	MaxAttempts: 4,
}

// ProvisionFunc is the signature of the provisioning operation supplied to
// RetryProvisionWithBackoff.  It attempts to provision a single resolved
// skill into the agent-process shape.
//
// The function MUST classify its error per the adapter's per-agent-type
// heuristic (HC-048a):
//   - Transient conditions (network errors, 5xx, timeout) → errors wrapping ErrTransient.
//   - Structural conditions (integrity failure, unsupported manifest, permission denied)
//     → errors wrapping ErrSkillProvisioningFailed (which wraps ErrStructural).
//
// Spec: specs/handler-contract.md §4.11.HC-048a.
type ProvisionFunc func(ctx context.Context, skill ResolvedSkill) error

// RetryProvisionWithBackoff provisions a single skill with transient-retry
// backoff per HC-048a.
//
// Algorithm:
//  1. Call provision(ctx, skill).
//  2. If it succeeds, return nil.
//  3. If the error wraps ErrTransient: apply exponential backoff and retry up to
//     cfg.MaxAttempts total attempts, bounded by provisioningTimeout.
//  4. If the error wraps ErrSkillProvisioningFailed or ErrStructural (structural
//     classification): return immediately — no retry.
//  5. On attempt-cap exhaustion or provisioningTimeout expiry: reclassify to
//     ErrStructural and return ErrSkillProvisioningFailed per HC-048a §8.2.
//
// provisioningTimeout is distinct from LaunchSpec.Timeout (per HC-048a) and
// MUST be positive.
//
// Spec: specs/handler-contract.md §4.11.HC-048a.
func RetryProvisionWithBackoff(
	ctx context.Context,
	skill ResolvedSkill,
	provision ProvisionFunc,
	cfg ProvisioningBackoffConfig,
	provisioningTimeout time.Duration,
) error {
	deadline := time.Now().Add(provisioningTimeout)
	backoff := cfg.Base

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		err := provision(ctx, skill)
		if err == nil {
			return nil
		}

		// Structural classification — do not retry.
		if isStructuralProvisioningError(err) {
			return err
		}

		// Transient classification — check if we've exhausted attempts or time.
		if attempt >= cfg.MaxAttempts {
			// Attempt-cap exhaustion: reclassify to ErrStructural per HC-048a.
			return fmt.Errorf(
				"handlercontract: HC-048a: skill %q: transient provisioning failed after %d attempts: %w",
				skill.Name, cfg.MaxAttempts, ErrSkillProvisioningFailed,
			)
		}

		// Wait backoff interval, but stop early if provisioningTimeout elapses or
		// ctx is cancelled.
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf(
				"handlercontract: HC-048a: skill %q: provisioning_timeout exceeded: %w",
				skill.Name, ErrSkillProvisioningFailed,
			)
		}
		waitDur := backoff
		if waitDur > remaining {
			waitDur = remaining
		}

		timer := time.NewTimer(waitDur)
		select {
		case <-timer.C:
			// Backoff elapsed; continue to next attempt.
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf(
				"handlercontract: HC-048a: skill %q: context cancelled during provisioning: %w",
				skill.Name, ErrCanceled,
			)
		}
		timer.Stop()

		// Check time budget after waiting.
		if time.Now().After(deadline) {
			return fmt.Errorf(
				"handlercontract: HC-048a: skill %q: provisioning_timeout exceeded after backoff: %w",
				skill.Name, ErrSkillProvisioningFailed,
			)
		}

		// Advance backoff: double each attempt, cap at cfg.Cap.
		backoff *= 2
		if backoff > cfg.Cap {
			backoff = cfg.Cap
		}
	}

	// Should be unreachable (covered by attempt-cap branch above), but
	// satisfies the compiler and defends against off-by-one drift.
	return fmt.Errorf(
		"handlercontract: HC-048a: skill %q: provisioning failed: %w",
		skill.Name, ErrSkillProvisioningFailed,
	)
}

// isStructuralProvisioningError reports whether err represents a structural
// provisioning failure (wraps ErrSkillProvisioningFailed or ErrStructural
// directly) that MUST NOT be retried per HC-048a.
func isStructuralProvisioningError(err error) bool {
	// ErrSkillProvisioningFailed already wraps ErrStructural, so testing either
	// catches structural sub-sentinels.
	if errors.Is(err, ErrSkillProvisioningFailed) {
		return true
	}
	// Bare ErrStructural without sub-sentinel also stops retries.
	if errors.Is(err, ErrStructural) {
		return true
	}
	return false
}
