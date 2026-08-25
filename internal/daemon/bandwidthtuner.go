package daemon

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
	"github.com/gregberns/harmonik/internal/runregistry"
)

const (
	bandwidthTunerWindow   = 5 * time.Hour
	bandwidthTunerInterval = 60 * time.Second
)

type bandwidthTunerBackstop struct {
	tuner atomic.Pointer[BandwidthTuner]
	reg   atomic.Pointer[runregistry.RunRegistry]
}

// SetTuner stores the running tuner so the bus handler can forward events.
// Must be called after NewBandwidthTuner and before beads are dispatched.
func (b *bandwidthTunerBackstop) SetTuner(t *BandwidthTuner) {
	b.tuner.Store(t)
}

// SetRunRegistry wires the run registry so that handle() can look up the
// agent type for incoming rate-limit events. PI-073: Pi runs must be
// isolated from the global tuner. Called from daemon init before beads
// are dispatched. Optional — if nil, no filtering is applied (safe default:
// all events reach the tuner, Pi is not yet in production).
func (b *bandwidthTunerBackstop) SetRunRegistry(r *runregistry.RunRegistry) {
	b.reg.Store(r)
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
			Types: map[core.EventType]struct{}{
				core.EventTypeAgentRateLimitStatus: {},
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
	if r := b.reg.Load(); r != nil && pl.RunID != (core.RunID{}) {
		if h, ok := r.Get(pl.RunID); ok && h != nil {
			if h.GetAgentType() == core.AgentTypePi {
				return nil // Pi: per-queue backoff only (PI-073)
			}
		}
	}
	var d time.Duration
	if pl.RetryAfterSeconds != nil && *pl.RetryAfterSeconds > 0 {
		d = time.Duration(*pl.RetryAfterSeconds) * time.Second
	}
	t.NotifyRateLimit(d)
	return nil
}

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
}

// BandwidthTuner adjusts the ConcurrencyController ceiling every 60 s based on
// the rolling 5 h token consumption read from Claude Code transcripts.
//
// Construct with NewBandwidthTuner and start with Run in a goroutine.
// NotifyRateLimit is the emergency backstop: it snaps the ceiling to 1 and
// suppresses upward adjustment until the retry-after window expires.
// SetGate wires the INACTIVE poll gate (SS-007, hk-w6q7); call before Run.
type BandwidthTuner struct {
	ctrl    *ConcurrencyController
	maxN    int
	ceiling int64
	// projectsDir is Claude Code's transcript store. The caller resolves it, so
	// the tuner never reads the operator's home directly and a test can point it
	// at a fixture. See internal/workspace.DefaultClaudeProjectsDir.
	projectsDir string
	interval    time.Duration
	window      time.Duration
	gate        *PollGate // nil = ungated; set via SetGate before Run

	// rateLimitUntilNanos is the unix-nanosecond timestamp until which upward
	// adjustment is suppressed.  0 = no active backoff.  Written by
	// NotifyRateLimit; read by the tuner goroutine.
	rateLimitUntilNanos atomic.Int64
}

// NewBandwidthTuner creates a BandwidthTuner.  ctrl must be non-nil.
// maxN is the static --max-concurrent ceiling; ceiling is the per-5h token cap
// supplied via --subscription-token-ceiling. projectsDir is Claude Code's
// transcript store, resolved by the caller.
func NewBandwidthTuner(ctrl *ConcurrencyController, maxN int, ceiling int64, projectsDir string) *BandwidthTuner {
	return &BandwidthTuner{
		ctrl:        ctrl,
		maxN:        maxN,
		ceiling:     ceiling,
		projectsDir: projectsDir,
		interval:    bandwidthTunerInterval,
		window:      bandwidthTunerWindow,
	}
}

// Run is the main tuner goroutine.  It blocks until ctx is cancelled.
// Call in a dedicated goroutine: go tuner.Run(ctx).
func (t *BandwidthTuner) Run(ctx context.Context) {
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

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

// SetGate wires the INACTIVE poll gate (SS-007).  Must be called before Run.
// When the gate reports INACTIVE, tick() returns early without adjusting the
// ceiling.  Nil means ungated.
func (t *BandwidthTuner) SetGate(g *PollGate) { t.gate = g }

// NotifyRateLimit is the emergency backstop for a 429-class rate limit hit.
// It snaps the concurrency ceiling to 1 immediately and suppresses upward
// adjustment until retryAfter has elapsed.  Safe to call from any goroutine.
func (t *BandwidthTuner) NotifyRateLimit(retryAfter time.Duration) {
	if retryAfter <= 0 {
		retryAfter = 5 * time.Minute // conservative default when no hint supplied
	}
	until := time.Now().Add(retryAfter).UnixNano()
	t.rateLimitUntilNanos.Store(until)
	if _, setErr := t.ctrl.Set(1); setErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: bandwidth tuner: snap concurrency ceiling to 1 after rate limit: %v\n", setErr)
	}
}

func (t *BandwidthTuner) tick() {
	if t.gate != nil && t.gate.IsInactive() {
		return
	}
	now := time.Now()

	if until := t.rateLimitUntilNanos.Load(); until > 0 && now.UnixNano() < until {
		return
	}
	t.rateLimitUntilNanos.Store(0)

	since := now.Add(-t.window)
	used, err := transcriptTokensUsed(t.projectsDir, since)
	if err != nil || t.ceiling <= 0 {
		return
	}

	headroom := t.ceiling - used
	if headroom < 0 {
		headroom = 0
	}

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
		if _, setErr := t.ctrl.Set(target); setErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: bandwidth tuner: set concurrency ceiling to %d: %v\n", target, setErr)
		}
	}
}

func transcriptTokensUsed(projectsDir string, since time.Time) (int64, error) {
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
			continue
		}
		total += sum
	}
	return total, nil
}

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

func scanJSONLFile(path string, since time.Time) (int64, error) {
	f, err := os.Open(path) //nolint:gosec // path is constructed from os.ReadDir, not user input
	if err != nil {
		return 0, err
	}
	defer f.Close() //nolint:errcheck

	var total int64
	scanner := bufio.NewScanner(f)
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
