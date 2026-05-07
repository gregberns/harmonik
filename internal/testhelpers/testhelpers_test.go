package testhelpers_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/testhelpers"
)

// ---------------------------------------------------------------------------
// assert.go — test the success paths (failure paths call FailNow/Goexit which
// cannot be exercised safely from an external test package without a real sub-T).
// ---------------------------------------------------------------------------

func TestAssertNoError_Nil(t *testing.T) {
	testhelpers.AssertNoError(t, nil)
}

func TestAssertEqual_Match(t *testing.T) {
	testhelpers.AssertEqual(t, 42, 42)
	testhelpers.AssertEqual(t, "hello", "hello")
}

func TestAssertTrue_True(t *testing.T) {
	testhelpers.AssertTrue(t, true, "should not fail")
}

// ---------------------------------------------------------------------------
// clock.go
// ---------------------------------------------------------------------------

func TestFakeClock_Now(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := testhelpers.NewFakeClock(epoch)

	if got := clk.Now(); !got.Equal(epoch) {
		t.Errorf("Now() = %v, want %v", got, epoch)
	}
}

func TestFakeClock_Advance(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := testhelpers.NewFakeClock(epoch)

	clk.Advance(5 * time.Minute)
	want := epoch.Add(5 * time.Minute)

	if got := clk.Now(); !got.Equal(want) {
		t.Errorf("After Advance: Now() = %v, want %v", got, want)
	}
}

func TestFakeClock_AdvanceNegativePanics(t *testing.T) {
	clk := testhelpers.NewFakeClock(time.Now())

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic from negative Advance, got none")
		}
	}()

	clk.Advance(-time.Second)
}

func TestFakeClock_ImplementsClock(t *testing.T) {
	// Compile-time assertion: *FakeClock satisfies the Clock interface.
	var _ testhelpers.Clock = testhelpers.NewFakeClock(time.Now())
}

// ---------------------------------------------------------------------------
// tempdir.go
// ---------------------------------------------------------------------------

func TestTempDir_CreatesDirectory(t *testing.T) {
	dir := testhelpers.TempDir(t)

	if dir == "" {
		t.Fatal("TempDir returned empty string")
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat(%q): %v", dir, err)
	}

	if !info.IsDir() {
		t.Errorf("TempDir result %q is not a directory", dir)
	}
}

// ---------------------------------------------------------------------------
// testenv.go
// ---------------------------------------------------------------------------

func TestNewEnv_CreatesLayout(t *testing.T) {
	env := testhelpers.NewEnv(t)

	if env.Root == "" {
		t.Fatal("Env.Root is empty")
	}

	if env.Harmonik == "" {
		t.Fatal("Env.Harmonik is empty")
	}

	expectedDirs := []string{
		".harmonik",
		".harmonik/events",
		".harmonik/beads-intents",
		".harmonik/reconciliation-locks",
		".harmonik/worktrees",
	}

	for _, rel := range expectedDirs {
		path := filepath.Join(env.Root, rel)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected directory %q to exist: %v", path, err)
			continue
		}

		if !info.IsDir() {
			t.Errorf("%q is not a directory", path)
		}
	}
}

func TestEnv_HarmonikPath(t *testing.T) {
	env := testhelpers.NewEnv(t)

	got := env.HarmonikPath("daemon.pid")
	want := filepath.Join(env.Harmonik, "daemon.pid")

	if got != want {
		t.Errorf("HarmonikPath() = %q, want %q", got, want)
	}
}
