package core

import (
	"encoding/json"
	"testing"
)

func TestOnPanicValid(t *testing.T) {
	t.Parallel()

	valid := []OnPanic{
		OnPanicRecoverAndLog,
		OnPanicQuarantineConsumer,
		OnPanicFailDaemon,
	}
	for _, p := range valid {
		if !p.Valid() {
			t.Errorf("expected %q to be valid", p)
		}
	}

	invalid := []OnPanic{
		"",
		"RECOVER_AND_LOG",
		"RecoverAndLog",
		"QUARANTINE_CONSUMER",
		"FAIL_DAEMON",
		"recover",
		"quarantine",
		"unknown",
	}
	for _, p := range invalid {
		if p.Valid() {
			t.Errorf("expected %q to be invalid", p)
		}
	}
}

func TestOnPanicMarshalText(t *testing.T) {
	t.Parallel()

	cases := []struct {
		val  OnPanic
		want string
	}{
		{OnPanicRecoverAndLog, "recover_and_log"},
		{OnPanicQuarantineConsumer, "quarantine_consumer"},
		{OnPanicFailDaemon, "fail_daemon"},
	}
	for _, tc := range cases {
		got, err := tc.val.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%q) error: %v", tc.val, err)
		}
		if string(got) != tc.want {
			t.Errorf("MarshalText(%q) = %q, want %q", tc.val, string(got), tc.want)
		}
	}

	if _, err := OnPanic("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}
	if _, err := OnPanic("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

func TestOnPanicUnmarshalText(t *testing.T) {
	t.Parallel()

	// hqwn66FixtureWrapper is a test-local wrapper for table-driven JSON round-trip tests.
	type hqwn66FixtureWrapper struct {
		Policy OnPanic `json:"on_panic"`
	}

	tests := []struct {
		name    string
		input   string
		want    OnPanic
		wantErr bool
	}{
		{
			name:  "recover_and_log",
			input: `{"on_panic":"recover_and_log"}`,
			want:  OnPanicRecoverAndLog,
		},
		{
			name:  "quarantine_consumer",
			input: `{"on_panic":"quarantine_consumer"}`,
			want:  OnPanicQuarantineConsumer,
		},
		{
			name:  "fail_daemon",
			input: `{"on_panic":"fail_daemon"}`,
			want:  OnPanicFailDaemon,
		},
		{
			name:    "unknown value rejected",
			input:   `{"on_panic":"unknown"}`,
			wantErr: true,
		},
		{
			name:    "empty string rejected",
			input:   `{"on_panic":""}`,
			wantErr: true,
		},
		{
			name:    "uppercase RECOVER_AND_LOG rejected",
			input:   `{"on_panic":"RECOVER_AND_LOG"}`,
			wantErr: true,
		},
		{
			name:    "partial match recover rejected",
			input:   `{"on_panic":"recover"}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w hqwn66FixtureWrapper
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
			if w.Policy != tc.want {
				t.Errorf("got %q, want %q", string(w.Policy), string(tc.want))
			}
		})
	}
}
