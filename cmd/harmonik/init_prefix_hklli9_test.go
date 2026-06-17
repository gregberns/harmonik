package main

// init_prefix_hklli9_test.go — unit tests for deriveBeadPrefix (hk-lli9).
//
// Verifies that the default bead prefix is derived from the project directory
// name rather than hardcoded to "hk", so that multi-project setups on one
// machine do not collide.
//
// Bead ref: hk-lli9.

import (
	"testing"
)

func TestDeriveBeadPrefix(t *testing.T) {
	cases := []struct {
		dir  string
		want string
	}{
		// Multi-word names → initials (up to 4).
		{"/home/user/my-project", "mp"},
		{"/home/user/my-great-project", "mgp"},
		{"/home/user/a-b-c-d-e", "abcd"},
		{"/home/user/great_app", "ga"},
		{"/home/user/fleet portability", "fp"},
		// Single word → first 2 chars.
		{"/home/user/harmonik", "ha"},
		{"/home/user/kerf", "ke"},
		{"/home/user/ab", "ab"},
		// Very short names.
		{"/home/user/a", "ax"},
		// Numbers are valid alphanumeric.
		{"/home/user/proj2", "pr"},
		{"/home/user/p2p", "p2"},
		// Dots as word separators.
		{"/home/user/my.project", "mp"},
		// Uppercase input is lowercased before processing.
		{"/home/user/MyProject", "my"}, // no word boundary → single-word path → first 2 chars
		{"/home/user/HARMONIK", "ha"},
		{"/home/user/My-Project", "mp"}, // hyphen is a word boundary → initials
	}

	for _, tc := range cases {
		got := deriveBeadPrefix(tc.dir)
		if got != tc.want {
			t.Errorf("deriveBeadPrefix(%q) = %q, want %q", tc.dir, got, tc.want)
		}
	}
}

func TestDeriveBeadPrefix_FallbackOnEmptyName(t *testing.T) {
	// A directory path whose base name contains only non-alphanumeric characters
	// should fall back to "hk".
	got := deriveBeadPrefix("/home/user/---")
	if got != "hk" {
		t.Errorf("deriveBeadPrefix(%q) = %q, want %q (fallback)", "/home/user/---", got, "hk")
	}
}

func TestDeriveBeadPrefix_AtMostFourChars(t *testing.T) {
	// Result must never exceed 4 characters regardless of word count.
	got := deriveBeadPrefix("/home/user/a-b-c-d-e-f-g-h")
	if len(got) > 4 {
		t.Errorf("deriveBeadPrefix returned %q (len %d), want at most 4 chars", got, len(got))
	}
}
