package main

// sync_assets_cmd_test.go — SAFETY-invariant coverage for `harmonik sync-assets`.
//
// The reconcile PLANNER is tested exhaustively in asset_reconcile_test.go; this
// file tests the EXECUTOR's class policy and the never-clobber guarantees:
//
//	(1) --dry-run writes NOTHING.
//	(2) Managed file with local edits → .harmonik-new created, original UNCHANGED.
//	(3) ManagedRegion file → managed region updated, out-of-marker text preserved
//	    byte-for-byte.
//	(4) ContentOwned FastForward → TIER header updated, BODY byte-identical.
//	(5) Conflict on content-owned → file untouched.
//	(6) lock written after apply and a second apply is a no-op (all Skip).
//	(7) the daemon-lull gate refuses when a dispatching daemon is simulated.
//
// Bead ref: hk-i7i3.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/queue"
)

// gitOutSync runs a git subcommand in dir and returns its combined output,
// fataling on error. (Named to avoid colliding with the package's existing
// runGit helper, which does not surface stdout.)
func gitOutSync(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...) //nolint:gosec
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

// writeFile is a test helper that writes data, creating parent dirs.
func writeFileT(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readFileT(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

func hexSha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// seedCurrentProject writes every embed asset to its init destination so the
// project starts "up to date" against the embed, and stamps a matching lock.
// Returns the manifest for convenience.
func seedCurrentProject(t *testing.T, dir string) Manifest {
	t.Helper()
	m, err := BuildManifest()
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	for _, f := range m.Files {
		dest, ok := destFor(f.Path)
		if !ok {
			continue
		}
		data, rerr := initSkillAssets.ReadFile(f.Path)
		if rerr != nil {
			t.Fatalf("read embed %s: %v", f.Path, rerr)
		}
		// For the AGENTS template, write what init would write (rendered) so the
		// on-disk hash lines up with the embed hash semantics used by the planner.
		if f.Class == ManagedRegion {
			data = []byte(renderAgentsTemplate(string(data), dir))
		}
		writeFileT(t, filepath.Join(dir, dest), data)
	}
	if err := WriteLock(dir, LockFromManifest(m)); err != nil {
		t.Fatalf("WriteLock: %v", err)
	}
	return m
}

// firstEmbedPathOfClass returns the first manifest entry of the given class.
func firstEmbedPathOfClass(t *testing.T, m Manifest, c AssetClass) FileEntry {
	t.Helper()
	for _, f := range m.Files {
		if f.Class == c {
			return f
		}
	}
	t.Fatalf("no embed asset of class %q", c)
	return FileEntry{}
}

// TestSyncAssetsDryRunWritesNothing — invariant (1).
func TestSyncAssetsDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	// A never-synced project: dry-run should plan creates but write nothing.
	var out, errOut bytes.Buffer
	code := runSyncAssets([]string{"--project", dir, "--dry-run"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("dry-run exit = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	// Nothing should exist under .claude or .harmonik/context, and no lock.
	for _, p := range []string{
		filepath.Join(dir, ".claude"),
		filepath.Join(dir, ".harmonik", "context"),
		filepath.Join(dir, ".harmonik", "assets.lock"),
		filepath.Join(dir, "AGENTS.md"),
	} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("dry-run wrote %s — must write nothing", p)
		}
	}
	if !strings.Contains(out.String(), "dry-run") {
		t.Errorf("dry-run output missing 'dry-run' marker:\n%s", out.String())
	}
}

// TestSyncAssetsManagedConflict — invariant (2): a Managed (skill) file with local
// edits and a behind-lock → .harmonik-new written, original UNCHANGED.
func TestSyncAssetsManagedConflict(t *testing.T) {
	dir := t.TempDir()
	m := seedCurrentProject(t, dir)
	skill := firstEmbedPathOfClass(t, m, Managed)
	dest, _ := destFor(skill.Path)
	full := filepath.Join(dir, dest)

	// Local edit: change the on-disk file so disk != embed.
	edited := []byte("LOCAL EDIT — do not clobber\n")
	writeFileT(t, full, edited)

	// Move the lock BEHIND the embed for this path so embed != lock and disk !=
	// both → Conflict.
	lock, _ := ReadLock(dir)
	le := lock.Files[skill.Path]
	le.Sha256 = "0000000000000000000000000000000000000000000000000000000000000000"
	lock.Files[skill.Path] = le
	if err := WriteLock(dir, lock); err != nil {
		t.Fatalf("rewrite lock: %v", err)
	}

	var out, errOut bytes.Buffer
	code := runSyncAssets([]string{"--project", dir, "--apply", "--force"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("apply exit = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	// Original must be UNCHANGED.
	if got := readFileT(t, full); !bytes.Equal(got, edited) {
		t.Errorf("Managed conflict clobbered the edited file:\n got: %q\nwant: %q", got, edited)
	}
	// .harmonik-new must exist and equal the embed content.
	newPath := full + ".harmonik-new"
	embedData, _ := initSkillAssets.ReadFile(skill.Path)
	if got := readFileT(t, newPath); !bytes.Equal(got, embedData) {
		t.Errorf(".harmonik-new content != embed")
	}
	if !strings.Contains(out.String(), "conflicted: 1") {
		t.Errorf("summary missing conflicted tally:\n%s", out.String())
	}
}

// TestSyncAssetsManagedRegionPreservesDeltas — invariant (3): the managed region
// updates while project text outside the markers is preserved byte-for-byte.
func TestSyncAssetsManagedRegionPreservesDeltas(t *testing.T) {
	dir := t.TempDir()
	m := seedCurrentProject(t, dir)
	tpl := firstEmbedPathOfClass(t, m, ManagedRegion)
	dest, _ := destFor(tpl.Path)
	full := filepath.Join(dir, dest)

	// Read the rendered template, find its managed region, and craft an on-disk
	// AGENTS.md that has (a) a STALE managed region body and (b) bespoke project
	// text outside the markers that MUST survive.
	embedRaw, _ := initSkillAssets.ReadFile(tpl.Path)
	rendered := renderAgentsTemplate(string(embedRaw), dir)
	regs := findManagedRegions(rendered)
	if len(regs) == 0 {
		t.Fatalf("template has no managed region")
	}
	r := regs[0]
	begin := rendered[:r.start]
	managed := rendered[r.start:r.end]
	after := rendered[r.end:]

	// Mutate the managed region content (simulate an OLD product region) and add
	// project deltas before and after.
	staleManaged := strings.Replace(managed, "harmonik:managed", "harmonik:managed STALE-MARKER-NOTE", 1)
	projectBefore := "PROJECT PREAMBLE — keep me\n" + begin
	projectAfter := after + "\nPROJECT EPILOGUE — keep me too\n"
	onDisk := projectBefore + staleManaged + projectAfter
	writeFileT(t, full, []byte(onDisk))

	// Behind-lock so the planner sees embed != lock, disk != lock → FastForward
	// is impossible (disk != lock) so it's Conflict — but for ManagedRegion the
	// executor splices the region on BOTH FastForward and Conflict. Set lock to
	// disk's hash to get a clean FastForward path.
	lock, _ := ReadLock(dir)
	le := lock.Files[tpl.Path]
	le.Sha256 = hexSha([]byte(onDisk))
	lock.Files[tpl.Path] = le
	if err := WriteLock(dir, lock); err != nil {
		t.Fatalf("rewrite lock: %v", err)
	}

	var out, errOut bytes.Buffer
	code := runSyncAssets([]string{"--project", dir, "--apply", "--force"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("apply exit = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	got := string(readFileT(t, full))
	// Project deltas survive byte-for-byte.
	if !strings.HasPrefix(got, "PROJECT PREAMBLE — keep me\n") {
		t.Errorf("project preamble not preserved:\n%s", got)
	}
	if !strings.Contains(got, "PROJECT EPILOGUE — keep me too") {
		t.Errorf("project epilogue not preserved")
	}
	// Managed region refreshed: the STALE marker note is gone, real region present.
	if strings.Contains(got, "STALE-MARKER-NOTE") {
		t.Errorf("stale managed region was not refreshed:\n%s", got)
	}
	if !strings.Contains(got, managed) {
		t.Errorf("refreshed managed region does not match template region")
	}
}

// TestSyncAssetsContentOwnedHeaderOnly — invariant (4): FastForward refreshes only
// the TIER header; the body is byte-identical.
func TestSyncAssetsContentOwnedHeaderOnly(t *testing.T) {
	dir := t.TempDir()
	m := seedCurrentProject(t, dir)
	co := firstEmbedPathOfClass(t, m, ContentOwned)
	dest, _ := destFor(co.Path)
	full := filepath.Join(dir, dest)

	embedRaw, _ := initSkillAssets.ReadFile(co.Path)
	hdr, ok := tierHeaderSpan(string(embedRaw))
	if !ok {
		t.Skipf("content-owned asset %s has no TIER header; skipping", co.Path)
	}
	body := string(embedRaw)[hdr.end:]

	// On-disk file: a MUTATED header + the project's own body (different from
	// embed body) — the body must survive untouched.
	staleHeader := "<!-- TIER: 9 (STALE HEADER) -->\n"
	projectBody := body + "\nPROJECT-OWNED BODY LINE — keep me\n"
	onDisk := staleHeader + projectBody
	writeFileT(t, full, []byte(onDisk))

	// Lock == disk hash, embed advanced → FastForward.
	lock, _ := ReadLock(dir)
	le := lock.Files[co.Path]
	le.Sha256 = hexSha([]byte(onDisk))
	lock.Files[co.Path] = le
	if err := WriteLock(dir, lock); err != nil {
		t.Fatalf("rewrite lock: %v", err)
	}

	var out, errOut bytes.Buffer
	code := runSyncAssets([]string{"--project", dir, "--apply", "--force"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("apply exit = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	got := string(readFileT(t, full))
	// Header refreshed (stale gone, embed header present).
	if strings.Contains(got, "STALE HEADER") {
		t.Errorf("stale TIER header not refreshed:\n%s", got)
	}
	// Body byte-identical to the project's body.
	gotHdr, ok := tierHeaderSpan(got)
	if !ok {
		t.Fatalf("output lost its TIER header")
	}
	if gotBody := got[gotHdr.end:]; gotBody != projectBody {
		t.Errorf("BODY changed under FastForward:\n got: %q\nwant: %q", gotBody, projectBody)
	}
}

// TestSyncAssetsContentOwnedConflictUntouched — invariant (5): a Conflict on a
// content-owned file leaves it untouched (no header rewrite, no .harmonik-new).
func TestSyncAssetsContentOwnedConflictUntouched(t *testing.T) {
	dir := t.TempDir()
	m := seedCurrentProject(t, dir)
	co := firstEmbedPathOfClass(t, m, ContentOwned)
	dest, _ := destFor(co.Path)
	full := filepath.Join(dir, dest)

	edited := []byte("<!-- TIER: 3 -->\nLOCALLY EDITED BODY\n")
	writeFileT(t, full, edited)

	// Behind-lock so embed != lock and disk != lock and disk != embed → Conflict.
	lock, _ := ReadLock(dir)
	le := lock.Files[co.Path]
	le.Sha256 = "1111111111111111111111111111111111111111111111111111111111111111"
	lock.Files[co.Path] = le
	if err := WriteLock(dir, lock); err != nil {
		t.Fatalf("rewrite lock: %v", err)
	}

	var out, errOut bytes.Buffer
	code := runSyncAssets([]string{"--project", dir, "--apply", "--force"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("apply exit = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	if got := readFileT(t, full); !bytes.Equal(got, edited) {
		t.Errorf("content-owned Conflict modified the file:\n got: %q\nwant: %q", got, edited)
	}
	if _, err := os.Stat(full + ".harmonik-new"); err == nil {
		t.Errorf("content-owned Conflict wrote a .harmonik-new (should report only)")
	}
}

// TestSyncAssetsLockReStampSecondApplyNoop — invariant (6): after apply the lock
// is stamped and a second apply is an all-Skip no-op (no files change).
func TestSyncAssetsLockReStampSecondApplyNoop(t *testing.T) {
	dir := t.TempDir()
	// Fresh project (no seed): first apply creates everything.
	var out1, err1 bytes.Buffer
	if code := runSyncAssets([]string{"--project", dir, "--apply", "--force"}, &out1, &err1); code != 0 {
		t.Fatalf("first apply exit = %d (stderr: %s)", code, err1.String())
	}
	// Lock must now exist and equal LockFromManifest.
	if _, err := os.Stat(filepath.Join(dir, ".harmonik", "assets.lock")); err != nil {
		t.Fatalf("lock not written after apply: %v", err)
	}
	// Snapshot every destination's hash.
	m, _ := BuildManifest()
	before := map[string]string{}
	for _, f := range m.Files {
		dest, ok := destFor(f.Path)
		if !ok {
			continue
		}
		full := filepath.Join(dir, dest)
		if b, err := os.ReadFile(full); err == nil {
			before[dest] = hexSha(b)
		}
	}

	// Second apply: should be an all-Skip no-op.
	var out2, err2 bytes.Buffer
	if code := runSyncAssets([]string{"--project", dir, "--apply", "--force"}, &out2, &err2); code != 0 {
		t.Fatalf("second apply exit = %d (stderr: %s)", code, err2.String())
	}
	for dest, h := range before {
		full := filepath.Join(dir, dest)
		b := readFileT(t, full)
		if hexSha(b) != h {
			t.Errorf("second apply changed %s (should be no-op)", dest)
		}
	}
	if !strings.Contains(out2.String(), "applied:    0") {
		t.Errorf("second apply not a no-op; summary:\n%s", out2.String())
	}
}

// TestSyncAssetsDaemonLullGate — invariant (7): the PURE dispatching-decision
// refuses on an active queue with in-flight items and allows otherwise. The live
// socket-probe path (daemonSocketUp) requires a running daemon and is covered by
// the live smoke test, not this unit test.
func TestSyncAssetsDaemonLullGate(t *testing.T) {
	now := time.Now()
	active := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "q-active",
		Name:          "main",
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Status:     queue.GroupStatusActive,
			Items: []queue.Item{
				{BeadID: "hk-inflight", Status: queue.ItemStatusDispatched},
			},
			CreatedAt: now,
		}},
	}
	if disp, reason := dispatchingQueue([]*queue.Queue{active}); !disp {
		t.Errorf("dispatchingQueue should refuse on an active dispatched item; got false")
	} else if !strings.Contains(reason, "in-flight") {
		t.Errorf("reason missing context: %q", reason)
	}

	// Completed queue → not dispatching.
	done := &queue.Queue{
		SchemaVersion: 1, QueueID: "q-done", Status: queue.QueueStatusCompleted,
		Groups: []queue.Group{{Status: queue.GroupStatusCompleteSuccess,
			Items: []queue.Item{{BeadID: "hk-done", Status: queue.ItemStatusCompleted}}}},
	}
	if disp, _ := dispatchingQueue([]*queue.Queue{done}); disp {
		t.Errorf("dispatchingQueue should allow on a completed queue")
	}

	// Active queue but all items completed → not dispatching (lull).
	lull := &queue.Queue{
		SchemaVersion: 1, QueueID: "q-lull", Status: queue.QueueStatusActive,
		Groups: []queue.Group{{Status: queue.GroupStatusActive,
			Items: []queue.Item{{BeadID: "hk-c", Status: queue.ItemStatusCompleted}}}},
	}
	if disp, _ := dispatchingQueue([]*queue.Queue{lull}); disp {
		t.Errorf("dispatchingQueue should allow when active group has no pending/dispatched items")
	}

	// No queues → not dispatching.
	if disp, _ := dispatchingQueue(nil); disp {
		t.Errorf("dispatchingQueue(nil) should be false")
	}
}

// TestSyncAssetsConflictNotBuriedInLock — FIX 1: a Managed file with local edits
// must NOT have its lock entry advanced to the embed sha on --apply; a SECOND
// --apply must STILL report the conflict (not Skip it away).
func TestSyncAssetsConflictNotBuriedInLock(t *testing.T) {
	dir := t.TempDir()
	m := seedCurrentProject(t, dir)
	skill := firstEmbedPathOfClass(t, m, Managed)
	dest, _ := destFor(skill.Path)
	full := filepath.Join(dir, dest)

	embedData, _ := initSkillAssets.ReadFile(skill.Path)
	embedSha := hexSha(embedData)

	// Local edit so disk != embed.
	edited := []byte("LOCAL EDIT — do not clobber\n")
	writeFileT(t, full, edited)

	// Move the lock BEHIND the embed for this path → Conflict (disk != lock,
	// disk != embed, embed != lock).
	priorLockSha := "0000000000000000000000000000000000000000000000000000000000000000"
	lock, _ := ReadLock(dir)
	le := lock.Files[skill.Path]
	le.Sha256 = priorLockSha
	lock.Files[skill.Path] = le
	if err := WriteLock(dir, lock); err != nil {
		t.Fatalf("rewrite lock: %v", err)
	}

	// First apply.
	var out1, err1 bytes.Buffer
	if code := runSyncAssets([]string{"--project", dir, "--apply", "--force"}, &out1, &err1); code != 0 {
		t.Fatalf("first apply exit = %d (stderr: %s)", code, err1.String())
	}
	if !strings.Contains(out1.String(), "conflicted: 1") {
		t.Fatalf("first apply did not report the conflict:\n%s", out1.String())
	}

	// The lock entry for the conflicted path must NOT have advanced to the embed
	// sha — it must keep its prior (behind) value so the conflict re-surfaces.
	postLock, _ := ReadLock(dir)
	got := postLock.Files[skill.Path].Sha256
	if got == embedSha {
		t.Fatalf("lock entry for conflicted path was advanced to embed sha — conflict buried")
	}
	if got != priorLockSha {
		t.Errorf("conflicted path lock = %q, want preserved prior %q", got, priorLockSha)
	}

	// Second apply: original still edited → must STILL report the conflict, NOT
	// skip it away.
	var out2, err2 bytes.Buffer
	if code := runSyncAssets([]string{"--project", dir, "--apply", "--force"}, &out2, &err2); code != 0 {
		t.Fatalf("second apply exit = %d (stderr: %s)", code, err2.String())
	}
	if !strings.Contains(out2.String(), "conflicted: 1") {
		t.Errorf("second apply buried the conflict (expected conflicted: 1):\n%s", out2.String())
	}
	// Original is still the local edit.
	if cur := readFileT(t, full); !bytes.Equal(cur, edited) {
		t.Errorf("second apply clobbered the edited file: %q", cur)
	}
}

// TestSyncAssetsCommitStagesOnlyTouchedPaths — FIX 2: --commit must stage ONLY
// the synced dest paths, never an unrelated dirty file (no `git add -A`).
func TestSyncAssetsCommitStagesOnlyTouchedPaths(t *testing.T) {
	dir := t.TempDir()

	// Stand up a real git repo with an initial commit.
	runGit(t, dir, "init")
	writeFileT(t, filepath.Join(dir, "seed.txt"), []byte("seed\n"))
	runGit(t, dir, "add", "seed.txt")
	runGit(t, dir, "commit", "-m", "seed")

	// An UNRELATED dirty file that must NOT be staged/committed by sync-assets.
	unrelated := filepath.Join(dir, "unrelated.txt")
	writeFileT(t, unrelated, []byte("DO NOT COMMIT ME\n"))

	// Apply + commit into the fresh repo (no daemon → gate proceeds).
	var out, errOut bytes.Buffer
	if code := runSyncAssets([]string{"--project", dir, "--commit", "--force"}, &out, &errOut); code != 0 {
		t.Fatalf("--commit exit = %d (stderr: %s)", code, errOut.String())
	}

	// The unrelated file must remain UNTRACKED (never added to the commit).
	status := gitOutSync(t, dir, "status", "--porcelain", "--", "unrelated.txt")
	if !strings.HasPrefix(strings.TrimSpace(status), "??") {
		t.Errorf("unrelated.txt was staged/committed by sync-assets; status = %q", status)
	}

	// And it must NOT appear in the sync-assets commit's file list.
	files := gitOutSync(t, dir, "show", "--name-only", "--pretty=format:", "HEAD")
	if strings.Contains(files, "unrelated.txt") {
		t.Errorf("sync-assets commit included unrelated.txt:\n%s", files)
	}
	// Sanity: the commit DID stage real synced assets (e.g. AGENTS.md or a skill).
	if strings.TrimSpace(files) == "" {
		t.Errorf("sync-assets commit staged nothing")
	}
}

// TestSyncAssetsGateProceedsWhenDaemonDown — the gate proceeds (apply runs) when
// no daemon socket exists, exercised end-to-end via a fresh project (no socket).
func TestSyncAssetsGateProceedsWhenDaemonDown(t *testing.T) {
	dir := t.TempDir()
	disp, _, err := daemonDispatchGate(dir)
	if err != nil {
		t.Fatalf("daemonDispatchGate on a project with no daemon: %v", err)
	}
	if disp {
		t.Errorf("gate reported dispatching with no daemon socket present")
	}
}
