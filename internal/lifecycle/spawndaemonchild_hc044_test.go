package lifecycle

import (
	"syscall"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// i3151PGIDValue is the canonical test PGID used by hk-8i31.51 test helpers.
const i3151PGIDValue = core.PGID(42000)

// TestHC044_SpawnChildSysProcAttr_SetpgidTrue verifies that SpawnChildSysProcAttr
// always sets Setpgid=true, which places the subprocess into the daemon's process
// group on both Linux and macOS.
//
// Spec ref: specs/handler-contract.md §4.10.HC-044 — "The daemon MUST spawn
// every handler subprocess as a direct child process."
func TestHC044_SpawnChildSysProcAttr_SetpgidTrue(t *testing.T) {
	t.Parallel()

	attr := SpawnChildSysProcAttr(i3151PGIDValue)

	if attr == nil {
		t.Fatal("HC-044: SpawnChildSysProcAttr returned nil")
	}
	if !attr.Setpgid {
		t.Error("HC-044: SpawnChildSysProcAttr: Setpgid = false, want true")
	}
}

// TestHC044_SpawnChildSysProcAttr_PgidMatches verifies that SpawnChildSysProcAttr
// records the caller-supplied PGID in the returned SysProcAttr.
//
// Spec ref: specs/handler-contract.md §4.10.HC-044; process-lifecycle.md §4.2
// PL-006a — "SysProcAttr{Setpgid: true, Pgid: <recorded_pgid>}".
func TestHC044_SpawnChildSysProcAttr_PgidMatches(t *testing.T) {
	t.Parallel()

	wantPGID := core.PGID(99991)
	attr := SpawnChildSysProcAttr(wantPGID)

	if attr == nil {
		t.Fatal("HC-044: SpawnChildSysProcAttr returned nil")
	}
	if attr.Pgid != wantPGID.Int() {
		t.Errorf("HC-044: SpawnChildSysProcAttr: Pgid = %d, want %d", attr.Pgid, wantPGID.Int())
	}
}

// TestHC044_SpawnChildSysProcAttr_ZeroPGIDAllowed verifies that PGID(0) is
// accepted and the SysProcAttr is still non-nil with Setpgid=true. When Pgid==0,
// the kernel assigns the child's own PID as the PGID, which is a valid use case
// during daemon initialisation before the PGID is established.
//
// Spec ref: specs/handler-contract.md §4.10.HC-044.
func TestHC044_SpawnChildSysProcAttr_ZeroPGIDAllowed(t *testing.T) {
	t.Parallel()

	attr := SpawnChildSysProcAttr(core.PGID(0))

	if attr == nil {
		t.Fatal("HC-044: SpawnChildSysProcAttr(0) returned nil")
	}
	if !attr.Setpgid {
		t.Error("HC-044: SpawnChildSysProcAttr(0): Setpgid = false, want true")
	}
	if attr.Pgid != 0 {
		t.Errorf("HC-044: SpawnChildSysProcAttr(0): Pgid = %d, want 0", attr.Pgid)
	}
}

// TestHC044_SpawnChildSysProcAttr_DistinctFromSpawnSysProcAttr verifies that
// SpawnChildSysProcAttr and SpawnSysProcAttr (the PL-006a helper) are distinct
// call sites. On Linux the child attr includes Pdeathsig; on darwin they differ
// only in documentation intent.  This test confirms the two functions coexist
// and produce independently correct values.
//
// Spec ref: specs/handler-contract.md §4.10.HC-044; process-lifecycle.md §4.2
// PL-006a.
func TestHC044_SpawnChildSysProcAttr_DistinctFromSpawnSysProcAttr(t *testing.T) {
	t.Parallel()

	pgid := core.PGID(55555)
	childAttr := SpawnChildSysProcAttr(pgid)
	plAttr := SpawnSysProcAttr(pgid)

	if childAttr == nil {
		t.Fatal("HC-044: SpawnChildSysProcAttr returned nil")
	}
	if plAttr == nil {
		t.Fatal("PL-006a: SpawnSysProcAttr returned nil")
	}
	// Both must set Setpgid and Pgid identically.
	if childAttr.Setpgid != plAttr.Setpgid {
		t.Errorf("HC-044 vs PL-006a: Setpgid mismatch: child=%v pl=%v", childAttr.Setpgid, plAttr.Setpgid)
	}
	if childAttr.Pgid != plAttr.Pgid {
		t.Errorf("HC-044 vs PL-006a: Pgid mismatch: child=%d pl=%d", childAttr.Pgid, plAttr.Pgid)
	}
}

// i3151VerifyPdeathsig is called by the platform-specific test file to assert
// the Pdeathsig field value. On Linux it checks syscall.SIGTERM; on darwin the
// field does not exist and the platform file is a no-op.
//
// Helper prefix: i3151 (hk-8i31.51).
func i3151VerifyPdeathsig(t *testing.T, attr *syscall.SysProcAttr) {
	t.Helper()
	i3151PlatformVerifyPdeathsig(t, attr)
}
