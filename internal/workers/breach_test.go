package workers

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// base is a fixed wall-clock anchor; all sample times are offsets from it so the
// machine's dwell math is exact and deterministic.
var base = time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)

func at(seconds int) time.Time { return base.Add(time.Duration(seconds) * time.Second) }

// --- sample builders: each isolates ONE signal so a test exercises it alone. ---

// cpuRep builds a sample with the given load5/ncpu ratio (ncpu=8), all other
// signals well OK (plenty of free memory, no swap).
func cpuRep(ratio float64) WorkerReportPayload {
	const ncpu = 8
	return WorkerReportPayload{
		NCPU:       ncpu,
		Load5:      ratio * ncpu,
		MemTotalMB: 16000,
		MemFreeMB:  8000, // free frac 0.5 — memory OK
		SwapUsedMB: 0,    // swap OK
	}
}

// memRep builds a sample with the given free fraction, cpu/swap OK.
func memRep(freeFrac float64) WorkerReportPayload {
	const total = 16000
	return WorkerReportPayload{
		NCPU:       8,
		Load5:      0.1 * 8, // cpu ratio 0.1 — OK
		MemTotalMB: total,
		MemFreeMB:  int64(freeFrac * total),
		SwapUsedMB: 0,
	}
}

// swapRep builds a sample with the given swap MB, cpu/memory OK.
func swapRep(swapMB int64) WorkerReportPayload {
	return WorkerReportPayload{
		NCPU:       8,
		Load5:      0.1 * 8,
		MemTotalMB: 16000,
		MemFreeMB:  8000,
		SwapUsedMB: swapMB,
	}
}

// feed drives a sequence of (sample, timeSeconds) through the detector and
// returns every event produced, in order.
func feed(d *breachDetector, steps []struct {
	rep WorkerReportPayload
	t   int
},
) []ResourceBreachPayload {
	var all []ResourceBreachPayload
	for _, s := range steps {
		all = append(all, d.Observe(s.rep, at(s.t))...)
	}
	return all
}

// ---------------------------------------------------------------------------
// PB1 — JSON round-trip + registration.
// ---------------------------------------------------------------------------

func TestResourceBreachPayload_JSONRoundTrip(t *testing.T) {
	want := ResourceBreachPayload{
		WorkerName:         "gb-mbp",
		Kind:               "breach",
		Signal:             "cpu",
		Value:              0.91,
		Threshold:          0.85,
		BreachedForSeconds: 0,
		InFlight:           3,
		StartedAt:          "2026-06-20T12:00:00Z",
		FiredAt:            "2026-06-20T12:00:20Z",
	}
	b, err := marshalResourceBreach(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Field names are the JSON-tag contract PB3/operators rely on.
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	for _, k := range []string{"worker_name", "kind", "signal", "value", "threshold", "breached_for_seconds", "in_flight", "started_at", "fired_at"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("missing JSON key %q in %s", k, b)
		}
	}
	var got ResourceBreachPayload
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != want {
		t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestResourceBreach_EventTypeRegistered(t *testing.T) {
	if core.EventTypeResourceBreach != "resource_breach" {
		t.Fatalf("EventTypeResourceBreach = %q, want resource_breach", core.EventTypeResourceBreach)
	}
	ev := core.Event{Type: string(core.EventTypeResourceBreach)}
	// A registered type decodes; an unregistered one errors. Encode a payload and
	// confirm DecodePayload yields the right concrete type.
	p := ResourceBreachPayload{WorkerName: "w", Kind: "breach", Signal: "swap"}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	ev.Payload = b
	decoded, err := ev.DecodePayload()
	if err != nil {
		t.Fatalf("DecodePayload (is resource_breach registered?): %v", err)
	}
	if _, ok := decoded.(*ResourceBreachPayload); !ok {
		t.Fatalf("decoded payload type = %T, want *ResourceBreachPayload", decoded)
	}
}

// ---------------------------------------------------------------------------
// PB2 — the state machine.
// ---------------------------------------------------------------------------

// dwell consts mirror the defaults so the tests read clearly.
const (
	bdwell = 20 // DefaultBreachDwell seconds
	cdwell = 15 // DefaultClearDwell seconds
)

func TestObserve_SubDwellSpike_NoEvent(t *testing.T) {
	d := NewBreachDetector("w", BreachConfig{})
	// Over enter at t=0, back under enter at t=10 (< 20s breach dwell).
	evs := feed(d, []struct {
		rep WorkerReportPayload
		t   int
	}{
		{cpuRep(0.95), 0},
		{cpuRep(0.50), 10},
		{cpuRep(0.50), 30},
	})
	if len(evs) != 0 {
		t.Fatalf("sub-dwell spike must produce 0 events, got %d: %+v", len(evs), evs)
	}
}

func TestObserve_SustainedBreach_OneEvent(t *testing.T) {
	d := NewBreachDetector("w", BreachConfig{})
	evs := feed(d, []struct {
		rep WorkerReportPayload
		t   int
	}{
		{cpuRep(0.95), 0},      // ARMING starts
		{cpuRep(0.95), 10},     // still arming
		{cpuRep(0.95), bdwell}, // dwell matured → breach fires
	})
	if len(evs) != 1 {
		t.Fatalf("want exactly 1 breach, got %d: %+v", len(evs), evs)
	}
	ev := evs[0]
	if ev.Kind != "breach" || ev.Signal != "cpu" {
		t.Errorf("kind/signal = %q/%q, want breach/cpu", ev.Kind, ev.Signal)
	}
	if ev.Threshold != DefaultCPUEnter {
		t.Errorf("threshold = %v, want %v", ev.Threshold, DefaultCPUEnter)
	}
	if ev.Value < 0.94 || ev.Value > 0.96 {
		t.Errorf("value = %v, want ~0.95", ev.Value)
	}
	if ev.StartedAt != at(0).Format(time.RFC3339) {
		t.Errorf("StartedAt = %q, want %q", ev.StartedAt, at(0).Format(time.RFC3339))
	}
	if ev.FiredAt != at(bdwell).Format(time.RFC3339) {
		t.Errorf("FiredAt = %q, want %q", ev.FiredAt, at(bdwell).Format(time.RFC3339))
	}
	if ev.BreachedForSeconds != 0 {
		t.Errorf("breach event BreachedForSeconds = %d, want 0", ev.BreachedForSeconds)
	}
	if ev.InFlight != 0 {
		t.Errorf("InFlight = %d, want 0 (detector never sets it)", ev.InFlight)
	}
}

func TestObserve_HysteresisBand_NoReFireNoClear(t *testing.T) {
	d := NewBreachDetector("w", BreachConfig{})
	// Drive to BREACHED, then oscillate strictly inside (exit, enter) = (0.70, 0.85).
	evs := feed(d, []struct {
		rep WorkerReportPayload
		t   int
	}{
		{cpuRep(0.95), 0},
		{cpuRep(0.95), bdwell}, // breach fires here
		{cpuRep(0.80), 30},     // in band — value > exit so NOT under-exit
		{cpuRep(0.72), 40},     // in band
		{cpuRep(0.84), 50},     // in band
		{cpuRep(0.80), 80},     // in band, well past any dwell
	})
	if len(evs) != 1 {
		t.Fatalf("band oscillation must give exactly the 1 breach (no re-fire, no clear), got %d: %+v", len(evs), evs)
	}
	if evs[0].Kind != "breach" {
		t.Fatalf("the single event must be the breach, got %q", evs[0].Kind)
	}
}

func TestObserve_SustainedClear_OneEvent_WithEpisodeLength(t *testing.T) {
	d := NewBreachDetector("w", BreachConfig{})
	evs := feed(d, []struct {
		rep WorkerReportPayload
		t   int
	}{
		{cpuRep(0.95), 0},
		{cpuRep(0.95), bdwell},       // breach fires at t=20
		{cpuRep(0.50), 100},          // under exit (0.50 < 0.70) → CLEARING starts at 100
		{cpuRep(0.50), 100 + cdwell}, // clear dwell matured → clear fires at t=115
	})
	if len(evs) != 2 {
		t.Fatalf("want breach + clear, got %d: %+v", len(evs), evs)
	}
	clear := evs[1]
	if clear.Kind != "clear" || clear.Signal != "cpu" {
		t.Errorf("second event = %q/%q, want clear/cpu", clear.Kind, clear.Signal)
	}
	if clear.Threshold != DefaultCPUExit {
		t.Errorf("clear threshold = %v, want %v", clear.Threshold, DefaultCPUExit)
	}
	// Episode = StartedAt (t=0, the breach onset reported in StartedAt) → clear-fire
	// (t=115) = 115s. BreachedForSeconds is kept self-consistent with StartedAt.
	if clear.BreachedForSeconds != 115 {
		t.Errorf("BreachedForSeconds = %d, want 115", clear.BreachedForSeconds)
	}
	if clear.StartedAt != at(0).Format(time.RFC3339) {
		t.Errorf("clear StartedAt = %q, want episode start %q", clear.StartedAt, at(0).Format(time.RFC3339))
	}
	if clear.FiredAt != at(100+cdwell).Format(time.RFC3339) {
		t.Errorf("clear FiredAt = %q, want %q", clear.FiredAt, at(100+cdwell).Format(time.RFC3339))
	}
}

func TestObserve_ClearingInterrupted_StaysBreached(t *testing.T) {
	d := NewBreachDetector("w", BreachConfig{})
	evs := feed(d, []struct {
		rep WorkerReportPayload
		t   int
	}{
		{cpuRep(0.95), 0},
		{cpuRep(0.95), bdwell}, // breach at t=20
		{cpuRep(0.50), 100},    // under exit → CLEARING
		{cpuRep(0.90), 105},    // popped back over exit (in fact over enter) before clear dwell → BREACHED, no event
		{cpuRep(0.80), 130},    // band, stays BREACHED
	})
	if len(evs) != 1 || evs[0].Kind != "breach" {
		t.Fatalf("interrupted clearing must yield only the breach, got %d: %+v", len(evs), evs)
	}
	// Confirm a fresh sustained clear from here still works (episode start preserved).
	more := feed(d, []struct {
		rep WorkerReportPayload
		t   int
	}{
		{cpuRep(0.50), 200},
		{cpuRep(0.50), 200 + cdwell},
	})
	if len(more) != 1 || more[0].Kind != "clear" {
		t.Fatalf("clear after interrupted clearing failed: %+v", more)
	}
	if more[0].StartedAt != at(0).Format(time.RFC3339) {
		t.Errorf("episode start lost across interruption: StartedAt = %q", more[0].StartedAt)
	}
}

func TestObserve_ArmingDropsUnderEnter_Disarms(t *testing.T) {
	d := NewBreachDetector("w", BreachConfig{})
	// Over enter, then drop under enter (still above exit, in band) before dwell —
	// ARMING must disarm silently because the value is no longer over enter.
	evs := feed(d, []struct {
		rep WorkerReportPayload
		t   int
	}{
		{cpuRep(0.95), 0},  // ARMING
		{cpuRep(0.80), 10}, // under enter (0.80 < 0.85) → disarm to OK
		{cpuRep(0.80), 40}, // stays OK (no new arm; 0.80 still < enter)
	})
	if len(evs) != 0 {
		t.Fatalf("disarm-during-arming must give 0 events, got %d: %+v", len(evs), evs)
	}
}

func TestObserve_SignalsIndependent_SwapBreachedCPUOK(t *testing.T) {
	d := NewBreachDetector("w", BreachConfig{})
	// Swap breaches; cpu/memory stay OK throughout. Only a swap breach should fire.
	evs := feed(d, []struct {
		rep WorkerReportPayload
		t   int
	}{
		{swapRep(500), 0},      // swap over 256 → ARMING (cpu/mem OK)
		{swapRep(500), bdwell}, // swap breach fires
	})
	if len(evs) != 1 {
		t.Fatalf("want exactly 1 swap breach, got %d: %+v", len(evs), evs)
	}
	if evs[0].Signal != "swap" || evs[0].Kind != "breach" {
		t.Errorf("event = %q/%q, want swap/breach", evs[0].Signal, evs[0].Kind)
	}
	if evs[0].Threshold != DefaultSwapEnter {
		t.Errorf("threshold = %v, want %v", evs[0].Threshold, DefaultSwapEnter)
	}
}

func TestObserve_MemorySignal_LowFreeFractionBreaches(t *testing.T) {
	d := NewBreachDetector("w", BreachConfig{})
	// Free fraction 0.05 < 0.08 enter → breach; memory is the inverted-direction signal.
	evs := feed(d, []struct {
		rep WorkerReportPayload
		t   int
	}{
		{memRep(0.05), 0},
		{memRep(0.05), bdwell},
	})
	if len(evs) != 1 || evs[0].Signal != "memory" || evs[0].Kind != "breach" {
		t.Fatalf("want 1 memory breach, got %+v", evs)
	}
	if evs[0].Threshold != DefaultMemEnter {
		t.Errorf("threshold = %v, want %v", evs[0].Threshold, DefaultMemEnter)
	}
	// Now recover: free fraction 0.20 > 0.15 exit, sustained.
	more := feed(d, []struct {
		rep WorkerReportPayload
		t   int
	}{
		{memRep(0.20), 100},
		{memRep(0.20), 100 + cdwell},
	})
	if len(more) != 1 || more[0].Kind != "clear" {
		t.Fatalf("want 1 memory clear, got %+v", more)
	}
}

func TestObserve_MultipleSignalsSameSample(t *testing.T) {
	d := NewBreachDetector("w", BreachConfig{})
	// A sample that is simultaneously over enter on cpu AND swap.
	hot := WorkerReportPayload{
		NCPU: 8, Load5: 0.95 * 8, // cpu 0.95 over
		MemTotalMB: 16000, MemFreeMB: 8000, // mem OK
		SwapUsedMB: 500, // swap over
	}
	evs := feed(d, []struct {
		rep WorkerReportPayload
		t   int
	}{
		{hot, 0},
		{hot, bdwell},
	})
	if len(evs) != 2 {
		t.Fatalf("want cpu + swap breach in same maturing sample, got %d: %+v", len(evs), evs)
	}
	// Stable order: cpu before swap.
	if evs[0].Signal != "cpu" || evs[1].Signal != "swap" {
		t.Errorf("signal order = %q,%q want cpu,swap", evs[0].Signal, evs[1].Signal)
	}
}

func TestReset_MidBreach_EmitsClear(t *testing.T) {
	d := NewBreachDetector("w", BreachConfig{})
	feed(d, []struct {
		rep WorkerReportPayload
		t   int
	}{
		{cpuRep(0.95), 0},
		{cpuRep(0.95), bdwell}, // BREACHED
	})
	evs := d.Reset(at(60))
	if len(evs) != 1 || evs[0].Kind != "clear" || evs[0].Signal != "cpu" {
		t.Fatalf("Reset mid-breach must emit one cpu clear, got %+v", evs)
	}
	// Episode = StartedAt (t=0) → reset (t=60) = 60s, self-consistent with StartedAt.
	if evs[0].BreachedForSeconds != 60 {
		t.Errorf("BreachedForSeconds = %d, want 60", evs[0].BreachedForSeconds)
	}
	if evs[0].StartedAt != at(0).Format(time.RFC3339) {
		t.Errorf("StartedAt = %q, want %q", evs[0].StartedAt, at(0).Format(time.RFC3339))
	}
	// After reset the machine is OK: a fresh under-exit sample produces nothing.
	post := d.Observe(cpuRep(0.10), at(70))
	if len(post) != 0 {
		t.Fatalf("post-reset OK machine must be quiet, got %+v", post)
	}
}

func TestReset_WhileOK_EmitsNothing(t *testing.T) {
	d := NewBreachDetector("w", BreachConfig{})
	d.Observe(cpuRep(0.10), at(0)) // never breached
	evs := d.Reset(at(10))
	if len(evs) != 0 {
		t.Fatalf("Reset while OK must emit nothing, got %+v", evs)
	}
}

func TestReset_DuringClearing_EmitsClear(t *testing.T) {
	d := NewBreachDetector("w", BreachConfig{})
	feed(d, []struct {
		rep WorkerReportPayload
		t   int
	}{
		{cpuRep(0.95), 0},
		{cpuRep(0.95), bdwell}, // BREACHED at t=20
		{cpuRep(0.50), 100},    // CLEARING (not yet matured)
	})
	evs := d.Reset(at(105))
	if len(evs) != 1 || evs[0].Kind != "clear" {
		t.Fatalf("Reset during CLEARING must emit one clear, got %+v", evs)
	}
}

func TestObserve_GuardsNoPanicNoFalseFire(t *testing.T) {
	d := NewBreachDetector("w", BreachConfig{})
	// NCPU<=0 and MemTotalMB<=0: cpu and memory signals must be inert; swap still
	// reads normally. Drive a "would-be hot" cpu/mem sample with the guards tripped.
	bad := WorkerReportPayload{
		NCPU:       0,  // cpu guard
		Load5:      99, // would be enormous if divided
		MemTotalMB: 0,  // memory guard
		MemFreeMB:  0,  //
		SwapUsedMB: 10, // swap OK
	}
	evs := feed(d, []struct {
		rep WorkerReportPayload
		t   int
	}{
		{bad, 0},
		{bad, bdwell},
		{bad, 2 * bdwell},
	})
	if len(evs) != 0 {
		t.Fatalf("guarded (invalid) signals must not fire, got %+v", evs)
	}
}

func TestObserve_CustomConfigOverridesDefaults(t *testing.T) {
	// A tighter swap config: enter 100, exit 20, breach dwell 5s.
	d := NewBreachDetector("w", BreachConfig{
		BreachDwell: 5 * time.Second,
		SwapEnter:   100,
		SwapExit:    20,
	})
	evs := feed(d, []struct {
		rep WorkerReportPayload
		t   int
	}{
		{swapRep(150), 0},
		{swapRep(150), 5}, // 5s custom breach dwell matured
	})
	if len(evs) != 1 || evs[0].Signal != "swap" || evs[0].Threshold != 100 {
		t.Fatalf("custom config not honored: %+v", evs)
	}
}
