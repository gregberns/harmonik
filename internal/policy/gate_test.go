package policy

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

func TestParseGateVerdict(t *testing.T) {
	cases := []struct {
		name    string
		data    string
		want    core.GateAction
		wantErr bool
	}{
		{
			name: "valid allow",
			data: `{"schema_version":1,"decision":"allow","reason":"ok"}`,
			want: core.GateActionAllow,
		},
		{
			name: "valid deny",
			data: `{"schema_version":1,"decision":"deny","reason":"nope"}`,
			want: core.GateActionDeny,
		},
		{
			name: "valid escalate-to-human",
			data: `{"schema_version":1,"decision":"escalate-to-human"}`,
			want: core.GateActionEscalateToHuman,
		},
		{
			name: "reason optional (absent)",
			data: `{"schema_version":1,"decision":"allow"}`,
			want: core.GateActionAllow,
		},
		{
			name:    "bad schema_version",
			data:    `{"schema_version":2,"decision":"allow"}`,
			wantErr: true,
		},
		{
			name:    "missing schema_version defaults to 0 (rejected)",
			data:    `{"decision":"allow"}`,
			wantErr: true,
		},
		{
			name:    "unknown decision",
			data:    `{"schema_version":1,"decision":"maybe"}`,
			wantErr: true,
		},
		{
			name:    "empty decision",
			data:    `{"schema_version":1,"decision":""}`,
			wantErr: true,
		},
		{
			name:    "malformed json",
			data:    `{"schema_version":1,"decision":`,
			wantErr: true,
		},
		{
			name:    "empty bytes",
			data:    ``,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseGateVerdict([]byte(tc.data))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseGateVerdict(%q) = %q, want error", tc.data, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseGateVerdict(%q) unexpected error: %v", tc.data, err)
			}
			if got != tc.want {
				t.Fatalf("ParseGateVerdict(%q) = %q, want %q", tc.data, got, tc.want)
			}
		})
	}
}

func TestMechanismDecision(t *testing.T) {
	if got := MechanismDecision(true); got != core.GateActionAllow {
		t.Fatalf("MechanismDecision(true) = %q, want %q", got, core.GateActionAllow)
	}
	if got := MechanismDecision(false); got != core.GateActionDeny {
		t.Fatalf("MechanismDecision(false) = %q, want %q", got, core.GateActionDeny)
	}
}

func TestGateEvalFailureOutcome(t *testing.T) {
	const reason = "gate_ref \"x\" not found in ControlPoint registry"
	got := GateEvalFailureOutcome(reason)

	if got.Status != core.OutcomeStatusFail {
		t.Errorf("Status = %q, want %q", got.Status, core.OutcomeStatusFail)
	}
	if got.Kind != core.OutcomeKindDefault {
		t.Errorf("Kind = %q, want %q", got.Kind, core.OutcomeKindDefault)
	}
	if got.FailureClass == nil || *got.FailureClass != core.FailureClassStructural {
		t.Errorf("FailureClass = %v, want %q", got.FailureClass, core.FailureClassStructural)
	}
	if want := "gate dispatch: " + reason; got.Notes != want {
		t.Errorf("Notes = %q, want %q", got.Notes, want)
	}
}
