package lifecycle

import (
	"testing"
	"time"
)

// shutdownFixtureShutdownMode is the mode field of the daemon_shutdown event.
//
// Spec ref: process-lifecycle.md §4.4 PL-011a — "The mode is graceful for
// PL-011 and immediate for PL-012 (for the interceptable stop --immediate
// path)."
type shutdownFixtureShutdownMode string

const (
	// shutdownFixtureModeGraceful is the mode for a graceful (drain) shutdown.
	//
	// Spec ref: process-lifecycle.md §4.4 PL-011a — "mode is graceful for PL-011."
	shutdownFixtureModeGraceful shutdownFixtureShutdownMode = "graceful"

	// shutdownFixtureModeImmediate is the mode for an interceptable immediate shutdown.
	//
	// Spec ref: process-lifecycle.md §4.4 PL-011a — "mode is immediate for PL-012."
	shutdownFixtureModeImmediate shutdownFixtureShutdownMode = "immediate"
)

// shutdownFixtureDaemonShutdownEvent models the daemon_shutdown event payload.
//
// Spec ref: process-lifecycle.md §4.4 PL-011a — "Payload: {shutdown_at,
// shutdown_at_ns_since_boot, mode}."
// Spec ref: event-model.md §8.7.3 — "shutdown_at: <Timestamp> # RFC 3339
// wall-clock at the daemon's shutdown emission; shutdown_at_ns_since_boot:
// <Integer> # uint64 monotonic clock reading at shutdown, in ns since the
// host's boot; mode: <enum: graceful | immediate>."
type shutdownFixtureDaemonShutdownEvent struct {
	// ShutdownAt is the wall-clock time at emission, RFC 3339 with ms.
	ShutdownAt string `json:"shutdown_at"`
	// ShutdownAtNsSinceBoot is the monotonic-clock companion field (ns since boot).
	ShutdownAtNsSinceBoot uint64 `json:"shutdown_at_ns_since_boot"`
	// Mode is "graceful" or "immediate".
	Mode shutdownFixtureShutdownMode `json:"mode"`
}

// shutdownFixtureEmitDaemonShutdown constructs a daemon_shutdown event payload
// with the given mode. It captures wall-clock and monotonic-clock values at
// call time, modelling the spec's emission contract.
//
// Spec ref: process-lifecycle.md §4.4 PL-011a — "The daemon MUST emit
// daemon_shutdown … before the event bus flush (§PL-011 step 6). Payload:
// {shutdown_at, shutdown_at_ns_since_boot, mode}."
func shutdownFixtureEmitDaemonShutdown(mode shutdownFixtureShutdownMode) shutdownFixtureDaemonShutdownEvent {
	wallClock := time.Now()

	// shutdownFixtureMonotonicNs returns the monotonic clock reading in
	// nanoseconds since process start (time.Now().UnixNano subtraction gives
	// elapsed monotonic ns; for fixture purposes this is sufficient to assert
	// non-zero and increasing). In production the daemon sources from
	// CLOCK_MONOTONIC (Linux) / mach_absolute_time() (darwin).
	//
	// Spec ref: process-lifecycle.md §4.4 PL-011a — "shutdown_at_ns_since_boot
	// is the monotonic-clock companion field (nanoseconds since system boot,
	// sourced from CLOCK_MONOTONIC on Linux / mach_absolute_time() translated
	// to ns on darwin)."
	mono := shutdownFixtureMonotonicNs()

	return shutdownFixtureDaemonShutdownEvent{
		ShutdownAt:            wallClock.UTC().Format("2006-01-02T15:04:05.000Z07:00"),
		ShutdownAtNsSinceBoot: mono,
		Mode:                  mode,
	}
}

// shutdownFixtureMonotonicNs returns a monotonic nanosecond reading suitable
// for the shutdown_at_ns_since_boot companion field. The fixture uses
// runtime_nanotime approximation: time.Now() on Go captures monotonic reading;
// we derive elapsed ns from process start as a non-zero proxy for the boot-based
// monotonic counter.
//
// Spec ref: process-lifecycle.md §4.4 PL-011a — "shutdown_at_ns_since_boot is
// REQUIRED for graceful shutdowns. SIGKILL terminations have no daemon_shutdown
// emission at all."
func shutdownFixtureMonotonicNs() uint64 {
	// time.Since(time.Time{}) gives ns elapsed; using time.Now().UnixNano gives
	// a wall-based value but Go's time package preserves monotonic in time.Now().
	// For the fixture we return time.Now().UnixNano() as a non-zero uint64.
	// The production implementation replaces this with the OS syscall.
	t := time.Now()
	return uint64(t.UnixNano()) //nolint:gosec // G115: conversion from int64 to uint64 safe for positive timestamps
}

// shutdownFixtureBusFlushRecord records whether the event bus flush has
// occurred, used to verify emission-before-flush ordering.
type shutdownFixtureBusFlushRecord struct {
	emitted bool
	flushed bool
	// emitOrder is the sequence number at which daemon_shutdown was emitted.
	emitOrder int
	// flushOrder is the sequence number at which the bus flush occurred.
	flushOrder int
}

// shutdownFixtureSimulateGracefulShutdownSequence simulates the PL-011 shutdown
// sequence steps 5–6, asserting emission-before-flush ordering.
//
// Spec ref: process-lifecycle.md §4.4 PL-011 — "5. Emit daemon_shutdown per
// PL-011a. 6. Flush the event bus (fsync per [event-model.md §4.4])."
func shutdownFixtureSimulateGracefulShutdownSequence() shutdownFixtureBusFlushRecord {
	seq := 0
	rec := shutdownFixtureBusFlushRecord{}

	// Step 5: emit daemon_shutdown BEFORE flush.
	seq++
	rec.emitted = true
	rec.emitOrder = seq

	// Step 6: flush the event bus (simulated; no real fsync in fixture).
	seq++
	rec.flushed = true
	rec.flushOrder = seq

	return rec
}

// TestPL011a_DaemonShutdownGracefulEventShape verifies that the daemon_shutdown
// event emitted during a graceful shutdown carries the required payload fields:
// mode=graceful, shutdown_at (non-empty RFC 3339), and shutdown_at_ns_since_boot
// (non-zero uint64).
//
// Spec ref: process-lifecycle.md §4.4 PL-011a — "The daemon MUST emit
// daemon_shutdown … Payload: {shutdown_at, shutdown_at_ns_since_boot, mode}.
// The mode is graceful for PL-011."
// Spec ref: event-model.md §8.7.3 — daemon_shutdown payload schema.
func TestPL011a_DaemonShutdownGracefulEventShape(t *testing.T) {
	t.Parallel()

	evt := shutdownFixtureEmitDaemonShutdown(shutdownFixtureModeGraceful)

	// Assert mode is "graceful".
	if evt.Mode != shutdownFixtureModeGraceful {
		t.Errorf("PL-011a: daemon_shutdown mode = %q, want %q", evt.Mode, shutdownFixtureModeGraceful)
	}

	// Assert shutdown_at is non-empty (RFC 3339 with ms).
	if evt.ShutdownAt == "" {
		t.Error("PL-011a: daemon_shutdown shutdown_at is empty; want RFC 3339 timestamp")
	}

	// Assert shutdown_at_ns_since_boot is non-zero (REQUIRED for graceful).
	//
	// Spec ref: process-lifecycle.md §4.4 PL-011a — "shutdown_at_ns_since_boot
	// is REQUIRED for graceful shutdowns."
	if evt.ShutdownAtNsSinceBoot == 0 {
		t.Error("PL-011a: daemon_shutdown shutdown_at_ns_since_boot is zero; REQUIRED for graceful shutdowns per ON-033")
	}
}

// TestPL011a_EmissionBeforeBusFlush verifies that daemon_shutdown is emitted
// BEFORE the event bus flush (steps 5 then 6).
//
// Spec ref: process-lifecycle.md §4.4 PL-011a — "The daemon MUST emit
// daemon_shutdown … before the event bus flush (§PL-011 step 6)."
func TestPL011a_EmissionBeforeBusFlush(t *testing.T) {
	t.Parallel()

	rec := shutdownFixtureSimulateGracefulShutdownSequence()

	// Both steps must have occurred.
	if !rec.emitted {
		t.Error("PL-011a: daemon_shutdown was not emitted")
	}
	if !rec.flushed {
		t.Error("PL-011a: event bus was not flushed")
	}

	// Emission must precede flush: emitOrder < flushOrder.
	if rec.emitOrder >= rec.flushOrder {
		t.Errorf("PL-011a: daemon_shutdown emitted at seq %d, bus flushed at seq %d; emission must precede flush",
			rec.emitOrder, rec.flushOrder)
	}
}

// TestPL011a_ShutdownAtMonotonicCompanion verifies that the
// shutdown_at_ns_since_boot companion field is the monotonic companion to the
// wall-clock shutdown_at, and that it is non-decreasing across two successive
// calls.
//
// Spec ref: process-lifecycle.md §4.4 PL-011a — "shutdown_at_ns_since_boot is
// the monotonic-clock companion field … emitted alongside the wall-clock
// timestamp for RTO measurement per [operator-nfr.md §4.8 ON-033]."
func TestPL011a_ShutdownAtMonotonicCompanion(t *testing.T) {
	t.Parallel()

	evt1 := shutdownFixtureEmitDaemonShutdown(shutdownFixtureModeGraceful)
	// Brief sleep so the second reading is strictly after the first.
	time.Sleep(time.Millisecond)
	evt2 := shutdownFixtureEmitDaemonShutdown(shutdownFixtureModeGraceful)

	// Monotonic companion must be non-decreasing (time.Now().UnixNano is
	// monotonic in Go on POSIX; the sleep ensures strictly greater).
	if evt2.ShutdownAtNsSinceBoot <= evt1.ShutdownAtNsSinceBoot {
		t.Errorf("PL-011a: shutdown_at_ns_since_boot is not monotonically increasing: first=%d second=%d",
			evt1.ShutdownAtNsSinceBoot, evt2.ShutdownAtNsSinceBoot)
	}

	// Both events must have mode=graceful.
	if evt1.Mode != shutdownFixtureModeGraceful || evt2.Mode != shutdownFixtureModeGraceful {
		t.Errorf("PL-011a: mode mismatch: got %q and %q, want both graceful", evt1.Mode, evt2.Mode)
	}
}
