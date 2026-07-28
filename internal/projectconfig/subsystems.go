package projectconfig

// subsystems.go — the `subsystems:` block of .harmonik/config.yaml.
//
// # Purpose
//
// Configuration-driven partitioning: an operator names a subsystem here and
// switches it OFF, and the composition root then never CONSTRUCTS it. "Off"
// means ABSENT, not constructed-and-inert. Inert code still holds the
// composition root hostage and still costs; absent code forces the nil-guard
// question to be answered at the seam.
//
// # Shape
//
//	subsystems:
//	  reconciliation_scheduler:
//	    enabled: false
//
// A map keyed by subsystem name, each entry carrying an `enabled:` switch.
// The map shape (rather than a struct with one field per subsystem) is what
// makes adding the NEXT subsystem a one-line change: append a SubsystemName
// constant to knownSubsystems below, and gate its constructor at the seam.
//
// # Defaults and fail-loud
//
//   - Absent block, absent entry, or absent `enabled:` key → ENABLED. Every
//     subsystem is on until an operator explicitly turns it off, so an existing
//     deployment's behaviour is unchanged by this block appearing.
//   - An UNKNOWN subsystem name is a HARD ERROR (ErrUnknownSubsystem), and an
//     unknown key INSIDE an entry is a HARD ERROR too (ErrUnknownConfigKey,
//     the hk-9f3f precedent). A typo must never silently mean the wrong thing:
//     `enbaled: false` that is silently ignored leaves the operator believing a
//     subsystem was partitioned away while it kept running.
//
// The `enabled: *bool` idiom (nil = absent = default) matches the settled
// watchdog.enabled / keeper.self_service.crews_enabled precedent in
// projectconfig.go — deliberately not a new idiom.
//
// Codename: subsystem-partition-01.

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// SubsystemName is the config key identifying a switchable subsystem.
type SubsystemName string

const (
	// SubsystemReconciliationScheduler names the RC-020a scheduled detector
	// cadence started by daemon.StartReconciliationScheduler.
	SubsystemReconciliationScheduler SubsystemName = "reconciliation_scheduler"

	// SubsystemSocketListener names the daemon's Unix-socket listener and the
	// subtree of handlers constructed beneath it in daemon.bindSocket — comms,
	// crew, crew-idle-reap, the branch reaper, live-state, the dashboard socket
	// surface, the bandwidth tuner, the operator-pause and concurrency
	// controllers, the drain detector, and the queue handler adapter.
	//
	// This is the single largest partition lever in the daemon: one gate, up to
	// 13 constructions. "Up to" because three of them are themselves conditional
	// — the queue handler adapter and drain detector need BrPath, and the
	// bandwidth tuner needs a positive SubscriptionTokenCeiling.
	//
	// Note that the pre-existing `ProjectDir == ""` early return in the enclosing
	// wireSocketListener is NOT this switch — it is a unit-test escape hatch that
	// both production callers (cmd/harmonik/main.go, cmd/harmonik/run.go) set
	// unconditionally, so it is never taken in a real deployment.
	SubsystemSocketListener SubsystemName = "socket_listener"

	// SubsystemDashboardGate names the dashboard FORCING GATE evaluated inside
	// the core work loop — daemon.evaluateDashboardGate, whose result feeds
	// selectNextQueue. Distinct from the dashboard's socket surface, which is
	// constructed under SubsystemSocketListener.
	//
	// This gate is the ONLY route by which internal/dashboard enters package
	// daemon, and it makes the core dispatch loop read the captain's lanes.json.
	// It is the sharpest violation of "the core runs without it" on the boot
	// path. (It is not the only route to internal/digest — that is imported
	// independently by eagerfill, bootworkloop, and the governor block, so
	// gating this off does not shed that dependency.)
	SubsystemDashboardGate SubsystemName = "dashboard_gate"

	// SubsystemMovementGovernor names the sentinel's movement-governor blocks
	// evaluated inline in the core dispatch loop (observe and act modes).
	//
	// Observe mode runs on every production daemon today: a `br ready` shell-out
	// plus an events.jsonl scan on a two-minute cadence, emitting governor_signal.
	// It is dead by measurement — governor_signal has no non-test consumer, and
	// as of 2026-07-28 it was ~12% of this machine's event log. Re-derive that
	// figure rather than trusting it; it is a point-in-time reading, not an
	// invariant. Act mode additionally writes a dispatch-blocking entry to
	// the DecisionBlocker. Switching this off does NOT touch sentinel.ComputeSnapshot
	// or sentinel.DetectLayerA, which are unrelated per-run stall detectors that
	// merely share the package name.
	SubsystemMovementGovernor SubsystemName = "movement_governor"
)

// knownSubsystems is the closed set of names the `subsystems:` block accepts.
// Adding a switchable subsystem is a one-line addition here (plus the gate at
// its construction seam). Any name outside this set is rejected loudly.
var knownSubsystems = map[SubsystemName]struct{}{
	SubsystemReconciliationScheduler: {},
	SubsystemSocketListener:          {},
	SubsystemDashboardGate:           {},
	SubsystemMovementGovernor:        {},
}

// ErrUnknownSubsystem is returned when the subsystems: block names a subsystem
// the schema does not recognise. Unknown names are a HARD ERROR rather than a
// silent ignore, because a silently-ignored typo means the operator's intent
// (on or off) is not what the daemon does.
type ErrUnknownSubsystem struct {
	// Path is the absolute path to the config file.
	Path string
	// Name is the offending key under subsystems:.
	Name string
}

func (e *ErrUnknownSubsystem) Error() string {
	return fmt.Sprintf("project config %s: unknown subsystem %q under subsystems: "+
		"(known: %s)", e.Path, e.Name, knownSubsystemsList())
}

// knownSubsystemsList renders the accepted names for the error message, sorted
// so the message is deterministic. Mirrors agentTypeNamesForError.
func knownSubsystemsList() string {
	names := make([]string, 0, len(knownSubsystems))
	for name := range knownSubsystems {
		names = append(names, string(name))
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

// rawSubsystemEntry is one entry under the subsystems: block. Enabled is a
// *bool so nil (absent) resolves to the default (true) while an explicit false
// is honoured without ambiguity — the watchdog.enabled idiom.
type rawSubsystemEntry struct {
	Enabled *bool `yaml:"enabled"`
}

// SubsystemsConfig is the resolved subsystems: block. The zero value (absent
// block) enables every subsystem, so a deployment with no subsystems: block
// behaves exactly as it did before the block existed.
type SubsystemsConfig struct {
	// disabled holds the names explicitly switched off. Nil = nothing off.
	// Only DISABLED names are stored, so "not present" unambiguously means
	// "default", and Enabled needs no second "was it configured" flag.
	disabled map[SubsystemName]struct{}
}

// Enabled reports whether the named subsystem should be CONSTRUCTED. It returns
// true for every name that was not explicitly switched off, including the zero
// value of SubsystemsConfig.
//
// Callers MUST use this at the construction seam — `if cfg.Enabled(X) { … }`
// around the constructor — not inside the subsystem to make it inert.
func (c SubsystemsConfig) Enabled(name SubsystemName) bool {
	_, off := c.disabled[name]
	return !off
}

// parseSubsystemsBlock converts the raw subsystems: map into a SubsystemsConfig.
// The daemon MUST refuse to start on either error it returns:
//   - *ErrUnknownSubsystem — the map key names no known subsystem.
//   - *ErrUnknownConfigKey — an entry carries a key other than `enabled:`.
//
// Entries arrive as yaml.Node (not a decoded struct) precisely so the second
// check is possible: the top-level unmarshal is deliberately tolerant, so a
// mistyped inner key would otherwise vanish without trace.
func parseSubsystemsBlock(path string, raw map[string]yaml.Node) (SubsystemsConfig, error) {
	if len(raw) == 0 {
		return SubsystemsConfig{}, nil
	}
	entryType := reflect.TypeOf(rawSubsystemEntry{})
	var cfg SubsystemsConfig
	for key, node := range raw {
		name := SubsystemName(key)
		if _, known := knownSubsystems[name]; !known {
			return SubsystemsConfig{}, &ErrUnknownSubsystem{Path: path, Name: key}
		}
		if keyPath, ok := unknownYAMLKey(&node, entryType, "subsystems."+key); !ok {
			return SubsystemsConfig{}, &ErrUnknownConfigKey{
				Path:    path,
				KeyPath: keyPath,
				Cause:   fmt.Errorf("unknown config key %q", keyPath),
			}
		}
		var entry rawSubsystemEntry
		if err := node.Decode(&entry); err != nil {
			return SubsystemsConfig{}, &ErrMalformedConfigYAML{Path: path, Cause: err}
		}
		// Absent enabled: → default (on). Explicit true → on. Only an explicit
		// false records a disable.
		if entry.Enabled != nil && !*entry.Enabled {
			if cfg.disabled == nil {
				cfg.disabled = make(map[SubsystemName]struct{}, len(raw))
			}
			cfg.disabled[name] = struct{}{}
		}
	}
	return cfg, nil
}
