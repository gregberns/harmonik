package workers

import (
	"encoding/json"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// ResourceBreachPayload is the typed event payload for the resource_breach event
// (worker-report Phase 2, PB1).
//
// It describes one transition of a single (worker × signal) breach state
// machine: either the onset of a sustained breach (Kind "breach") or its
// sustained recovery (Kind "clear"). One event is emitted per transition — never
// per sample — so a flapping signal in the hysteresis band produces no noise.
//
// Value is the normalized signal value that crossed the threshold at the moment
// the transition fired (e.g. load5/ncpu for cpu, mem_free/mem_total for memory,
// swap_used_mb for swap); Threshold is the configured enter (for "breach") or
// exit (for "clear") threshold the value was compared against. BreachedForSeconds
// is zero on a "breach" event and the episode duration (breach-fire → clear-fire)
// on a "clear" event.
//
// InFlight is NOT populated by the detector — it has no knowledge of dispatch
// state. The caller (PB3) stamps the worker's current in-flight slot count onto
// each emitted event before it is evented. It is left zero-valued here.
//
// Durability class: O (ordinary — operator observability; the breach/clear state
// is reconstructible from the worker_report event stream). Event: "resource_breach".
type ResourceBreachPayload struct {
	// WorkerName is the name of the worker the breach describes.
	WorkerName string `json:"worker_name"`
	// Kind is the transition kind: "breach" (onset) or "clear" (recovery).
	Kind string `json:"kind"`
	// Signal is the breached resource signal: "cpu", "memory", or "swap".
	Signal string `json:"signal"`
	// Value is the normalized signal value at the moment the transition fired.
	Value float64 `json:"value"`
	// Threshold is the enter (breach) or exit (clear) threshold compared against.
	Threshold float64 `json:"threshold"`
	// BreachedForSeconds is the episode duration in seconds — 0 on a "breach"
	// event, and StartedAt→FiredAt elapsed on a "clear" event (kept consistent
	// with the StartedAt/FiredAt this same payload reports).
	BreachedForSeconds int `json:"breached_for_seconds"`
	// InFlight is the worker's in-flight slot count at emit time. NOT set by the
	// detector (it has no dispatch knowledge) — stamped by the caller (PB3).
	InFlight int `json:"in_flight"`
	// StartedAt is the RFC 3339 UTC timestamp of the breach episode's start (the
	// first over-enter sample of the ARMING run that produced the breach).
	StartedAt string `json:"started_at"`
	// FiredAt is the RFC 3339 UTC timestamp at which this transition event fired.
	FiredAt string `json:"fired_at"`
}

const (
	breachKindBreach = "breach"
	breachKindClear  = "clear"

	signalCPU    = "cpu"
	signalMemory = "memory"
	signalSwap   = "swap"
)

func init() {
	if err := core.RegisterEventType(core.EventTypeResourceBreach, func() core.EventPayload { return &ResourceBreachPayload{} }); err != nil {
		panic("workers: init: register resource_breach: " + err.Error())
	}
	if err := core.RegisterPayloadCompatEntry(core.PayloadCompatEntry{
		TypeName:          core.EventTypeResourceBreach,
		CurrentVersion:    1,
		CompatWindowHolds: true,
		AdditiveOnly:      true,
	}); err != nil {
		panic("workers: init: register resource_breach compatibility: " + err.Error()) //nolint:forbidigo // init-time registry wiring: a duplicate or bad registration is a build-time bug, and there is no caller to return an error to.
	}
}

// Default dwell windows. A signal must stay over its enter threshold for
// DefaultBreachDwell before a breach fires, and under its exit threshold for
// DefaultClearDwell before the clear fires. These are package consts (not
// hardcoded deep in the machine) so PB3 can override them via BreachConfig.
const (
	DefaultBreachDwell = 20 * time.Second
	DefaultClearDwell  = 15 * time.Second
)

// Default hysteresis thresholds per signal. Enter > exit for every signal so
// there is a dead-band between them; a sample in the band while BREACHED neither
// re-fires nor clears.
const (
	// cpu: normalized load5/ncpu. Enter at 0.85, recover under 0.70.
	DefaultCPUEnter = 0.85
	DefaultCPUExit  = 0.70
	// memory: free fraction mem_free/mem_total — LOW is the breach direction, so
	// enter when the free fraction drops BELOW 0.08, recover when it rises ABOVE
	// 0.15. (Direction is inverted vs cpu/swap; see signalMemory handling.)
	DefaultMemEnter = 0.08
	DefaultMemExit  = 0.15
	// swap: swap_used_mb. Enter above 256 MB, recover under 64 MB.
	DefaultSwapEnter = 256.0
	DefaultSwapExit  = 64.0
)

// BreachConfig holds the thresholds + dwell windows for one breach detector.
// PB3 feeds real per-worker config here; NewBreachDetector(BreachConfig{}) (the
// zero value) selects every package default, so callers can override only the
// fields they care about and leave the rest defaulted.
type BreachConfig struct {
	BreachDwell time.Duration
	ClearDwell  time.Duration

	CPUEnter  float64
	CPUExit   float64
	MemEnter  float64
	MemExit   float64
	SwapEnter float64
	SwapExit  float64
}

func (c BreachConfig) withDefaults() BreachConfig {
	if c.BreachDwell <= 0 {
		c.BreachDwell = DefaultBreachDwell
	}
	if c.ClearDwell <= 0 {
		c.ClearDwell = DefaultClearDwell
	}
	if c.CPUEnter == 0 {
		c.CPUEnter = DefaultCPUEnter
	}
	if c.CPUExit == 0 {
		c.CPUExit = DefaultCPUExit
	}
	if c.MemEnter == 0 {
		c.MemEnter = DefaultMemEnter
	}
	if c.MemExit == 0 {
		c.MemExit = DefaultMemExit
	}
	if c.SwapEnter == 0 {
		c.SwapEnter = DefaultSwapEnter
	}
	if c.SwapExit == 0 {
		c.SwapExit = DefaultSwapExit
	}
	return c
}

type breachState int

const (
	stateOK breachState = iota
	stateArming
	stateBreached
	stateClearing
)

type signalSpec struct {
	name  string
	value func(rep WorkerReportPayload) (float64, bool) // (value, valid)
	enter float64
	exit  float64
	over  func(value, threshold float64) bool
}

func higherIsWorse(value, threshold float64) bool { return value > threshold }
func lowerIsWorse(value, threshold float64) bool  { return value < threshold }

type signalMachine struct {
	spec  signalSpec
	state breachState

	// armingSince is the first over-enter sample time of the current ARMING run.
	armingSince time.Time
	// breachStart is the StartedAt of the active breach episode (the armingSince
	// that matured into a breach), retained across BREACHED/CLEARING so a clear
	// reports the full episode duration and the original start.
	breachStart time.Time
	// clearingSince is the first under-exit sample time of the current CLEARING run.
	clearingSince time.Time
}

type breachDetector struct {
	workerName string
	cfg        BreachConfig
	signals    []*signalMachine
}

// NewBreachDetector builds a detector for workerName with cfg (zero-value cfg ⇒
// all package defaults). It installs the three signal machines (cpu, memory,
// swap) in a stable order so emitted events are deterministic.
func NewBreachDetector(workerName string, cfg BreachConfig) *breachDetector {
	cfg = cfg.withDefaults()
	specs := []signalSpec{
		{
			name:  signalCPU,
			enter: cfg.CPUEnter,
			exit:  cfg.CPUExit,
			over:  higherIsWorse,
			value: func(rep WorkerReportPayload) (float64, bool) {
				if rep.NCPU <= 0 {
					return 0, false
				}
				return rep.Load5 / float64(rep.NCPU), true
			},
		},
		{
			name:  signalMemory,
			enter: cfg.MemEnter,
			exit:  cfg.MemExit,
			over:  lowerIsWorse,
			value: func(rep WorkerReportPayload) (float64, bool) {
				if rep.MemTotalMB <= 0 {
					return 0, false
				}
				return float64(rep.MemFreeMB) / float64(rep.MemTotalMB), true
			},
		},
		{
			name:  signalSwap,
			enter: cfg.SwapEnter,
			exit:  cfg.SwapExit,
			over:  higherIsWorse,
			value: func(rep WorkerReportPayload) (float64, bool) {
				return float64(rep.SwapUsedMB), true
			},
		},
	}
	d := &breachDetector{workerName: workerName, cfg: cfg}
	for _, s := range specs {
		d.signals = append(d.signals, &signalMachine{spec: s, state: stateOK})
	}
	return d
}

// Observe feeds one sample at injected time `now` through every signal machine
// and returns the breach/clear events produced by this sample (0+). Events are
// returned in stable signal order (cpu, memory, swap). InFlight is left zero on
// every event — the caller stamps it.
func (d *breachDetector) Observe(rep WorkerReportPayload, now time.Time) []ResourceBreachPayload {
	var out []ResourceBreachPayload
	for _, m := range d.signals {
		if ev, ok := d.step(m, rep, now); ok {
			out = append(out, ev)
		}
	}
	return out
}

func (d *breachDetector) step(m *signalMachine, rep WorkerReportPayload, now time.Time) (ResourceBreachPayload, bool) {
	value, valid := m.spec.value(rep)
	overEnter := valid && m.spec.over(value, m.spec.enter)
	underExit := valid && !m.spec.over(value, m.spec.exit)

	switch m.state {
	case stateOK:
		if overEnter {
			m.state = stateArming
			m.armingSince = now
		}

	case stateArming:
		if !overEnter {
			m.state = stateOK
			m.armingSince = time.Time{}
			break
		}
		if now.Sub(m.armingSince) >= d.cfg.BreachDwell {
			m.state = stateBreached
			m.breachStart = m.armingSince
			m.armingSince = time.Time{}
			return ResourceBreachPayload{
				WorkerName: d.workerName,
				Kind:       breachKindBreach,
				Signal:     m.spec.name,
				Value:      value,
				Threshold:  m.spec.enter,
				StartedAt:  m.breachStart.UTC().Format(time.RFC3339),
				FiredAt:    now.UTC().Format(time.RFC3339),
			}, true
		}

	case stateBreached:
		if underExit {
			m.state = stateClearing
			m.clearingSince = now
		}

	case stateClearing:
		if !underExit {
			m.state = stateBreached
			m.clearingSince = time.Time{}
			break
		}
		if now.Sub(m.clearingSince) >= d.cfg.ClearDwell {
			ev := ResourceBreachPayload{
				WorkerName:         d.workerName,
				Kind:               breachKindClear,
				Signal:             m.spec.name,
				Value:              value,
				Threshold:          m.spec.exit,
				BreachedForSeconds: int(now.Sub(m.breachStart).Seconds()),
				StartedAt:          m.breachStart.UTC().Format(time.RFC3339),
				FiredAt:            now.UTC().Format(time.RFC3339),
			}
			m.state = stateOK
			m.clearingSince = time.Time{}
			m.breachStart = time.Time{}
			return ev, true
		}
	}

	return ResourceBreachPayload{}, false
}

// Reset returns every signal to OK and emits a "clear" event for any signal that
// was currently BREACHED or CLEARING (i.e. mid-breach episode). PB3 calls this on
// the worker's transition-to-idle so a breach episode does not dangle open after
// dispatch stops. Events are returned in stable signal order; OK/ARMING signals
// emit nothing.
//
// The emitted clear reports the episode duration (breach-fire → now) and the
// original StartedAt, with Threshold set to the signal's exit threshold for
// symmetry with an organically-fired clear. Value is left zero — Reset is not
// driven by a sample.
func (d *breachDetector) Reset(now time.Time) []ResourceBreachPayload {
	var out []ResourceBreachPayload
	for _, m := range d.signals {
		breached := m.state == stateBreached || m.state == stateClearing
		if breached {
			out = append(out, ResourceBreachPayload{
				WorkerName:         d.workerName,
				Kind:               breachKindClear,
				Signal:             m.spec.name,
				Threshold:          m.spec.exit,
				BreachedForSeconds: int(now.Sub(m.breachStart).Seconds()),
				StartedAt:          m.breachStart.UTC().Format(time.RFC3339),
				FiredAt:            now.UTC().Format(time.RFC3339),
			})
		}
		m.state = stateOK
		m.armingSince = time.Time{}
		m.breachStart = time.Time{}
		m.clearingSince = time.Time{}
	}
	return out
}

func marshalResourceBreach(p ResourceBreachPayload) ([]byte, error) {
	return json.Marshal(p)
}
