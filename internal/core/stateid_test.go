package core

import (
	"testing"

	"github.com/google/uuid"
)

func TestStateID_String(t *testing.T) {
	u := uuid.MustParse("018f4d8a-7f6e-7000-8000-000000000001")
	id := StateID(u)
	if got, want := id.String(), u.String(); got != want {
		t.Fatalf("StateID.String() = %q, want %q", got, want)
	}
}

func TestStateID_MarshalText(t *testing.T) {
	u := uuid.MustParse("018f4d8a-7f6e-7000-8000-000000000002")
	id := StateID(u)
	got, err := id.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != u.String() {
		t.Fatalf("MarshalText = %q, want %q", string(got), u.String())
	}
}

func TestStateID_UnmarshalText(t *testing.T) {
	want := uuid.MustParse("018f4d8a-7f6e-7000-8000-000000000003")
	var id StateID
	if err := id.UnmarshalText([]byte(want.String())); err != nil {
		t.Fatalf("UnmarshalText error: %v", err)
	}
	if uuid.UUID(id) != want {
		t.Fatalf("UnmarshalText produced %v, want %v", uuid.UUID(id), want)
	}

	if err := id.UnmarshalText([]byte("not-a-uuid")); err == nil {
		t.Fatal("UnmarshalText accepted invalid input")
	}
}
