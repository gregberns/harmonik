// Package sentinel implements the flywheel movement governor (flywheel-motion §§1, 6.1).
//
// The governor is a deterministic, LLM-free subsystem that measures real terminal
// progress over a sliding window, applies a discrete inverse-staircase activation
// function, and emits a GovernorSignal indicating whether the system is dormant,
// watching, active (tripped), or halted (G-liveness doom-loop detected).
//
// Design constraints (spec §§1.2, 0.3):
//   - DISCRETE activation — a step/staircase function, NOT a smooth EWMA. An
//     operator MUST be able to reproduce the activation level by hand from the
//     events window alone.
//   - INVERSE — high movement → dormant; low movement → escalated scrutiny.
//   - LLM-FREE — no language model is consulted in this package.
//
// G-liveness self-kill (spec §6.1, bead hk-2do3): when N consecutive evaluation
// cycles observe zero terminal progress (no HEAD advance / bead close / run complete),
// the governor signals ActivationHalt. Callers MUST halt dispatch and page.
//
// Spec ref: .kerf/works/flywheel-motion/05-spec-drafts/flywheel-motion.md §§1, 6.1.
// Bead: hk-u0lv (V1 movement governor). Epic: hk-0oca (codename:flywheel).
package sentinel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// DefaultHighWeight is the movement score awarded for each terminal-progress event.
const DefaultHighWeight = 10

// DefaultSentinelEvalCadence is the default minimum interval between consecutive
// sentinel.Evaluate calls from the daemon workloop. Prevents computeWindowMovement
// from scanning events.jsonl on every 2s workloop tick (hk-usn8o).
const DefaultSentinelEvalCadence = 2 * time.Minute

// DefaultWeights is the default per-event-type weight table (spec §1.1).
// Terminal-progress events carry DefaultHighWeight; all other events carry 0
// (enforced by the zero-value default in ComputeWindowMovement).
//
// reviewer_verdict is handled specially: only APPROVE verdicts carry weight;
// REQUEST_CHANGES and BLOCK carry 0. This is applied inside ComputeWindowMovement
// by inspecting the payload.
var DefaultWeights = map[core.EventType]int{
	core.EventTypeBeadClosed:      DefaultHighWeight,
	core.EventTypeRunCompleted:    DefaultHighWeight,
	core.EventTypeReviewerVerdict: DefaultHighWeight, // gated by APPROVE payload check below
}

// ActivationLevel is the discrete staircase output (spec §1.2, §6.1).
//
// Levels are inverse to movement score:
//
//	ActivationDormant  — high movement; governor takes no action
//	ActivationWatching — moderate movement; governor observing
//	ActivationActive   — sustained low movement + actionable work; governor trips
//	ActivationHalt     — G-liveness doom-loop: N zero-progress cycles; MUST halt + page
type ActivationLevel int

const (
	// ActivationDormant means sufficient terminal progress was observed in the window.
	ActivationDormant ActivationLevel = 0
	// ActivationWatching means low movement but not yet sustained enough to trip.
	ActivationWatching ActivationLevel = 1
	// ActivationActive means sustained low movement with actionable work: governor trips.
	// At this level the caller SHOULD emit a digest exception (spec §3.5).
	ActivationActive ActivationLevel = 2
	// ActivationHalt means the G-liveness self-kill gate has fired (spec §6.1):
	// LivenessNoProgressN consecutive evaluation cycles observed zero terminal
	// progress. Callers MUST halt dispatch and emit a liveness_halt page event.
	ActivationHalt ActivationLevel = 3
)

func (a ActivationLevel) String() string {
	switch a {
	case ActivationDormant:
		return "dormant"
	case ActivationWatching:
		return "watching"
	case ActivationActive:
		return "active"
	case ActivationHalt:
		return "halt"
	default:
		return fmt.Sprintf("unknown(%d)", int(a))
	}
}

// Config holds the tunable parameters for the movement governor (spec §7).
// Zero values use the defaults documented on each field.
type Config struct {
	// Window is the sliding-window duration for movement computation.
	// Default: 30 minutes (spec §1.2).
	Window time.Duration

	// WarmupWindow is the minimum elapsed time since DaemonStartedAt before
	// the governor may trip (spec §1.4 cold-start gate).
	// Default: 30 minutes.
	WarmupWindow time.Duration

	// SustainedWindows is the number of consecutive "low" windows required
	// before the governor can trip (spec §1.4 sustained-low gate).
	// Default: 2.
	SustainedWindows int

	// HighThreshold: movement scores at or above this value are "high" → dormant.
	// Default: DefaultHighWeight (10) — at least one terminal-progress event.
	HighThreshold int

	// LowThreshold: movement scores strictly below this value are "low".
	// Default: DefaultHighWeight (10) — any score < 10 counts as low.
	LowThreshold int

	// Weights overrides the per-event-type weight table. Nil uses DefaultWeights.
	Weights map[core.EventType]int

	// LivenessNoProgressN is the number of consecutive zero-progress evaluation
	// cycles before the G-liveness self-kill gate fires (spec §6.1, bead hk-2do3).
	// A cycle is zero-progress when MovementScore == 0 (no HEAD advance, no
	// bead_closed, no run_completed, no reviewer_verdict{APPROVE}).
	// 0 (default) disables the G-liveness gate entirely.
	LivenessNoProgressN int

	// EvalCadence is the minimum interval between consecutive Evaluate calls.
	// Zero uses DefaultSentinelEvalCadence (2 minutes). The daemon workloop reads
	// this field to cadence-gate the O(events.jsonl) scan (hk-usn8o).
	EvalCadence time.Duration
}

func (c Config) window() time.Duration {
	if c.Window <= 0 {
		return 30 * time.Minute
	}
	return c.Window
}

func (c Config) warmupWindow() time.Duration {
	if c.WarmupWindow <= 0 {
		return 30 * time.Minute
	}
	return c.WarmupWindow
}

func (c Config) sustainedWindows() int {
	if c.SustainedWindows <= 0 {
		return 2
	}
	return c.SustainedWindows
}

func (c Config) highThreshold() int {
	if c.HighThreshold <= 0 {
		return DefaultHighWeight
	}
	return c.HighThreshold
}

func (c Config) lowThreshold() int {
	if c.LowThreshold <= 0 {
		return DefaultHighWeight
	}
	return c.LowThreshold
}

func (c Config) weights() map[core.EventType]int {
	if c.Weights != nil {
		return c.Weights
	}
	return DefaultWeights
}

func (c Config) livenessNoProgressN() int {
	return c.LivenessNoProgressN // 0 = disabled; no default
}

// GovernorState is the mutable state that must persist across governor evaluations.
//
// Callers own the state lifetime; Evaluate reads and writes it. For a stateless
// single-shot call, pass a pointer to a zero-value GovernorState — the cold-start
// gate will suppress the first activation correctly.
type GovernorState struct {
	// ConsecutiveLowWindows is the count of back-to-back windows whose movement
	// score was below the low threshold. Reset to 0 when a high window is observed.
	// The sustained-low gate requires this to reach SustainedWindows before tripping.
	ConsecutiveLowWindows int

	// ConsecutiveZeroCycles is the count of back-to-back evaluation cycles where
	// MovementScore was exactly zero (no HEAD advance, no bead_closed, no
	// run_completed, no reviewer_verdict{APPROVE}). Reset to 0 when any
	// terminal-progress event appears. Used by the G-liveness self-kill gate
	// (spec §6.1, bead hk-2do3).
	ConsecutiveZeroCycles int

	// DaemonStartedAt is the wall-clock time the calling daemon started.
	// Used by the cold-start warmup gate (spec §1.4). Zero suppresses the gate
	// (no warmup required), which is suitable for tests.
	DaemonStartedAt time.Time
}

// GovernorInput collects the external observations needed by one Evaluate call.
type GovernorInput struct {
	// ProjectDir is the root of the harmonik project (parent of .harmonik/).
	ProjectDir string

	// Now is the current wall-clock time used as the right edge of the sliding window.
	// Zero is NOT valid; callers must supply a real time.
	Now time.Time

	// HasReadyBeads is true when ≥1 unblocked open bead exists (opportunity gate §1.3).
	HasReadyBeads bool

	// HasUndeployedTail is true when merged-but-undeployed work exists (§1.3, §5.2).
	HasUndeployedTail bool

	// GitPath is the path to the git binary; defaults to "git" on PATH.
	GitPath string
}

// WindowSample records the movement observed in one evaluation window.
// All fields are human-readable so an operator can reproduce the activation level by hand.
type WindowSample struct {
	// WindowStart is the left edge of the sliding window (Now - Window).
	WindowStart time.Time
	// WindowEnd is the right edge (Now).
	WindowEnd time.Time
	// MovementScore is the weighted sum of terminal-progress events in the window.
	MovementScore int
	// TerminalEventCount is the number of terminal-progress events from events.jsonl.
	TerminalEventCount int
	// HeadAdvanceCount is the number of commits on origin/main within the window.
	HeadAdvanceCount int
}

// GovernorSignal is the output of one Evaluate call.
type GovernorSignal struct {
	// Level is the activation level (DORMANT / WATCHING / ACTIVE / HALT).
	Level ActivationLevel
	// Sample holds the window observation that produced this signal.
	Sample WindowSample
	// SuppressedBy describes which gate suppressed activation (empty when not suppressed).
	SuppressedBy string
	// HasOpportunity is true when the opportunity gate (§1.3) is open.
	HasOpportunity bool
	// ConsecutiveLowWindows is the value of GovernorState.ConsecutiveLowWindows
	// AFTER this Evaluate call updates it (useful for audit).
	ConsecutiveLowWindows int
	// ConsecutiveZeroCycles is the value of GovernorState.ConsecutiveZeroCycles
	// AFTER this Evaluate call updates it. Used by the G-liveness gate (§6.1).
	ConsecutiveZeroCycles int
	// LivenessViolated is true when the G-liveness self-kill gate fires:
	// ConsecutiveZeroCycles >= Config.LivenessNoProgressN (and N > 0).
	// When true, Level is set to ActivationHalt. Callers MUST halt dispatch
	// and emit a liveness_halt page event.
	LivenessViolated bool
}

// reviewerVerdictPayload is a minimal unmarshal target for reviewer_verdict events.
type reviewerVerdictPayload struct {
	Verdict core.ReviewerVerdict `json:"verdict"`
}

// ComputeWindowMovement reads events.jsonl and git log to produce a WindowSample
// over the window [windowStart, windowEnd].
//
// Terminal-progress events (spec §1.1):
//   - bead_closed                             → +weight (from weights map)
//   - run_completed (always success)           → +weight
//   - reviewer_verdict{verdict=APPROVE}        → +weight (others carry 0)
//   - commit on origin/main within the window  → +weight (from git log)
//
// All other event types carry weight 0 (start/chatter; spec §1.1 "weight 0").
// A missing or unreadable events.jsonl is treated as zero movement (not an error).
//
// eventsPath is the full path to events.jsonl.
// gitPath is the git binary (empty → "git").
// projectDir is the repo root (for git -C).
func ComputeWindowMovement(
	ctx context.Context,
	eventsPath string,
	windowStart time.Time,
	windowEnd time.Time,
	weights map[core.EventType]int,
	gitPath string,
	projectDir string,
) WindowSample {
	return computeWindowMovement(ctx, eventsPath, windowStart, windowEnd, weights, gitPath, projectDir)
}

// computeWindowMovement is the internal implementation.
func computeWindowMovement(
	ctx context.Context,
	eventsPath string,
	windowStart time.Time,
	windowEnd time.Time,
	weights map[core.EventType]int,
	gitPath string,
	projectDir string,
) WindowSample {
	if gitPath == "" {
		gitPath = "git"
	}
	sample := WindowSample{
		WindowStart: windowStart,
		WindowEnd:   windowEnd,
	}

	// --- events.jsonl scan ---
	// Derive a cursor near windowStart so ScanAfter skips all events older than
	// the window, bounding the I/O to the trailing window instead of the full
	// file (hk-usn8o). The wall-clock guard below is kept as a safety net for
	// the rare case where UUIDv7 timestamp and wall-clock differ by <1ms.
	cursor := eventIDFloorForTime(windowStart)
	for ev := range eventbus.ScanAfter(eventsPath, cursor) {
		// Filter to events within the window by wall-clock time.
		// UUIDv7 ordering would be more correct but wall-clock is sufficient
		// for a ~30-minute window and avoids timestamp parsing from EventIDs.
		if ev.TimestampWall.Before(windowStart) {
			continue
		}
		if ev.TimestampWall.After(windowEnd) {
			break // events are ordered; no need to scan further
		}

		evType := core.EventType(ev.Type)
		switch evType {
		case core.EventTypeBeadClosed, core.EventTypeRunCompleted:
			w := weights[evType]
			sample.MovementScore += w
			sample.TerminalEventCount++

		case core.EventTypeReviewerVerdict:
			var p reviewerVerdictPayload
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				continue
			}
			if p.Verdict == core.ReviewerVerdictApprove {
				w := weights[evType]
				sample.MovementScore += w
				sample.TerminalEventCount++
			}
		}
	}

	// --- git: commits on origin/main within the window ---
	headAdvances := countHeadAdvances(ctx, gitPath, projectDir, windowStart, windowEnd)
	sample.HeadAdvanceCount = headAdvances
	if headAdvances > 0 {
		// Each HEAD advance is one terminal-progress unit at the high weight.
		// Use bead_closed weight as the canonical "high" weight for git advances.
		highWeight := weights[core.EventTypeBeadClosed]
		if highWeight == 0 {
			highWeight = DefaultHighWeight
		}
		sample.MovementScore += headAdvances * highWeight
	}

	return sample
}

// eventIDFloorForTime returns the lexicographically minimum UUIDv7 that could
// represent the given instant. Passed as the 'after' cursor to ScanAfter so
// the events.jsonl scan starts near the window rather than from offset zero,
// bounding the per-Evaluate I/O to trailing-window events (hk-usn8o).
//
// The floor embeds the ms-precision Unix timestamp in the 48 most-significant
// bits (RFC 9562 §5.7) and zeros all random/variant bits. Any real UUIDv7 at
// the same millisecond (with non-zero random bits) is strictly greater, so
// ScanAfter correctly yields events at or after t.
func eventIDFloorForTime(t time.Time) core.EventID {
	ms := t.UnixMilli()
	var b [16]byte
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	b[6] = 0x70 // version nibble = 7, rand_a high nibble = 0
	// bytes 7-15: all zero (minimum possible random/variant bits)
	return core.EventID(b)
}

// countHeadAdvances counts commits on origin/main whose committer date falls
// within [windowStart, windowEnd]. Returns 0 on any git error (non-fatal).
func countHeadAdvances(ctx context.Context, gitPath, projectDir string, windowStart, windowEnd time.Time) int {
	if projectDir == "" {
		return 0
	}
	// --after and --before use committer date by default.
	// RFC3339 format is accepted by git log date filters.
	after := windowStart.UTC().Format(time.RFC3339)
	before := windowEnd.UTC().Format(time.RFC3339)
	args := []string{
		"-C", projectDir,
		"log", "origin/main",
		"--oneline",
		"--after=" + after,
		"--before=" + before,
	}
	//nolint:gosec // G204: all args are daemon-controlled, not user input.
	cmd := exec.CommandContext(ctx, gitPath, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return 0
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

// Evaluate runs one governor evaluation cycle.
//
// It reads events.jsonl and git log to compute the movement score for the current
// window, applies the discrete inverse staircase, checks all gates (opportunity,
// cold-start, sustained), and returns a GovernorSignal.
//
// state is updated in-place: ConsecutiveLowWindows and ConsecutiveZeroCycles are
// incremented or reset based on the window observation. Callers must persist state
// between calls to implement both the sustained-low gate and the G-liveness gate.
//
// Spec ref: flywheel-motion.md §§1.2, 1.3, 1.4, 6.1.
func Evaluate(
	ctx context.Context,
	state *GovernorState,
	input GovernorInput,
	cfg Config,
) GovernorSignal {
	win := cfg.window()
	windowStart := input.Now.Add(-win)
	windowEnd := input.Now

	eventsPath := filepath.Join(input.ProjectDir, ".harmonik", "events", "events.jsonl")
	weights := cfg.weights()

	sample := computeWindowMovement(
		ctx,
		eventsPath,
		windowStart,
		windowEnd,
		weights,
		input.GitPath,
		input.ProjectDir,
	)

	sig := GovernorSignal{
		Sample:         sample,
		HasOpportunity: input.HasReadyBeads || input.HasUndeployedTail,
	}

	// --- G-liveness self-kill gate (spec §6.1, bead hk-2do3) ---
	// Track consecutive evaluation cycles with zero terminal progress independently
	// of the inverse-staircase gate. Any terminal-progress event (score > 0)
	// resets the counter; score == 0 increments it.
	// This tracking runs before the staircase so the counter is always current.
	if sample.MovementScore == 0 {
		state.ConsecutiveZeroCycles++
	} else {
		state.ConsecutiveZeroCycles = 0
	}
	sig.ConsecutiveZeroCycles = state.ConsecutiveZeroCycles

	n := cfg.livenessNoProgressN()
	if n > 0 && state.ConsecutiveZeroCycles >= n {
		// Respect the warmup gate: a just-restarted daemon has naturally-low
		// movement, so G-liveness does not fire during the startup grace window.
		inWarmup := !state.DaemonStartedAt.IsZero() &&
			input.Now.Sub(state.DaemonStartedAt) < cfg.warmupWindow()
		if !inWarmup {
			sig.LivenessViolated = true
			sig.Level = ActivationHalt
			return sig
		}
	}

	// --- Discrete inverse staircase (spec §1.2) ---
	// A movement score >= highThreshold means at least one terminal-progress event
	// in the window: the governor is dormant. Score < lowThreshold is "low".
	// The staircase is auditable by reading sample.MovementScore directly.
	isHighWindow := sample.MovementScore >= cfg.highThreshold()
	isLowWindow := sample.MovementScore < cfg.lowThreshold()

	if isHighWindow {
		state.ConsecutiveLowWindows = 0
		sig.Level = ActivationDormant
		sig.ConsecutiveLowWindows = state.ConsecutiveLowWindows
		return sig
	}

	if isLowWindow {
		state.ConsecutiveLowWindows++
	} else {
		// Moderate — in between thresholds. Count as low for the sustained gate.
		state.ConsecutiveLowWindows++
	}
	sig.ConsecutiveLowWindows = state.ConsecutiveLowWindows

	// Default to WATCHING; gates below can promote to ACTIVE.
	sig.Level = ActivationWatching

	// --- Opportunity gate (spec §1.3) ---
	// MUST NOT trip if there is no actionable work.
	if !sig.HasOpportunity {
		sig.SuppressedBy = "no_opportunity"
		return sig
	}

	// --- Cold-start warmup gate (spec §1.4) ---
	// Suppress until the warmup watermark has elapsed since daemon start.
	if !state.DaemonStartedAt.IsZero() {
		elapsed := input.Now.Sub(state.DaemonStartedAt)
		if elapsed < cfg.warmupWindow() {
			sig.SuppressedBy = fmt.Sprintf("warmup(elapsed=%s,required=%s)", elapsed.Round(time.Second), cfg.warmupWindow())
			return sig
		}
	}

	// --- Sustained-low gate (spec §1.4) ---
	// Require ≥ sustainedWindows consecutive low windows before tripping.
	if state.ConsecutiveLowWindows < cfg.sustainedWindows() {
		// Still watching; not yet sustained.
		return sig
	}

	// All gates passed: trip.
	sig.Level = ActivationActive
	return sig
}
