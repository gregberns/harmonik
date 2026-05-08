package core

import (
	"encoding/json"
	"fmt"
)

// LLMFreedom is the llm-freedom axis of the four-axis determinism classification
// (architecture.md §4.1 AR-001).
//
// Values: none, bounded, unbounded.
// Baseline (AR-002): none.
type LLMFreedom string

// LLMFreedom values per architecture.md §4.1 AR-001.
const (
	// LLMFreedomNone means no LLM invocation occurs at this point;
	// the evaluation is fully deterministic (skeleton operation per AR-003).
	LLMFreedomNone LLMFreedom = "none"

	// LLMFreedomBounded means an LLM is invoked but operates within a
	// constrained output space (e.g., constrained decoding, enum-shaped output).
	LLMFreedomBounded LLMFreedom = "bounded"

	// LLMFreedomUnbounded means an LLM is invoked with unconstrained output
	// (organ operation per AR-003; MUST reside in a handler subprocess, not the daemon).
	LLMFreedomUnbounded LLMFreedom = "unbounded"
)

// Valid reports whether f is one of the three declared LLMFreedom constants.
// An unknown value MUST be treated as a classification error per AR-001.
func (f LLMFreedom) Valid() bool {
	switch f {
	case LLMFreedomNone, LLMFreedomBounded, LLMFreedomUnbounded:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so LLMFreedom serialises
// correctly in JSON and YAML.
func (f LLMFreedom) MarshalText() ([]byte, error) {
	if !f.Valid() {
		return nil, fmt.Errorf("llmfreedom: unknown value %q", string(f))
	}
	return []byte(f), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value not declared in the closed set per AR-001.
func (f *LLMFreedom) UnmarshalText(text []byte) error {
	v := LLMFreedom(text)
	if !v.Valid() {
		return fmt.Errorf(
			"llmfreedom: unknown value %q; must be one of none, bounded, unbounded",
			string(text),
		)
	}
	*f = v
	return nil
}

// IODeterminism is the io-determinism axis of the four-axis determinism classification
// (architecture.md §4.1 AR-001).
//
// Values: deterministic, best-effort, nondeterministic.
// Baseline (AR-002): deterministic.
type IODeterminism string

// IODeterminism values per architecture.md §4.1 AR-001.
const (
	// IODeterminismDeterministic means the I/O operation always produces the
	// same output for the same input with no external side-effects beyond
	// the declared state mutation.
	IODeterminismDeterministic IODeterminism = "deterministic"

	// IODeterminismBestEffort means the operation attempts determinism but
	// may produce varying results due to environmental conditions (e.g., network).
	IODeterminismBestEffort IODeterminism = "best-effort"

	// IODeterminismNondeterministic means the operation's output depends on
	// factors outside the caller's control (LLM inference, external services).
	IODeterminismNondeterministic IODeterminism = "nondeterministic"
)

// Valid reports whether d is one of the three declared IODeterminism constants.
func (d IODeterminism) Valid() bool {
	switch d {
	case IODeterminismDeterministic, IODeterminismBestEffort, IODeterminismNondeterministic:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so IODeterminism serialises
// correctly in JSON and YAML.
func (d IODeterminism) MarshalText() ([]byte, error) {
	if !d.Valid() {
		return nil, fmt.Errorf("iodeterminism: unknown value %q", string(d))
	}
	return []byte(d), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value not declared in the closed set per AR-001.
func (d *IODeterminism) UnmarshalText(text []byte) error {
	v := IODeterminism(text)
	if !v.Valid() {
		return fmt.Errorf(
			"iodeterminism: unknown value %q; must be one of deterministic, best-effort, nondeterministic",
			string(text),
		)
	}
	*d = v
	return nil
}

// ReplaySafety is the replay-safety axis of the four-axis determinism classification
// (architecture.md §4.1 AR-001).
//
// Values: safe, unsafe, n/a.
// Baseline (AR-002): safe.
type ReplaySafety string

// ReplaySafety values per architecture.md §4.1 AR-001.
const (
	// ReplaySafetySafe means the operation can be replayed without additional
	// side-effects; replaying produces an identical result.
	ReplaySafetySafe ReplaySafety = "safe"

	// ReplaySafetyUnsafe means the operation MUST NOT be replayed naively;
	// replay may cause duplicate side-effects or incorrect state transitions.
	ReplaySafetyUnsafe ReplaySafety = "unsafe"

	// ReplaySafetyNA means replay-safety is not applicable to this operation
	// (e.g., read-only operations that have no replay semantics).
	ReplaySafetyNA ReplaySafety = "n/a"
)

// Valid reports whether s is one of the three declared ReplaySafety constants.
func (s ReplaySafety) Valid() bool {
	switch s {
	case ReplaySafetySafe, ReplaySafetyUnsafe, ReplaySafetyNA:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so ReplaySafety serialises
// correctly in JSON and YAML.
func (s ReplaySafety) MarshalText() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("replaysafety: unknown value %q", string(s))
	}
	return []byte(s), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value not declared in the closed set per AR-001.
func (s *ReplaySafety) UnmarshalText(text []byte) error {
	v := ReplaySafety(text)
	if !v.Valid() {
		return fmt.Errorf(
			"replaysafety: unknown value %q; must be one of safe, unsafe, n/a",
			string(text),
		)
	}
	*s = v
	return nil
}

// AxisIdempotency is the idempotency axis of the four-axis determinism classification
// (architecture.md §4.1 AR-001).
//
// Note: this is distinct from [IdempotencyClass] (execution-model.md §6.1), which
// is the per-node tag on a workflow Node. AxisIdempotency is the axis value carried
// in the AxisTags four-tuple for cross-subsystem classification.
//
// Values: idempotent, non-idempotent, recoverable-non-idempotent, n/a.
// Baseline (AR-002): idempotent.
type AxisIdempotency string

// AxisIdempotency values per architecture.md §4.1 AR-001.
const (
	// AxisIdempotencyIdempotent means the operation is idempotent: repeated
	// invocations with the same inputs produce the same result with no
	// additional side-effects.
	AxisIdempotencyIdempotent AxisIdempotency = "idempotent"

	// AxisIdempotencyNonIdempotent means the operation is not idempotent;
	// repeated invocations may produce different results or accumulate side-effects.
	AxisIdempotencyNonIdempotent AxisIdempotency = "non-idempotent"

	// AxisIdempotencyRecoverableNonIdempotent means the operation is not idempotent
	// but the side-effects from a failed or partial invocation can be recovered
	// through a defined compensating procedure.
	AxisIdempotencyRecoverableNonIdempotent AxisIdempotency = "recoverable-non-idempotent"

	// AxisIdempotencyNA means idempotency is not applicable to this operation.
	AxisIdempotencyNA AxisIdempotency = "n/a"
)

// Valid reports whether a is one of the four declared AxisIdempotency constants.
func (a AxisIdempotency) Valid() bool {
	switch a {
	case AxisIdempotencyIdempotent,
		AxisIdempotencyNonIdempotent,
		AxisIdempotencyRecoverableNonIdempotent,
		AxisIdempotencyNA:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so AxisIdempotency serialises
// correctly in JSON and YAML.
func (a AxisIdempotency) MarshalText() ([]byte, error) {
	if !a.Valid() {
		return nil, fmt.Errorf("axisidempotency: unknown value %q", string(a))
	}
	return []byte(a), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value not declared in the closed set per AR-001.
func (a *AxisIdempotency) UnmarshalText(text []byte) error {
	v := AxisIdempotency(text)
	if !v.Valid() {
		return fmt.Errorf(
			"axisidempotency: unknown value %q; must be one of idempotent, non-idempotent, recoverable-non-idempotent, n/a",
			string(text),
		)
	}
	*a = v
	return nil
}

// AxisTags is the four-axis determinism classification tuple applied to every
// type, interface, and evaluation point that crosses a subsystem boundary
// (architecture.md §4.1 AR-001 through AR-004).
//
// The four axes are:
//
//   - LLMFreedom:    llm-freedom ∈ {none, bounded, unbounded}
//   - IODeterminism: io-determinism ∈ {deterministic, best-effort, nondeterministic}
//   - ReplaySafety:  replay-safety ∈ {safe, unsafe, n/a}
//   - Idempotency:   idempotency ∈ {idempotent, non-idempotent, recoverable-non-idempotent, n/a}
//
// The baseline tuple (AR-002) is:
//
//	LLMFreedomNone, IODeterminismDeterministic, ReplaySafetySafe, AxisIdempotencyIdempotent
//
// Used by: execution-model.md §6.1 RECORD Node (axes : AxisTags) and RECORD Workflow.
//
// Wire format: JSON object with fields llm_freedom, io_determinism, replay_safety, idempotency.
// Each field round-trips through its sub-enum's MarshalText/UnmarshalText.
type AxisTags struct {
	// LLMFreedom is the llm-freedom axis value.
	// Baseline: LLMFreedomNone.
	LLMFreedom LLMFreedom `json:"llm_freedom"`

	// IODeterminism is the io-determinism axis value.
	// Baseline: IODeterminismDeterministic.
	IODeterminism IODeterminism `json:"io_determinism"`

	// ReplaySafety is the replay-safety axis value.
	// Baseline: ReplaySafetySafe.
	ReplaySafety ReplaySafety `json:"replay_safety"`

	// Idempotency is the idempotency axis value.
	// Baseline: AxisIdempotencyIdempotent.
	Idempotency AxisIdempotency `json:"idempotency"`
}

// BaselineAxisTags is the baseline four-axis tuple per architecture.md §4.1 AR-002:
// (llm-freedom=none, io-determinism=deterministic, replay-safety=safe, idempotency=idempotent).
//
// A requirement that matches baseline on every axis MAY omit the Axes: line;
// reviewers infer baseline from absence.
var BaselineAxisTags = AxisTags{
	LLMFreedom:    LLMFreedomNone,
	IODeterminism: IODeterminismDeterministic,
	ReplaySafety:  ReplaySafetySafe,
	Idempotency:   AxisIdempotencyIdempotent,
}

// Valid reports whether all four axis values are declared constants.
// An AxisTags with any unknown axis value is invalid; callers MUST NOT
// persist or forward invalid tags per AR-001.
func (t AxisTags) Valid() bool {
	return t.LLMFreedom.Valid() &&
		t.IODeterminism.Valid() &&
		t.ReplaySafety.Valid() &&
		t.Idempotency.Valid()
}

// axistagsJSON is the wire shape used for JSON marshal/unmarshal.
// The sub-enum TextMarshaler/TextUnmarshaler implementations enforce validation.
type axistagsJSON struct {
	LLMFreedom    LLMFreedom      `json:"llm_freedom"`
	IODeterminism IODeterminism   `json:"io_determinism"`
	ReplaySafety  ReplaySafety    `json:"replay_safety"`
	Idempotency   AxisIdempotency `json:"idempotency"`
}

// MarshalJSON implements json.Marshaler.
// It rejects any AxisTags whose axis values are not declared constants.
func (t AxisTags) MarshalJSON() ([]byte, error) {
	if !t.Valid() {
		return nil, fmt.Errorf(
			"axistags: invalid tuple (%q, %q, %q, %q)",
			string(t.LLMFreedom),
			string(t.IODeterminism),
			string(t.ReplaySafety),
			string(t.Idempotency),
		)
	}
	return json.Marshal(axistagsJSON{
		LLMFreedom:    t.LLMFreedom,
		IODeterminism: t.IODeterminism,
		ReplaySafety:  t.ReplaySafety,
		Idempotency:   t.Idempotency,
	})
}

// UnmarshalJSON implements json.Unmarshaler.
// It rejects any axis value not in the declared constant set per AR-001.
func (t *AxisTags) UnmarshalJSON(data []byte) error {
	var wire axistagsJSON
	if err := json.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("axistags: json decode: %w", err)
	}
	t.LLMFreedom = wire.LLMFreedom
	t.IODeterminism = wire.IODeterminism
	t.ReplaySafety = wire.ReplaySafety
	t.Idempotency = wire.Idempotency
	return nil
}
