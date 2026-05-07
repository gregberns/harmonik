package core

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// validLeaseLockFile returns a fully-populated LeaseLockFile with all required
// fields set to non-zero values.
func validLeaseLockFile(t *testing.T) LeaseLockFile {
	t.Helper()
	return LeaseLockFile{
		RunID:     RunID(uuid.Must(uuid.NewV7())),
		PID:       12345,
		CreatedAt: time.Now(),
		TTLSec:    300,
	}
}

func TestLeaseLockFileValid_HappyPath(t *testing.T) {
	t.Parallel()

	l := validLeaseLockFile(t)
	if !l.Valid() {
		t.Error("Valid() = false for fully-populated LeaseLockFile, want true")
	}
}

func TestLeaseLockFileValid_ZeroRunID(t *testing.T) {
	t.Parallel()

	l := validLeaseLockFile(t)
	l.RunID = RunID(uuid.Nil)
	if l.Valid() {
		t.Error("Valid() = true with zero RunID, want false")
	}
}

func TestLeaseLockFileValid_ZeroPID(t *testing.T) {
	t.Parallel()

	l := validLeaseLockFile(t)
	l.PID = 0
	if l.Valid() {
		t.Error("Valid() = true with PID=0, want false")
	}
}

func TestLeaseLockFileValid_NegativePID(t *testing.T) {
	t.Parallel()

	l := validLeaseLockFile(t)
	l.PID = -1
	if l.Valid() {
		t.Error("Valid() = true with negative PID, want false")
	}
}

func TestLeaseLockFileValid_ZeroCreatedAt(t *testing.T) {
	t.Parallel()

	l := validLeaseLockFile(t)
	l.CreatedAt = time.Time{}
	if l.Valid() {
		t.Error("Valid() = true with zero CreatedAt, want false")
	}
}

func TestLeaseLockFileValid_ZeroTTLSec(t *testing.T) {
	t.Parallel()

	l := validLeaseLockFile(t)
	l.TTLSec = 0
	if l.Valid() {
		t.Error("Valid() = true with TTLSec=0, want false")
	}
}

func TestLeaseLockFileValid_NegativeTTLSec(t *testing.T) {
	t.Parallel()

	l := validLeaseLockFile(t)
	l.TTLSec = -1
	if l.Valid() {
		t.Error("Valid() = true with negative TTLSec, want false")
	}
}

func TestLeaseLockFileValid_MinimalPositiveValues(t *testing.T) {
	t.Parallel()

	l := LeaseLockFile{
		RunID:     RunID(uuid.Must(uuid.NewV7())),
		PID:       1,
		CreatedAt: time.Unix(1, 0),
		TTLSec:    1,
	}
	if !l.Valid() {
		t.Error("Valid() = false for LeaseLockFile with minimal positive values, want true")
	}
}
