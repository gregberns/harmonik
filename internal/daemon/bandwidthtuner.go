package daemon

// bandwidthtuner.go — rolling-5h token-rate auto-tuner for --max-concurrent.
//
// The tuner reads ~/.claude/projects/*/*.jsonl (the transcript files that Claude
// Code writes per-message) every 60 s, sums the tokens consumed over the
// trailing 5 h window, and adjusts the runtime concurrency ceiling so that
// harmonik doesn't overshoot the operator's subscription bandwidth.
//
// Formula: effectiveMax = clamp(round(N_max * (ceiling − used) / ceiling), 1, N_max)
//
// where:
//   N_max   = the user-configured --max-concurrent value (the static ceiling)
//   ceiling = --subscription-token-ceiling (tokens per 5 h; operator-supplied)
//   used    = sum of (input + output + cache_creation) tokens across ALL
//             ~/.claude/projects over the trailing 5 h window
//             (cache_read is excluded: it may not count toward the subscription cap)
//
// Emergency backstop: NotifyRateLimit() snaps maxConcurrent to 1 and suppresses
// further upward adjustment until the retry-after window expires.
//
// Bead ref: hk-ymav1.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

const (
	bandwidthTunerWindow   = 5 * time.Hour
	bandwidthTunerInterval = 60 * time.Second
)

// bandwidthTunerBackstop bridges the pre-Seal bus subscription to the
// post-Seal BandwidthTuner construction.  Subscribe must be called before
// bus.Seal (EV-009); SetTuner wires the live tuner after it is constructed.
// This two-phase wiring avoids restructuring the daemon init order: the tuner
// depends on concurrencyCtrl, which is built after Seal.
type bandwidthTunerBackstop struct {
	tuner atomic.Pointer[BandwidthTuner]
}

// SetTuner stores the running tuner so the bus handler can forward events.
// Must be called after NewBandwidthTuner and before beads are dispatched.
func (b *bandwidthTunerBackstop) SetTuner(t *BandwidthTuner) {
	b.tuner.Store(t)
}

// Subscribe registers an asynchronous consumer for agent_rate_limit_status bus
// events.  When the tuner is set and a status=active event arrives (emitted by
// dispatchHookRelayEnvelope in hookrelay_chb025.go when agent_rate_limited
// arrives on the socket), it calls tuner.NotifyRateLimit with the parsed
// retry_after duration.
// Must be called before bus.Seal (EV-009).
func (b *bandwidthTunerBackstop) Subscribe(bus eventbus.EventBus) error {
	sub := core.Subscription{
		ConsumerID:    "bandwidth-tuner-rate-limit-backstop",
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern: core.EventPattern{
			Types: map[string]struct{}{
				string(core.EventTypeAgentRateLimitStatus): {},
			},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: b.handle,
	}
	if _, err := bus.Subscribe(sub); err != nil {
		return fmt.Errorf("bandwidthTunerBackstop.Subscribe: %w", err)
	}
	return nil
}

// handle is the bus event handler for agent_rate_limit_status events.
// Only status=active events trigger NotifyRateLimit; cleared events are ignored.
func (b *bandwidthTunerBackstop) handle(_ context.Context, evt core.Event) error {
	t := b.tuner.Load()
	if t == nil {
		return nil // tuner not running (--subscription-token-ceiling not set)
	}
	var pl core.AgentRateLimitStatusPayload
	if err := json.Unmarshal(evt.Payload, &pl); err != nil {
		return nil // malformed payload — skip
	}
	if pl.Status != core.AgentRateLimitStatusActive {
		return nil // only act on the active (rate-limited) transition
	}
	var d time.Duration
	if pl.RetryAfterSeconds != nil && *pl.RetryAfterSeconds > 0 {
		d = time.Duration(*pl.RetryAfterSeconds) * time.Second
	}
	t.NotifyRateLimit(d)
	return nil
}

// transcriptRecord is a minimal parse target for a single line in a
// ~/.claude/projects/*/*.jsonl transcript file.
type transcriptRecord struct {
	Timestamp string             `json:"timestamp"`
	Message   *transcriptMessage `json:"message"`
}

type transcriptMessage struct {
	Usage *transcriptUsage `json:"usage"`
}

type transcriptUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	// CacheReadInputTokens intentionally excluded per bead spec.
}

// BandwidthTuner adjusts the ConcurrencyController ceiling every 60 s based on
// the rolling 5 h token consumption read from Claude Code transcripts.
//
// Construct with NewBandwidthTuner and start with Run in a goroutine.
// NotifyRateLimit is the emergency backstop: it snaps the ceiling to 1 and
// suppresses upward adjustment until the retry-after window expires.
type BandwidthTuner struct {
	ctrl     *ConcurrencyController
	maxN     int
	ceiling  int64
	homeDir  string
	interval time.Duration
	window   time.Duration

	// rateLimitUntilNanos is the unix-nanosecond timestamp until which upward
	// adjustment is suppressed.  0 = no active backoff.  Written by
	// NotifyRateLimit; read by the tuner goroutine.
	rateLimitUntilNanos atomic.Int64
}

// NewBandwidthTuner creates a BandwidthTuner.  ctrl must be non-nil.
// maxN is the static --max-concurrent ceiling; ceiling is the per-5h token cap
// supplied via --subscription-token-ceiling.
func NewBandwidthTuner(ctrl *ConcurrencyController, maxN int, ceiling int64, homeDir string) *BandwidthTuner {
	return &BandwidthTuner{
		ctrl:     ctrl,
		maxN:     maxN,
		ceiling:  ceiling,
		homeDir:  homeDir,
		interval: bandwidthTunerInterval,
		window:   bandwidthTunerWindow,
	}
}

// Run is the main tuner goroutine.  It blocks until ctx is cancelled.
// Call in a dedicated goroutine: go tuner.Run(ctx).
func (t *BandwidthTuner) Run(ctx context.Context) {
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	// Run one tick immediately on startup so the ceiling is set before the first
	// dispatch rather than waiting 60 s.
	t.tick()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.tick()
		}
	}
}

// NotifyRateLimit is the emergency backstop for a 429-class rate limit hit.
// It snaps the concurrency ceiling to 1 immediately and suppresses upward
// adjustment until retryAfter has elapsed.  Safe to call from any goroutine.
func (t *BandwidthTuner) NotifyRateLimit(retryAfter time.Duration) {
	if retryAfter <= 0 {
		retryAfter = 5 * time.Minute // conservative default when no hint supplied
	}
	until := time.Now().Add(retryAfter).UnixNano()
	t.rateLimitUntilNanos.Store(until)
	// Snap to 1 immediately regardless of the normal tuning formula.
	_, _ = t.ctrl.Set(1)
}

// tick is a single tuner evaluation.  It reads transcript usage, computes the
// adjusted ceiling, and calls ctrl.Set if the value changed.
func (t *BandwidthTuner) tick() {
	now := time.Now()

	// Respect rate-limit backoff: if we're still in the suppression window, do
	// not raise the ceiling (it was already snapped to 1 by NotifyRateLimit).
	if until := t.rateLimitUntilNanos.Load(); until > 0 && now.UnixNano() < until {
		return
	}
	// Clear the backoff once we're past it.
	t.rateLimitUntilNanos.Store(0)

	since := now.Add(-t.window)
	used, err := transcriptTokensUsed(t.homeDir, since)
	if err != nil || t.ceiling <= 0 {
		// If we can't read transcripts, leave ceiling unchanged.
		return
	}

	headroom := t.ceiling - used
	if headroom < 0 {
		headroom = 0
	}

	// effectiveMax = clamp(round(N_max * headroom / ceiling), 1, N_max)
	ratio := float64(headroom) / float64(t.ceiling)
	target := int(math.Round(float64(t.maxN) * ratio))
	if target < 1 {
		target = 1
	}
	if target > t.maxN {
		target = t.maxN
	}

	current := t.ctrl.Get()
	if current != target {
		_, _ = t.ctrl.Set(target)
	}
}

// transcriptTokensUsed walks ~/.claude/projects/*/*.jsonl and sums
// input_tokens + output_tokens + cache_creation_input_tokens for all
// assistant messages with a timestamp after `since`.
//
// Files not modified since `since` are skipped to avoid reading large
// historical transcripts.
func transcriptTokensUsed(homeDir string, since time.Time) (int64, error) {
	projectsDir := filepath.Join(homeDir, ".claude", "projects")

	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	var total int64
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		projDir := filepath.Join(projectsDir, entry.Name())
		sum, scanErr := scanProjectDir(projDir, since)
		if scanErr != nil {
			// Non-fatal: one corrupt project dir should not block the tuner.
			continue
		}
		total += sum
	}
	return total, nil
}

// scanProjectDir scans a single ~/.claude/projects/<name>/ directory.
func scanProjectDir(dir string, since time.Time) (int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}

	var total int64
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		// Skip files not modified within the window; mtime is a cheap gate.
		info, infoErr := entry.Info()
		if infoErr != nil || !info.ModTime().After(since) {
			continue
		}
		sum, scanErr := scanJSONLFile(filepath.Join(dir, entry.Name()), since)
		if scanErr != nil {
			continue
		}
		total += sum
	}
	return total, nil
}

// scanJSONLFile scans a single JSONL transcript file and sums tokens for
// records within the time window.
func scanJSONLFile(path string, since time.Time) (int64, error) {
	f, err := os.Open(path) //nolint:gosec // path is constructed from os.ReadDir, not user input
	if err != nil {
		return 0, err
	}
	defer f.Close() //nolint:errcheck

	var total int64
	scanner := bufio.NewScanner(f)
	// Increase the scanner buffer for long lines (large model outputs).
	scanner.Buffer(make([]byte, 256*1024), 2*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var rec transcriptRecord
		if jsonErr := json.Unmarshal(line, &rec); jsonErr != nil {
			continue
		}
		if rec.Timestamp == "" || rec.Message == nil || rec.Message.Usage == nil {
			continue
		}

		ts, tsErr := time.Parse(time.RFC3339Nano, rec.Timestamp)
		if tsErr != nil {
			// Try without sub-second precision (some entries use millisecond suffix)
			ts, tsErr = time.Parse("2006-01-02T15:04:05.000Z", rec.Timestamp)
			if tsErr != nil {
				continue
			}
		}
		if !ts.After(since) {
			continue
		}

		u := rec.Message.Usage
		total += u.InputTokens + u.OutputTokens + u.CacheCreationInputTokens
	}
	return total, scanner.Err()
}
