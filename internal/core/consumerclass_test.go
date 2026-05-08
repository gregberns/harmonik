package core

import (
	"encoding/json"
	"testing"
)

func TestConsumerClassValid(t *testing.T) {
	t.Parallel()

	valid := []ConsumerClass{
		ConsumerClassSynchronous,
		ConsumerClassAsynchronous,
		ConsumerClassObserver,
	}
	for _, c := range valid {
		if !c.Valid() {
			t.Errorf("expected %q to be valid", c)
		}
	}

	invalid := []ConsumerClass{
		"",
		"SYNCHRONOUS",
		"Synchronous",
		"ASYNCHRONOUS",
		"OBSERVER",
		"sync",
		"async",
		"unknown",
	}
	for _, c := range invalid {
		if c.Valid() {
			t.Errorf("expected %q to be invalid", c)
		}
	}
}

func TestConsumerClassMarshalText(t *testing.T) {
	t.Parallel()

	cases := []struct {
		val  ConsumerClass
		want string
	}{
		{ConsumerClassSynchronous, "synchronous"},
		{ConsumerClassAsynchronous, "asynchronous"},
		{ConsumerClassObserver, "observer"},
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

	if _, err := ConsumerClass("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}
	if _, err := ConsumerClass("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

func TestConsumerClassUnmarshalText(t *testing.T) {
	t.Parallel()

	// hqwn65FixtureWrapper is a test-local wrapper for table-driven JSON round-trip tests.
	type hqwn65FixtureWrapper struct {
		Class ConsumerClass `json:"consumer_class"`
	}

	tests := []struct {
		name    string
		input   string
		want    ConsumerClass
		wantErr bool
	}{
		{
			name:  "synchronous",
			input: `{"consumer_class":"synchronous"}`,
			want:  ConsumerClassSynchronous,
		},
		{
			name:  "asynchronous",
			input: `{"consumer_class":"asynchronous"}`,
			want:  ConsumerClassAsynchronous,
		},
		{
			name:  "observer",
			input: `{"consumer_class":"observer"}`,
			want:  ConsumerClassObserver,
		},
		{
			name:    "unknown value rejected",
			input:   `{"consumer_class":"unknown"}`,
			wantErr: true,
		},
		{
			name:    "empty string rejected",
			input:   `{"consumer_class":""}`,
			wantErr: true,
		},
		{
			name:    "uppercase SYNCHRONOUS rejected",
			input:   `{"consumer_class":"SYNCHRONOUS"}`,
			wantErr: true,
		},
		{
			name:    "partial match sync rejected",
			input:   `{"consumer_class":"sync"}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w hqwn65FixtureWrapper
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
			if w.Class != tc.want {
				t.Errorf("got %q, want %q", string(w.Class), string(tc.want))
			}
		})
	}
}
