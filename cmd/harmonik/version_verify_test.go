package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// version_verify_test.go — the meaning of `harmonik version --binary` is locked
// down here.
//
// WHY THIS FILE EXISTS. Before hk-gate-clean-but-binary-dirty-7gwil this command
// had no tests at all, while being the single authoritative answer to "which
// commit is this binary". That bead was filed because the command returned exit
// 3 for EVERY binary the documented assessor sequence produced, so it could
// never return 0 and therefore verified nothing.
//
// The real fault was in the build harness (scripts/scratch-daemon.sh built after
// `harmonik init` had rewritten tracked files, so Go stamped vcs.modified=true),
// and scripts/scratch-daemon-provenance-test.sh is where that end-to-end claim is
// proved. The tests here guard the OTHER direction: the obvious way to make a
// failing provenance check "pass" is to stop letting it fail. A check that always
// returns 0 is the same defect wearing the opposite sign, and it would be
// invisible in every green gate afterwards.
//
// So: vcs.modified=true must keep producing contains-dirty and exit 3, exit 0
// must stay reachable only from a clean ancestor build, and an unreadable
// provenance must stay distinguishable from a decided verdict.

// TestContainmentVerdict_DirtyIsNeverShipSafe pins the ancestry x cleanliness
// table. The dirty-ancestor row is the load-bearing one: it is the row a
// well-meaning "make the gate pass" change would flip to exit 0.
func TestContainmentVerdict_DirtyIsNeverShipSafe(t *testing.T) {
	const (
		target = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		rev    = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	cases := []struct {
		name       string
		modified   bool
		isAncestor bool
		wantStatus string
		wantExit   int
	}{
		{
			name:       "clean ancestor is the only ship-safe answer",
			modified:   false,
			isAncestor: true,
			wantStatus: verifyStatusContains,
			wantExit:   verifyExitOK,
		},
		{
			name:       "dirty ancestor is refused, not waved through",
			modified:   true,
			isAncestor: true,
			wantStatus: verifyStatusContainsDirty,
			wantExit:   verifyExitDirty,
		},
		{
			name:       "clean non-ancestor is missing",
			modified:   false,
			isAncestor: false,
			wantStatus: verifyStatusMissing,
			wantExit:   verifyExitMissing,
		},
		{
			name:       "dirty non-ancestor is still missing",
			modified:   true,
			isAncestor: false,
			wantStatus: verifyStatusMissing,
			wantExit:   verifyExitMissing,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stamp := binaryStamp{Revision: rev, Modified: tc.modified}
			got := containmentVerdict(stamp, target, tc.isAncestor, verifyResult{})
			if got.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", got.Status, tc.wantStatus)
			}
			if got.ExitCode != tc.wantExit {
				t.Errorf("exit = %d, want %d", got.ExitCode, tc.wantExit)
			}
			if got.Detail == "" {
				t.Error("detail is empty; every refusal must say why")
			}
		})
	}
}

// TestContainmentVerdict_ExitZeroImpliesCleanContains states the invariant the
// deploy gate leans on from the other side: exit 0 out of a --contains query
// means clean AND ancestor, and nothing else can reach it.
func TestContainmentVerdict_ExitZeroImpliesCleanContains(t *testing.T) {
	const rev = "cccccccccccccccccccccccccccccccccccccccc"
	for _, modified := range []bool{false, true} {
		for _, isAncestor := range []bool{false, true} {
			stamp := binaryStamp{Revision: rev, Modified: modified}
			got := containmentVerdict(stamp, rev, isAncestor, verifyResult{})
			if got.ExitCode != verifyExitOK {
				continue
			}
			if modified || !isAncestor {
				t.Errorf("exit 0 reached with modified=%t isAncestor=%t (status %q); "+
					"exit 0 must mean a clean build that contains the commit",
					modified, isAncestor, got.Status)
			}
			if got.Status != verifyStatusContains {
				t.Errorf("exit 0 carried status %q, want %q", got.Status, verifyStatusContains)
			}
		}
	}
}

// TestClassifyStamp_NoRevisionIsIndeterminate keeps "cannot answer" separate
// from "answered no". A binary built with -buildvcs=false has no provenance to
// read, and reporting that as containment either way would be a guess.
func TestClassifyStamp_NoRevisionIsIndeterminate(t *testing.T) {
	res, err := classifyStamp(context.Background(), binaryStamp{}, t.TempDir(), "deadbeef")
	if err != nil {
		t.Fatalf("classifyStamp: %v", err)
	}
	if res.Status != verifyStatusNoVCSStamp {
		t.Errorf("status = %q, want %q", res.Status, verifyStatusNoVCSStamp)
	}
	if res.ExitCode != verifyExitIndeterminate {
		t.Errorf("exit = %d, want %d", res.ExitCode, verifyExitIndeterminate)
	}
}

// TestClassifyStamp_NoTargetReportsCleanliness covers the informational form
// scripts/scratch-daemon.sh `status` mirrors: with no --contains the command
// still has to say whether the tree was clean, because that is the fact the two
// tools were measuring differently.
func TestClassifyStamp_NoTargetReportsCleanliness(t *testing.T) {
	const rev = "dddddddddddddddddddddddddddddddddddddddd"
	for _, tc := range []struct {
		modified   bool
		wantSubstr string
	}{
		{modified: false, wantSubstr: "vcs.modified=false"},
		{modified: true, wantSubstr: "vcs.modified=true"},
	} {
		res, err := classifyStamp(context.Background(),
			binaryStamp{Revision: rev, Modified: tc.modified}, t.TempDir(), "")
		if err != nil {
			t.Fatalf("classifyStamp(modified=%t): %v", tc.modified, err)
		}
		if res.Status != verifyStatusRevision {
			t.Errorf("modified=%t: status = %q, want %q", tc.modified, res.Status, verifyStatusRevision)
		}
		if res.ExitCode != verifyExitOK {
			t.Errorf("modified=%t: exit = %d, want %d", tc.modified, res.ExitCode, verifyExitOK)
		}
		if !strings.Contains(res.Detail, tc.wantSubstr) {
			t.Errorf("modified=%t: detail %q does not state %q", tc.modified, res.Detail, tc.wantSubstr)
		}
	}
}

// TestReadBinaryStamp_DistinguishesMissingFromNotAGoBinary keeps an operator
// mistake (wrong path) apart from a verdict (this file has no build info). The
// command reports the first as exit 2 and the second as exit 4, and it can only
// do that if the read layer tells them apart.
func TestReadBinaryStamp_DistinguishesMissingFromNotAGoBinary(t *testing.T) {
	dir := t.TempDir()

	_, err := readBinaryStamp(filepath.Join(dir, "no-such-file"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing file: err = %v, want an fs.ErrNotExist", err)
	}

	notGo := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(notGo, []byte("this is not a Go binary\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := readBinaryStamp(notGo); !errors.Is(err, errNoBuildInfo) {
		t.Errorf("non-Go file: err = %v, want errNoBuildInfo", err)
	}
}

// TestInspectBinary_NoBuildInfoIsAVerdictNotAnError checks the seam above:
// a file with no Go build info must come back as a rendered verdict with exit 4,
// not as an error the caller reports as a bad command line.
func TestInspectBinary_NoBuildInfoIsAVerdictNotAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("not a Go binary\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	res, err := inspectBinary(context.Background(), versionVerifyFlags{binary: path, repo: t.TempDir()})
	if err != nil {
		t.Fatalf("inspectBinary: %v", err)
	}
	if res.Status != verifyStatusNoBuildInfo {
		t.Errorf("status = %q, want %q", res.Status, verifyStatusNoBuildInfo)
	}
	if res.ExitCode != verifyExitIndeterminate {
		t.Errorf("exit = %d, want %d", res.ExitCode, verifyExitIndeterminate)
	}
}
