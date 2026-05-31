package daemon_test

// queueledger_bridge_hkdv8qv_test.go — regression coverage for hk-dv8qv.
//
// hk-dv8qv: the daemon's brQueueLedger.BlocksEdge had the Beads dependency
// edge direction REVERSED. A "blocks" dependency is stored in Beads with
// issue_id = the BLOCKED (dependent) bead and depends_on_id = the BLOCKER bead
// (verified against live `br dep list` and brcli/listdependencies_test.go). The
// queue.BeadLedger contract is BlocksEdge(blocker, blocked) == true iff blocker
// must complete before blocked may start (i.e. blocked DEPENDS ON blocker).
//
// The prior impl listed dependencies of blocker and matched
// FromBeadID==blocker / ToBeadID==blocked, making BlocksEdge(a,b) true when a
// DEPENDS ON b — exactly inverted. That deferred chain roots while dispatching
// leaves with open blockers out of order.
//
// These tests pin the direction at the production-bridge boundary (a real
// brcli.Adapter over a mock `br` binary) so a future re-inversion fails CI.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// hkdv8qvMockBr writes a mock `br` shell script that answers the two read
// commands brQueueLedger issues:
//
//   - `br dep list <id> --direction both --format json` → the dep-list JSON for
//     <id> looked up in depListByID (default: empty array).
//   - `br show <id> --format json` → the show JSON for <id> looked up in
//     showByID (default: ISSUE_NOT_FOUND envelope, exit 1).
//
// The script dispatches on $1 (the subcommand) and $2 (the bead ID). Chains are
// modeled the way Beads actually stores them: an edge "X depends on Y" appears
// in `br dep list X` as {"issue_id":"X","depends_on_id":"Y","type":"blocks"}.
func hkdv8qvMockBr(t *testing.T, depListByID, showByID map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")

	var b []byte
	b = append(b, []byte("#!/bin/sh\n")...)
	// Arg layout (brcli.runFormatJSON appends --format json):
	//   br dep list <id> --direction both --format json   → $1=dep $2=list $3=<id>
	//   br show <id> --format json                         → $1=show $2=<id>
	b = append(b, []byte("subcmd=\"$1\"\n")...)
	b = append(b, []byte("case \"$subcmd\" in\n")...)

	// dep list branch (bead ID is $3).
	b = append(b, []byte("  dep)\n    case \"$3\" in\n")...)
	for id, js := range depListByID {
		b = append(b, []byte(fmt.Sprintf("      %s) printf '%%s' %q ; exit 0 ;;\n", id, js))...)
	}
	b = append(b, []byte("      *) printf '%s' '[]' ; exit 0 ;;\n    esac ;;\n")...)

	// show branch (bead ID is $2).
	b = append(b, []byte("  show)\n    case \"$2\" in\n")...)
	for id, js := range showByID {
		b = append(b, []byte(fmt.Sprintf("      %s) printf '%%s' %q ; exit 0 ;;\n", id, js))...)
	}
	b = append(b, []byte("      *) printf '%s' '{\"error\":{\"code\":\"ISSUE_NOT_FOUND\",\"message\":\"not found\"}}' ; exit 1 ;;\n    esac ;;\n")...)

	b = append(b, []byte("  *) printf '%s' '[]' ; exit 0 ;;\nesac\n")...)

	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(path, b, 0o755); err != nil {
		t.Fatalf("hkdv8qvMockBr: write mock: %v", err)
	}
	return path
}

// depListBlocks renders the Beads `br dep list` JSON for a single bead `id`
// that depends-on each blocker in blockers (a "blocks" edge each, stored as
// issue_id=id, depends_on_id=blocker).
func depListBlocks(id string, blockers ...string) string {
	out := "["
	for i, blk := range blockers {
		if i > 0 {
			out += ","
		}
		out += fmt.Sprintf(
			`{"issue_id":%q,"depends_on_id":%q,"type":"blocks","title":"t","status":"open","priority":2}`,
			id, blk)
	}
	return out + "]"
}

// TestBlocksEdge_Direction_hkdv8qv is the core regression: with the chain
// R ← A ← B (A depends on R; B depends on A), BlocksEdge must report the
// blocker→blocked relationship in the contract direction, and NOT the inverse.
func TestBlocksEdge_Direction_hkdv8qv(t *testing.T) {
	const (
		R = "hk-tigaf.1"  // root: depends on nothing
		A = "hk-tigaf.2"  // depends on R
		B = "hk-tigaf.10" // depends on A
	)
	depList := map[string]string{
		R: depListBlocks(R),    // no blockers
		A: depListBlocks(A, R), // A depends on R
		B: depListBlocks(B, A), // B depends on A
	}
	mock := hkdv8qvMockBr(t, depList, nil)
	adapter, err := brcli.New(mock)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}
	ledger := daemon.ExportedNewBRQueueLedger(adapter)
	ctx := context.Background()

	// Contract: BlocksEdge(blocker, blocked) == true iff blocked depends on blocker.
	cases := []struct {
		name             string
		blocker, blocked core.BeadID
		want             bool
	}{
		// True direction: the dependency parent (blocker) must finish first.
		{"R blocks A (A depends on R)", R, A, true},
		{"A blocks B (B depends on A)", A, B, true},
		// Inverse direction must be FALSE — this is exactly what regressed.
		{"A does NOT block R", A, R, false},
		{"B does NOT block A", B, A, false},
		// Unrelated / root has no blockers.
		{"R is not blocked by B", B, R, false},
		{"A is not blocked by B", B, A, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ledger.BlocksEdge(ctx, tc.blocker, tc.blocked)
			if err != nil {
				t.Fatalf("BlocksEdge(%q,%q): unexpected error: %v", tc.blocker, tc.blocked, err)
			}
			if got != tc.want {
				t.Errorf("BlocksEdge(blocker=%q, blocked=%q) = %v; want %v",
					tc.blocker, tc.blocked, got, tc.want)
			}
		})
	}
}

// TestBlocksEdge_RootHasNoBlocker_hkdv8qv pins the exact ground-truth symptom:
// the root of a chain (depends on nothing) must NOT be reported as blocked by
// anything, so it stays eligible rather than deferred.
func TestBlocksEdge_RootHasNoBlocker_hkdv8qv(t *testing.T) {
	const (
		root = "hk-tigaf.1"
		leaf = "hk-tigaf.10"
	)
	depList := map[string]string{
		root: depListBlocks(root),       // ZERO blocking deps
		leaf: depListBlocks(leaf, root), // leaf depends on root
	}
	mock := hkdv8qvMockBr(t, depList, nil)
	adapter, err := brcli.New(mock)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}
	ledger := daemon.ExportedNewBRQueueLedger(adapter)
	ctx := context.Background()

	// No in-queue bead should be reported as a blocker of the root.
	for _, blocker := range []core.BeadID{root, leaf} {
		got, err := ledger.BlocksEdge(ctx, blocker, root)
		if err != nil {
			t.Fatalf("BlocksEdge(%q, root): %v", blocker, err)
		}
		if got {
			t.Errorf("BlocksEdge(blocker=%q, blocked=root) = true; root depends on nothing and must be eligible (hk-dv8qv inversion)", blocker)
		}
	}

	// The leaf, conversely, IS blocked by the root.
	got, err := ledger.BlocksEdge(ctx, root, leaf)
	if err != nil {
		t.Fatalf("BlocksEdge(root, leaf): %v", err)
	}
	if !got {
		t.Error("BlocksEdge(blocker=root, blocked=leaf) = false; leaf depends on root and must be deferred until root closes")
	}
}
