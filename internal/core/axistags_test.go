package core

import (
	"encoding/json"
	"testing"
)

// --- LLMFreedom ---

func TestLLMFreedomValid(t *testing.T) {
	t.Parallel()

	valid := []LLMFreedom{
		LLMFreedomNone,
		LLMFreedomBounded,
		LLMFreedomUnbounded,
	}
	for _, f := range valid {
		if !f.Valid() {
			t.Errorf("expected %q to be valid", f)
		}
	}

	invalid := []LLMFreedom{
		"",
		"NONE",
		"None",
		"free",
		"full",
		"unbounded ",
		"unknown",
	}
	for _, f := range invalid {
		if f.Valid() {
			t.Errorf("expected %q to be invalid", f)
		}
	}
}

func TestLLMFreedomMarshalText(t *testing.T) {
	t.Parallel()

	cases := []struct {
		value LLMFreedom
		want  string
	}{
		{LLMFreedomNone, "none"},
		{LLMFreedomBounded, "bounded"},
		{LLMFreedomUnbounded, "unbounded"},
	}
	for _, tc := range cases {
		got, err := tc.value.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%q) error: %v", tc.value, err)
		}
		if string(got) != tc.want {
			t.Errorf("MarshalText(%q) = %q, want %q", tc.value, got, tc.want)
		}
	}

	if _, err := LLMFreedom("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}
}

func TestLLMFreedomUnmarshalText(t *testing.T) {
	t.Parallel()

	type axisFixtureLLMFreedomWrapper struct {
		F LLMFreedom `json:"llm_freedom"`
	}

	tests := []struct {
		name    string
		input   string
		want    LLMFreedom
		wantErr bool
	}{
		{name: "none", input: `{"llm_freedom":"none"}`, want: LLMFreedomNone},
		{name: "bounded", input: `{"llm_freedom":"bounded"}`, want: LLMFreedomBounded},
		{name: "unbounded", input: `{"llm_freedom":"unbounded"}`, want: LLMFreedomUnbounded},
		{name: "unknown", input: `{"llm_freedom":"unknown"}`, wantErr: true},
		{name: "empty", input: `{"llm_freedom":""}`, wantErr: true},
		{name: "uppercase NONE", input: `{"llm_freedom":"NONE"}`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w axisFixtureLLMFreedomWrapper
			err := json.Unmarshal([]byte(tc.input), &w)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for input %q, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error for input %q: %v", tc.input, err)
				return
			}
			if w.F != tc.want {
				t.Errorf("got %q, want %q", w.F, tc.want)
			}
		})
	}
}

// --- IODeterminism ---

func TestIODeterminismValid(t *testing.T) {
	t.Parallel()

	valid := []IODeterminism{
		IODeterminismDeterministic,
		IODeterminismBestEffort,
		IODeterminismNondeterministic,
	}
	for _, d := range valid {
		if !d.Valid() {
			t.Errorf("expected %q to be valid", d)
		}
	}

	invalid := []IODeterminism{
		"",
		"DETERMINISTIC",
		"Deterministic",
		"nondeterministic ",
		"best_effort",
		"unknown",
	}
	for _, d := range invalid {
		if d.Valid() {
			t.Errorf("expected %q to be invalid", d)
		}
	}
}

func TestIODeterminismMarshalText(t *testing.T) {
	t.Parallel()

	cases := []struct {
		value IODeterminism
		want  string
	}{
		{IODeterminismDeterministic, "deterministic"},
		{IODeterminismBestEffort, "best-effort"},
		{IODeterminismNondeterministic, "nondeterministic"},
	}
	for _, tc := range cases {
		got, err := tc.value.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%q) error: %v", tc.value, err)
		}
		if string(got) != tc.want {
			t.Errorf("MarshalText(%q) = %q, want %q", tc.value, got, tc.want)
		}
	}

	if _, err := IODeterminism("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}
}

func TestIODeterminismUnmarshalText(t *testing.T) {
	t.Parallel()

	type axisFixtureIODeterminismWrapper struct {
		D IODeterminism `json:"io_determinism"`
	}

	tests := []struct {
		name    string
		input   string
		want    IODeterminism
		wantErr bool
	}{
		{name: "deterministic", input: `{"io_determinism":"deterministic"}`, want: IODeterminismDeterministic},
		{name: "best-effort", input: `{"io_determinism":"best-effort"}`, want: IODeterminismBestEffort},
		{name: "nondeterministic", input: `{"io_determinism":"nondeterministic"}`, want: IODeterminismNondeterministic},
		{name: "underscore variant best_effort", input: `{"io_determinism":"best_effort"}`, wantErr: true},
		{name: "uppercase", input: `{"io_determinism":"DETERMINISTIC"}`, wantErr: true},
		{name: "empty", input: `{"io_determinism":""}`, wantErr: true},
		{name: "unknown", input: `{"io_determinism":"random"}`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w axisFixtureIODeterminismWrapper
			err := json.Unmarshal([]byte(tc.input), &w)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for input %q, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error for input %q: %v", tc.input, err)
				return
			}
			if w.D != tc.want {
				t.Errorf("got %q, want %q", w.D, tc.want)
			}
		})
	}
}

// --- ReplaySafety ---

func TestReplaySafetyValid(t *testing.T) {
	t.Parallel()

	valid := []ReplaySafety{
		ReplaySafetySafe,
		ReplaySafetyUnsafe,
		ReplaySafetyNA,
	}
	for _, s := range valid {
		if !s.Valid() {
			t.Errorf("expected %q to be valid", s)
		}
	}

	invalid := []ReplaySafety{
		"",
		"SAFE",
		"Safe",
		"na",
		"N/A",
		"not-applicable",
		"unknown",
	}
	for _, s := range invalid {
		if s.Valid() {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

func TestReplaySafetyMarshalText(t *testing.T) {
	t.Parallel()

	cases := []struct {
		value ReplaySafety
		want  string
	}{
		{ReplaySafetySafe, "safe"},
		{ReplaySafetyUnsafe, "unsafe"},
		{ReplaySafetyNA, "n/a"},
	}
	for _, tc := range cases {
		got, err := tc.value.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%q) error: %v", tc.value, err)
		}
		if string(got) != tc.want {
			t.Errorf("MarshalText(%q) = %q, want %q", tc.value, got, tc.want)
		}
	}

	if _, err := ReplaySafety("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}
}

func TestReplaySafetyUnmarshalText(t *testing.T) {
	t.Parallel()

	type axisFixtureReplaySafetyWrapper struct {
		S ReplaySafety `json:"replay_safety"`
	}

	tests := []struct {
		name    string
		input   string
		want    ReplaySafety
		wantErr bool
	}{
		{name: "safe", input: `{"replay_safety":"safe"}`, want: ReplaySafetySafe},
		{name: "unsafe", input: `{"replay_safety":"unsafe"}`, want: ReplaySafetyUnsafe},
		{name: "n/a", input: `{"replay_safety":"n/a"}`, want: ReplaySafetyNA},
		{name: "na without slash", input: `{"replay_safety":"na"}`, wantErr: true},
		{name: "uppercase N/A", input: `{"replay_safety":"N/A"}`, wantErr: true},
		{name: "empty", input: `{"replay_safety":""}`, wantErr: true},
		{name: "unknown", input: `{"replay_safety":"yes"}`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w axisFixtureReplaySafetyWrapper
			err := json.Unmarshal([]byte(tc.input), &w)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for input %q, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error for input %q: %v", tc.input, err)
				return
			}
			if w.S != tc.want {
				t.Errorf("got %q, want %q", w.S, tc.want)
			}
		})
	}
}

// --- AxisIdempotency ---

func TestAxisIdempotencyValid(t *testing.T) {
	t.Parallel()

	valid := []AxisIdempotency{
		AxisIdempotencyIdempotent,
		AxisIdempotencyNonIdempotent,
		AxisIdempotencyRecoverableNonIdempotent,
		AxisIdempotencyNA,
	}
	for _, a := range valid {
		if !a.Valid() {
			t.Errorf("expected %q to be valid", a)
		}
	}

	invalid := []AxisIdempotency{
		"",
		"IDEMPOTENT",
		"Idempotent",
		"non_idempotent",
		"recoverable_non_idempotent",
		"na",
		"N/A",
		"unknown",
	}
	for _, a := range invalid {
		if a.Valid() {
			t.Errorf("expected %q to be invalid", a)
		}
	}
}

func TestAxisIdempotencyMarshalText(t *testing.T) {
	t.Parallel()

	cases := []struct {
		value AxisIdempotency
		want  string
	}{
		{AxisIdempotencyIdempotent, "idempotent"},
		{AxisIdempotencyNonIdempotent, "non-idempotent"},
		{AxisIdempotencyRecoverableNonIdempotent, "recoverable-non-idempotent"},
		{AxisIdempotencyNA, "n/a"},
	}
	for _, tc := range cases {
		got, err := tc.value.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%q) error: %v", tc.value, err)
		}
		if string(got) != tc.want {
			t.Errorf("MarshalText(%q) = %q, want %q", tc.value, got, tc.want)
		}
	}

	if _, err := AxisIdempotency("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}
}

func TestAxisIdempotencyUnmarshalText(t *testing.T) {
	t.Parallel()

	type axisFixtureIdempotencyWrapper struct {
		A AxisIdempotency `json:"idempotency"`
	}

	tests := []struct {
		name    string
		input   string
		want    AxisIdempotency
		wantErr bool
	}{
		{name: "idempotent", input: `{"idempotency":"idempotent"}`, want: AxisIdempotencyIdempotent},
		{name: "non-idempotent", input: `{"idempotency":"non-idempotent"}`, want: AxisIdempotencyNonIdempotent},
		{
			name:  "recoverable-non-idempotent",
			input: `{"idempotency":"recoverable-non-idempotent"}`,
			want:  AxisIdempotencyRecoverableNonIdempotent,
		},
		{name: "n/a", input: `{"idempotency":"n/a"}`, want: AxisIdempotencyNA},
		{name: "underscore non_idempotent", input: `{"idempotency":"non_idempotent"}`, wantErr: true},
		{name: "uppercase", input: `{"idempotency":"IDEMPOTENT"}`, wantErr: true},
		{name: "empty", input: `{"idempotency":""}`, wantErr: true},
		{name: "na without slash", input: `{"idempotency":"na"}`, wantErr: true},
		{name: "unknown", input: `{"idempotency":"retry"}`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w axisFixtureIdempotencyWrapper
			err := json.Unmarshal([]byte(tc.input), &w)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for input %q, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error for input %q: %v", tc.input, err)
				return
			}
			if w.A != tc.want {
				t.Errorf("got %q, want %q", w.A, tc.want)
			}
		})
	}
}

// --- AxisTags struct ---

func TestAxisTagsBaseline(t *testing.T) {
	t.Parallel()

	got := BaselineAxisTags
	if got.LLMFreedom != LLMFreedomNone {
		t.Errorf("BaselineAxisTags.LLMFreedom = %q, want %q", got.LLMFreedom, LLMFreedomNone)
	}
	if got.IODeterminism != IODeterminismDeterministic {
		t.Errorf("BaselineAxisTags.IODeterminism = %q, want %q", got.IODeterminism, IODeterminismDeterministic)
	}
	if got.ReplaySafety != ReplaySafetySafe {
		t.Errorf("BaselineAxisTags.ReplaySafety = %q, want %q", got.ReplaySafety, ReplaySafetySafe)
	}
	if got.Idempotency != AxisIdempotencyIdempotent {
		t.Errorf("BaselineAxisTags.Idempotency = %q, want %q", got.Idempotency, AxisIdempotencyIdempotent)
	}
	if !got.Valid() {
		t.Error("BaselineAxisTags.Valid() = false, want true")
	}
}

func TestAxisTagsValid(t *testing.T) {
	t.Parallel()

	validCases := []AxisTags{
		BaselineAxisTags,
		{
			LLMFreedom:    LLMFreedomBounded,
			IODeterminism: IODeterminismBestEffort,
			ReplaySafety:  ReplaySafetyUnsafe,
			Idempotency:   AxisIdempotencyNonIdempotent,
		},
		{
			LLMFreedom:    LLMFreedomUnbounded,
			IODeterminism: IODeterminismNondeterministic,
			ReplaySafety:  ReplaySafetyNA,
			Idempotency:   AxisIdempotencyNA,
		},
		{
			LLMFreedom:    LLMFreedomNone,
			IODeterminism: IODeterminismDeterministic,
			ReplaySafety:  ReplaySafetySafe,
			Idempotency:   AxisIdempotencyRecoverableNonIdempotent,
		},
	}
	for _, tags := range validCases {
		if !tags.Valid() {
			t.Errorf("expected Valid() = true for %+v", tags)
		}
	}

	invalidCases := []AxisTags{
		{LLMFreedom: "bad", IODeterminism: IODeterminismDeterministic, ReplaySafety: ReplaySafetySafe, Idempotency: AxisIdempotencyIdempotent},
		{LLMFreedom: LLMFreedomNone, IODeterminism: "bad", ReplaySafety: ReplaySafetySafe, Idempotency: AxisIdempotencyIdempotent},
		{LLMFreedom: LLMFreedomNone, IODeterminism: IODeterminismDeterministic, ReplaySafety: "bad", Idempotency: AxisIdempotencyIdempotent},
		{LLMFreedom: LLMFreedomNone, IODeterminism: IODeterminismDeterministic, ReplaySafety: ReplaySafetySafe, Idempotency: "bad"},
		{},
	}
	for _, tags := range invalidCases {
		if tags.Valid() {
			t.Errorf("expected Valid() = false for %+v", tags)
		}
	}
}

func TestAxisTagsMarshalJSON(t *testing.T) {
	t.Parallel()

	got, err := json.Marshal(BaselineAxisTags)
	if err != nil {
		t.Fatalf("json.Marshal(BaselineAxisTags) error: %v", err)
	}

	// Unmarshal back and compare to verify round-trip.
	var rt AxisTags
	if err := json.Unmarshal(got, &rt); err != nil {
		t.Fatalf("round-trip Unmarshal error: %v", err)
	}
	if rt != BaselineAxisTags {
		t.Errorf("round-trip mismatch: got %+v, want %+v", rt, BaselineAxisTags)
	}

	// Confirm invalid AxisTags are rejected.
	invalid := AxisTags{LLMFreedom: "bogus", IODeterminism: IODeterminismDeterministic, ReplaySafety: ReplaySafetySafe, Idempotency: AxisIdempotencyIdempotent}
	if _, err := json.Marshal(invalid); err == nil {
		t.Error("json.Marshal accepted AxisTags with invalid LLMFreedom")
	}
}

func TestAxisTagsUnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    AxisTags
		wantErr bool
	}{
		{
			name: "baseline tuple",
			input: `{
				"llm_freedom":"none",
				"io_determinism":"deterministic",
				"replay_safety":"safe",
				"idempotency":"idempotent"
			}`,
			want: BaselineAxisTags,
		},
		{
			name: "unbounded organ operation",
			input: `{
				"llm_freedom":"unbounded",
				"io_determinism":"nondeterministic",
				"replay_safety":"unsafe",
				"idempotency":"non-idempotent"
			}`,
			want: AxisTags{
				LLMFreedom:    LLMFreedomUnbounded,
				IODeterminism: IODeterminismNondeterministic,
				ReplaySafety:  ReplaySafetyUnsafe,
				Idempotency:   AxisIdempotencyNonIdempotent,
			},
		},
		{
			name: "n/a axes",
			input: `{
				"llm_freedom":"bounded",
				"io_determinism":"best-effort",
				"replay_safety":"n/a",
				"idempotency":"n/a"
			}`,
			want: AxisTags{
				LLMFreedom:    LLMFreedomBounded,
				IODeterminism: IODeterminismBestEffort,
				ReplaySafety:  ReplaySafetyNA,
				Idempotency:   AxisIdempotencyNA,
			},
		},
		{
			name:    "unknown llm_freedom",
			input:   `{"llm_freedom":"free","io_determinism":"deterministic","replay_safety":"safe","idempotency":"idempotent"}`,
			wantErr: true,
		},
		{
			name:    "unknown io_determinism",
			input:   `{"llm_freedom":"none","io_determinism":"random","replay_safety":"safe","idempotency":"idempotent"}`,
			wantErr: true,
		},
		{
			name:    "unknown replay_safety",
			input:   `{"llm_freedom":"none","io_determinism":"deterministic","replay_safety":"yes","idempotency":"idempotent"}`,
			wantErr: true,
		},
		{
			name:    "unknown idempotency",
			input:   `{"llm_freedom":"none","io_determinism":"deterministic","replay_safety":"safe","idempotency":"retry"}`,
			wantErr: true,
		},
		{
			name:    "underscore separator best_effort rejected",
			input:   `{"llm_freedom":"none","io_determinism":"best_effort","replay_safety":"safe","idempotency":"idempotent"}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got AxisTags
			err := json.Unmarshal([]byte(tc.input), &got)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for input %q, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error for input %q: %v", tc.input, err)
				return
			}
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
