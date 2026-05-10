package handlercontract_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// provisioningRetry — per-bead helper prefix for test helpers in this file.
// (implementer-protocol.md §Helper-prefix discipline; bead hk-8i31.57)

// ─────────────────────────────────────────────────────────────────────────────
// Test fixtures
// ─────────────────────────────────────────────────────────────────────────────

// provisioningRetryFixtureSkill is a minimal resolved skill for test use.
func provisioningRetryFixtureSkill(name string) handlercontract.ResolvedSkill {
	return handlercontract.ResolvedSkill{
		Name:       name,
		SourcePath: "/tmp/skills/" + name,
	}
}

// provisioningRetryFixtureFastCfg returns a fast backoff config for tests:
// base 1ms, cap 4ms, max 4 attempts.  Wall-clock delays are negligible.
func provisioningRetryFixtureFastCfg() handlercontract.ProvisioningBackoffConfig {
	return handlercontract.ProvisioningBackoffConfig{
		Base:        1 * time.Millisecond,
		Cap:         4 * time.Millisecond,
		MaxAttempts: 4,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestHC048a_DefaultBackoffConfig verifies the default backoff parameters per
// HC-048a: base 1s, cap 16s, max 4 attempts.
//
// Spec ref: handler-contract.md §4.11.HC-048a.
func TestHC048a_DefaultBackoffConfig(t *testing.T) {
	t.Parallel()

	cfg := handlercontract.DefaultProvisioningBackoffConfig
	if cfg.Base != 1*time.Second {
		t.Errorf("HC-048a: DefaultProvisioningBackoffConfig.Base = %v, want 1s", cfg.Base)
	}
	if cfg.Cap != 16*time.Second {
		t.Errorf("HC-048a: DefaultProvisioningBackoffConfig.Cap = %v, want 16s", cfg.Cap)
	}
	if cfg.MaxAttempts != 4 {
		t.Errorf("HC-048a: DefaultProvisioningBackoffConfig.MaxAttempts = %d, want 4", cfg.MaxAttempts)
	}
}

// TestHC048a_RetryProvisionWithBackoff_SuccessOnFirstAttempt verifies that
// when provisioning succeeds on the first attempt, RetryProvisionWithBackoff
// returns nil and calls the function exactly once.
//
// Spec ref: handler-contract.md §4.11.HC-048a.
func TestHC048a_RetryProvisionWithBackoff_SuccessOnFirstAttempt(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	provision := func(_ context.Context, _ handlercontract.ResolvedSkill) error {
		calls.Add(1)
		return nil
	}

	skill := provisioningRetryFixtureSkill("beads-cli")
	err := handlercontract.RetryProvisionWithBackoff(
		context.Background(),
		skill,
		provision,
		provisioningRetryFixtureFastCfg(),
		5*time.Second,
	)
	if err != nil {
		t.Errorf("HC-048a: success on first attempt: got err %v, want nil", err)
	}
	if calls.Load() != 1 {
		t.Errorf("HC-048a: success on first attempt: provision called %d times, want 1", calls.Load())
	}
}

// TestHC048a_RetryProvisionWithBackoff_SuccessAfterTransientRetries verifies
// that transient failures are retried and succeed when provisioning eventually
// succeeds.
//
// Spec ref: handler-contract.md §4.11.HC-048a — "transient conditions wrap
// ErrTransient; MUST be retried with exponential backoff."
func TestHC048a_RetryProvisionWithBackoff_SuccessAfterTransientRetries(t *testing.T) {
	t.Parallel()

	const failCount = 2
	var calls atomic.Int32
	provision := func(_ context.Context, _ handlercontract.ResolvedSkill) error {
		n := calls.Add(1)
		if n <= failCount {
			return fmt.Errorf("transient: %w", handlercontract.ErrTransient)
		}
		return nil
	}

	skill := provisioningRetryFixtureSkill("my-skill")
	err := handlercontract.RetryProvisionWithBackoff(
		context.Background(),
		skill,
		provision,
		provisioningRetryFixtureFastCfg(),
		5*time.Second,
	)
	if err != nil {
		t.Errorf("HC-048a: success after %d retries: got err %v, want nil", failCount, err)
	}
	if calls.Load() != failCount+1 {
		t.Errorf("HC-048a: calls = %d, want %d", calls.Load(), failCount+1)
	}
}

// TestHC048a_RetryProvisionWithBackoff_AttemptCapExhaustionReturnsErrStructural
// verifies that on attempt-cap exhaustion, the error wraps ErrSkillProvisioningFailed
// (which wraps ErrStructural) per HC-048a §8.2 reclassification.
//
// Spec ref: handler-contract.md §4.11.HC-048a — "on timeout or attempt-cap
// exhaustion, reclassify to ErrStructural per §8.2 and fail-launch."
func TestHC048a_RetryProvisionWithBackoff_AttemptCapExhaustionReturnsErrStructural(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	provision := func(_ context.Context, _ handlercontract.ResolvedSkill) error {
		calls.Add(1)
		return fmt.Errorf("always transient: %w", handlercontract.ErrTransient)
	}

	cfg := handlercontract.ProvisioningBackoffConfig{
		Base:        1 * time.Millisecond,
		Cap:         4 * time.Millisecond,
		MaxAttempts: 3,
	}
	skill := provisioningRetryFixtureSkill("always-fails")
	err := handlercontract.RetryProvisionWithBackoff(
		context.Background(),
		skill,
		provision,
		cfg,
		30*time.Second,
	)
	if err == nil {
		t.Fatal("HC-048a: attempt cap exhaustion: expected non-nil error, got nil")
	}
	if !errors.Is(err, handlercontract.ErrSkillProvisioningFailed) {
		t.Errorf("HC-048a: attempt cap exhaustion: got %v, want wrapping ErrSkillProvisioningFailed", err)
	}
	if !errors.Is(err, handlercontract.ErrStructural) {
		t.Errorf("HC-048a: attempt cap exhaustion: got %v, want wrapping ErrStructural", err)
	}
	if calls.Load() != 3 {
		t.Errorf("HC-048a: attempt cap exhaustion: calls = %d, want 3", calls.Load())
	}
}

// TestHC048a_RetryProvisionWithBackoff_StructuralFailureNoRetry verifies that
// a structural error (wrapping ErrSkillProvisioningFailed) is not retried.
//
// Spec ref: handler-contract.md §4.11.HC-048a — "structural conditions wrap
// ErrSkillProvisioningFailed; no retry."
func TestHC048a_RetryProvisionWithBackoff_StructuralFailureNoRetry(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	provision := func(_ context.Context, _ handlercontract.ResolvedSkill) error {
		calls.Add(1)
		return fmt.Errorf("integrity check failed: %w", handlercontract.ErrSkillProvisioningFailed)
	}

	skill := provisioningRetryFixtureSkill("corrupt-skill")
	err := handlercontract.RetryProvisionWithBackoff(
		context.Background(),
		skill,
		provision,
		provisioningRetryFixtureFastCfg(),
		5*time.Second,
	)
	if err == nil {
		t.Fatal("HC-048a: structural failure: expected non-nil error, got nil")
	}
	if !errors.Is(err, handlercontract.ErrSkillProvisioningFailed) {
		t.Errorf("HC-048a: structural failure: got %v, want wrapping ErrSkillProvisioningFailed", err)
	}
	if calls.Load() != 1 {
		t.Errorf("HC-048a: structural failure must not be retried; calls = %d, want 1", calls.Load())
	}
}

// TestHC048a_RetryProvisionWithBackoff_CtxCancelReturnsErrCanceled verifies
// that context cancellation during a backoff wait returns ErrCanceled.
//
// Spec ref: handler-contract.md §4.11.HC-048a.
func TestHC048a_RetryProvisionWithBackoff_CtxCancelReturnsErrCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	var calls atomic.Int32
	provision := func(_ context.Context, _ handlercontract.ResolvedSkill) error {
		calls.Add(1)
		// Cancel context after first attempt so the backoff wait is cancelled.
		cancel()
		return fmt.Errorf("transient: %w", handlercontract.ErrTransient)
	}

	skill := provisioningRetryFixtureSkill("cancellable")
	// Use a large backoff to ensure the timer is running when cancel fires.
	cfg := handlercontract.ProvisioningBackoffConfig{
		Base:        5 * time.Second,
		Cap:         5 * time.Second,
		MaxAttempts: 4,
	}
	err := handlercontract.RetryProvisionWithBackoff(
		ctx,
		skill,
		provision,
		cfg,
		30*time.Second,
	)
	if err == nil {
		t.Fatal("HC-048a: ctx cancel: expected non-nil error, got nil")
	}
	if !errors.Is(err, handlercontract.ErrCanceled) {
		t.Errorf("HC-048a: ctx cancel: got %v, want wrapping ErrCanceled", err)
	}
}

// TestHC048a_RetryProvisionWithBackoff_ProvisioningTimeoutReturnsErrStructural
// verifies that provisioning_timeout expiry reclassifies to ErrSkillProvisioningFailed.
//
// Spec ref: handler-contract.md §4.11.HC-048a — "bounded by
// LaunchSpec.provisioning_timeout; on timeout: reclassify to ErrStructural."
func TestHC048a_RetryProvisionWithBackoff_ProvisioningTimeoutReturnsErrStructural(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	provision := func(_ context.Context, _ handlercontract.ResolvedSkill) error {
		calls.Add(1)
		return fmt.Errorf("transient: %w", handlercontract.ErrTransient)
	}

	// Very short provisioning_timeout; first backoff (1ms) will exceed it.
	cfg := handlercontract.ProvisioningBackoffConfig{
		Base:        50 * time.Millisecond,
		Cap:         50 * time.Millisecond,
		MaxAttempts: 4,
	}
	skill := provisioningRetryFixtureSkill("slow-skill")
	err := handlercontract.RetryProvisionWithBackoff(
		context.Background(),
		skill,
		provision,
		cfg,
		1*time.Millisecond, // tiny timeout
	)
	if err == nil {
		t.Fatal("HC-048a: provisioning_timeout: expected non-nil error, got nil")
	}
	if !errors.Is(err, handlercontract.ErrSkillProvisioningFailed) {
		t.Errorf("HC-048a: provisioning_timeout: got %v, want wrapping ErrSkillProvisioningFailed", err)
	}
}

// TestHC048a_RetryProvisionWithBackoff_MaxAttemptsIs4 verifies that the spec's
// max-attempts value of 4 is honoured when all attempts fail transiently.
//
// Spec ref: handler-contract.md §4.11.HC-048a — "max 4 attempts."
func TestHC048a_RetryProvisionWithBackoff_MaxAttemptsIs4(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	provision := func(_ context.Context, _ handlercontract.ResolvedSkill) error {
		calls.Add(1)
		return fmt.Errorf("transient: %w", handlercontract.ErrTransient)
	}

	skill := provisioningRetryFixtureSkill("always-transient")
	_ = handlercontract.RetryProvisionWithBackoff(
		context.Background(),
		skill,
		provision,
		provisioningRetryFixtureFastCfg(), // MaxAttempts=4
		30*time.Second,
	)
	if calls.Load() != 4 {
		t.Errorf("HC-048a: MaxAttempts=4: calls = %d, want 4", calls.Load())
	}
}
