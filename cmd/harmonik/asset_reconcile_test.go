package main

// asset_reconcile_test.go — exhaustive coverage of the 3-way reconcile PLANNER
// and the assets.lock I/O. Every matrix cell (skip/create/fast-forward/conflict/
// leave + the lock-stale-but-disk==embed case + missing-disk-restore) is exercised
// for at least one representative of each AssetClass, and the lock round-trip is
// checked including the absent-file → empty-lock contract and deterministic JSON.
//
// Bead ref: hk-gh1m (assets.lock + 3-way reconcile engine).

import (
	"os"
	"path/filepath"
	"testing"
)

// hash stand-ins. The reconcile planner is a pure string comparison, so any
// distinct fixed strings stand in for sha256 digests.
const (
	shaEmbed = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	shaLock  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	shaLocal = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

// classRep returns a representative embed path for each AssetClass, so the matrix
// is exercised once per class via Classify.
func classRep(c AssetClass) string {
	switch c {
	case Managed:
		return "assets/skills/keeper/SKILL.md"
	case ManagedRegion:
		return "assets/templates/AGENTS.template.md"
	case ContentOwned:
		return "assets/context/project.yaml.tmpl"
	case Scaffold:
		return "assets/scaffolds/STATUS.md"
	default:
		return "assets/other/thing.md"
	}
}

func TestReconcileMatrix(t *testing.T) {
	classes := []AssetClass{Managed, ManagedRegion, ContentOwned, Scaffold}

	// Each case describes the three legs for ONE path and the expected verdict.
	// inEmbed/inLock toggle membership; embedSha/lockSha/diskSha set the hashes.
	type tc struct {
		name    string
		inEmbed bool
		inLock  bool
		embed   string
		lock    string
		disk    string // "" = absent on disk
		want    Action
	}

	cases := []tc{
		{name: "skip-current", inEmbed: true, inLock: true, embed: shaEmbed, lock: shaEmbed, disk: shaEmbed, want: ActionSkip},
		{name: "create-restore-current", inEmbed: true, inLock: true, embed: shaEmbed, lock: shaEmbed, disk: "", want: ActionCreate},
		{name: "fast-forward", inEmbed: true, inLock: true, embed: shaEmbed, lock: shaLock, disk: shaLock, want: ActionFastForward},
		{name: "conflict-local-edit", inEmbed: true, inLock: true, embed: shaEmbed, lock: shaLock, disk: shaLocal, want: ActionConflict},
		{name: "skip-lock-stale-disk-eq-embed", inEmbed: true, inLock: true, embed: shaEmbed, lock: shaLock, disk: shaEmbed, want: ActionSkip},
		{name: "create-deleted-locally", inEmbed: true, inLock: true, embed: shaEmbed, lock: shaLock, disk: "", want: ActionCreate},
		{name: "create-new-asset-absent", inEmbed: true, inLock: false, embed: shaEmbed, disk: "", want: ActionCreate},
		{name: "skip-new-asset-disk-eq-embed", inEmbed: true, inLock: false, embed: shaEmbed, disk: shaEmbed, want: ActionSkip},
		{name: "conflict-new-asset-disk-differs", inEmbed: true, inLock: false, embed: shaEmbed, disk: shaLocal, want: ActionConflict},
		{name: "leave-project-authored", inEmbed: false, inLock: false, disk: shaLocal, want: ActionLeave},
	}

	for _, class := range classes {
		path := classRep(class)
		for _, c := range cases {
			t.Run(string(class)+"/"+c.name, func(t *testing.T) {
				var m Manifest
				m.FormatVersion = ManifestFormatVersion
				if c.inEmbed {
					m.Files = append(m.Files, FileEntry{Path: path, Sha256: c.embed, Class: class})
				}

				lock := Lock{FormatVersion: LockFormatVersion, Files: map[string]LockEntry{}}
				if c.inLock {
					lock.Files[path] = LockEntry{Path: path, Sha256: c.lock}
				}

				disk := map[string]string{}
				if c.disk != "" {
					disk[path] = c.disk
				}
				// The leave case is only meaningful when disk holds a path absent
				// from the manifest; ensure the path is recorded on disk.
				if !c.inEmbed {
					disk[path] = c.disk
				}

				items := Reconcile(m, lock, disk)
				if len(items) != 1 {
					t.Fatalf("expected 1 item, got %d: %+v", len(items), items)
				}
				got := items[0]
				if got.Action != c.want {
					t.Errorf("Action = %q, want %q (reason: %s)", got.Action, c.want, got.Reason)
				}
				if got.Class != class {
					t.Errorf("Class = %q, want %q", got.Class, class)
				}
				if got.Path != path {
					t.Errorf("Path = %q, want %q", got.Path, path)
				}
				// Sanity: the *Sha fields echo the inputs.
				if c.inEmbed && got.EmbedSha != c.embed {
					t.Errorf("EmbedSha = %q, want %q", got.EmbedSha, c.embed)
				}
				if c.inLock && got.LockSha != c.lock {
					t.Errorf("LockSha = %q, want %q", got.LockSha, c.lock)
				}
				if got.DiskSha != c.disk {
					t.Errorf("DiskSha = %q, want %q", got.DiskSha, c.disk)
				}
			})
		}
	}
}

// TestReconcileUnclassified covers the Unclassified class (hk-gh1m gap 1): an
// embed path matching no known class prefix is still planned, carries the
// Unclassified class, and reconciles conservatively (create-if-missing, skip when
// disk already matches, conflict when disk differs).
func TestReconcileUnclassified(t *testing.T) {
	const path = "assets/other/thing.md"
	if got := Classify(path); got != Unclassified {
		t.Fatalf("Classify(%q) = %q, want %q", path, got, Unclassified)
	}

	cases := []struct {
		name string
		disk string // "" = absent
		want Action
	}{
		{name: "create-when-absent", disk: "", want: ActionCreate},
		{name: "skip-when-disk-eq-embed", disk: shaEmbed, want: ActionSkip},
		{name: "conflict-when-disk-differs", disk: shaLocal, want: ActionConflict},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := Manifest{FormatVersion: ManifestFormatVersion, Files: []FileEntry{
				{Path: path, Sha256: shaEmbed, Class: Unclassified},
			}}
			lock := Lock{FormatVersion: LockFormatVersion, Files: map[string]LockEntry{}}
			disk := map[string]string{}
			if c.disk != "" {
				disk[path] = c.disk
			}
			items := Reconcile(m, lock, disk)
			if len(items) != 1 {
				t.Fatalf("expected 1 item, got %d", len(items))
			}
			if items[0].Class != Unclassified {
				t.Errorf("Class = %q, want %q", items[0].Class, Unclassified)
			}
			if items[0].Action != c.want {
				t.Errorf("Action = %q, want %q (reason: %s)", items[0].Action, c.want, items[0].Reason)
			}
		})
	}
}

// TestReconcileLeavePathClass asserts the Leave path (a project-authored file not
// in the embed manifest) is both ActionLeave AND classified by its on-disk path
// (hk-gh1m gap 2): a disk-only path under context/ classifies ContentOwned even
// though the planner leaves it untouched.
func TestReconcileLeavePathClass(t *testing.T) {
	// Empty manifest: nothing is shipped, so every disk path is project-authored.
	m := Manifest{FormatVersion: ManifestFormatVersion}
	lock := Lock{FormatVersion: LockFormatVersion, Files: map[string]LockEntry{}}
	disk := map[string]string{
		"assets/context/notes.md":  shaLocal, // structurally ContentOwned
		"assets/skills/mine/X.md":  shaLocal, // structurally Managed
		"assets/random/standalone": shaLocal, // Unclassified
	}
	items := Reconcile(m, lock, disk)
	byPath := map[string]ReconcileItem{}
	for _, it := range items {
		byPath[it.Path] = it
	}
	checks := []struct {
		path  string
		class AssetClass
	}{
		{"assets/context/notes.md", ContentOwned},
		{"assets/skills/mine/X.md", Managed},
		{"assets/random/standalone", Unclassified},
	}
	for _, c := range checks {
		it, ok := byPath[c.path]
		if !ok {
			t.Fatalf("missing reconcile item for %q", c.path)
		}
		if it.Action != ActionLeave {
			t.Errorf("%q: Action = %q, want %q", c.path, it.Action, ActionLeave)
		}
		if it.Class != c.class {
			t.Errorf("%q: Class = %q, want %q (Leave items must still carry the structural class)", c.path, it.Class, c.class)
		}
	}
}

// TestReconcileDeterministicOrder asserts the plan is sorted by path regardless
// of map iteration order.
func TestReconcileDeterministicOrder(t *testing.T) {
	m := Manifest{FormatVersion: ManifestFormatVersion, Files: []FileEntry{
		{Path: "assets/skills/zeta/SKILL.md", Sha256: shaEmbed, Class: Managed},
		{Path: "assets/skills/alpha/SKILL.md", Sha256: shaEmbed, Class: Managed},
	}}
	lock := Lock{FormatVersion: LockFormatVersion, Files: map[string]LockEntry{}}
	disk := map[string]string{"assets/mine.md": shaLocal}

	items := Reconcile(m, lock, disk)
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	want := []string{"assets/mine.md", "assets/skills/alpha/SKILL.md", "assets/skills/zeta/SKILL.md"}
	for i, w := range want {
		if items[i].Path != w {
			t.Errorf("items[%d].Path = %q, want %q", i, items[i].Path, w)
		}
	}
}

func TestLockRoundTrip(t *testing.T) {
	dir := t.TempDir()

	l := Lock{
		FormatVersion: LockFormatVersion,
		Files: map[string]LockEntry{
			"assets/skills/keeper/SKILL.md":       {Path: "assets/skills/keeper/SKILL.md", Sha256: shaEmbed},
			"assets/templates/AGENTS.template.md": {Path: "assets/templates/AGENTS.template.md", Sha256: shaLock},
		},
	}

	if err := WriteLock(dir, l); err != nil {
		t.Fatalf("WriteLock: %v", err)
	}

	got, err := ReadLock(dir)
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}
	if got.FormatVersion != LockFormatVersion {
		t.Errorf("FormatVersion = %d, want %d", got.FormatVersion, LockFormatVersion)
	}
	if len(got.Files) != len(l.Files) {
		t.Fatalf("Files len = %d, want %d", len(got.Files), len(l.Files))
	}
	for p, want := range l.Files {
		if got.Files[p] != want {
			t.Errorf("Files[%q] = %+v, want %+v", p, got.Files[p], want)
		}
	}
}

// TestReadLockAbsentFile asserts a never-synced project yields an empty Lock and
// no error.
func TestReadLockAbsentFile(t *testing.T) {
	dir := t.TempDir()
	got, err := ReadLock(dir)
	if err != nil {
		t.Fatalf("ReadLock on absent file should not error, got: %v", err)
	}
	if len(got.Files) != 0 {
		t.Errorf("expected empty lock, got %d entries", len(got.Files))
	}
}

// TestWriteLockDeterministic asserts byte-identical output for equal locks
// regardless of map insertion order.
func TestWriteLockDeterministic(t *testing.T) {
	d1 := t.TempDir()
	d2 := t.TempDir()

	files := map[string]LockEntry{
		"assets/c.md": {Path: "assets/c.md", Sha256: shaEmbed},
		"assets/a.md": {Path: "assets/a.md", Sha256: shaLock},
		"assets/b.md": {Path: "assets/b.md", Sha256: shaLocal},
	}
	// Two locks with identically-valued (but distinctly-constructed) maps.
	l1 := Lock{FormatVersion: LockFormatVersion, Files: files}
	l2 := Lock{FormatVersion: LockFormatVersion, Files: map[string]LockEntry{
		"assets/b.md": {Path: "assets/b.md", Sha256: shaLocal},
		"assets/a.md": {Path: "assets/a.md", Sha256: shaLock},
		"assets/c.md": {Path: "assets/c.md", Sha256: shaEmbed},
	}}

	if err := WriteLock(d1, l1); err != nil {
		t.Fatalf("WriteLock d1: %v", err)
	}
	if err := WriteLock(d2, l2); err != nil {
		t.Fatalf("WriteLock d2: %v", err)
	}

	b1, err := os.ReadFile(filepath.Join(d1, lockRelPath))
	if err != nil {
		t.Fatalf("read d1: %v", err)
	}
	b2, err := os.ReadFile(filepath.Join(d2, lockRelPath))
	if err != nil {
		t.Fatalf("read d2: %v", err)
	}
	if string(b1) != string(b2) {
		t.Errorf("lock JSON not deterministic:\n--- d1 ---\n%s\n--- d2 ---\n%s", b1, b2)
	}
}

func TestLockFromManifest(t *testing.T) {
	m := Manifest{FormatVersion: ManifestFormatVersion, Files: []FileEntry{
		{Path: "assets/skills/keeper/SKILL.md", Sha256: shaEmbed, Class: Managed},
		{Path: "assets/context/project.yaml.tmpl", Sha256: shaLock, Class: ContentOwned},
	}}

	l := LockFromManifest(m)
	if l.FormatVersion != LockFormatVersion {
		t.Errorf("FormatVersion = %d, want %d", l.FormatVersion, LockFormatVersion)
	}
	if len(l.Files) != 2 {
		t.Fatalf("Files len = %d, want 2", len(l.Files))
	}
	for _, f := range m.Files {
		e, ok := l.Files[f.Path]
		if !ok {
			t.Errorf("missing lock entry for %q", f.Path)
			continue
		}
		if e.Sha256 != f.Sha256 || e.Path != f.Path {
			t.Errorf("Files[%q] = %+v, want path=%q sha=%q", f.Path, e, f.Path, f.Sha256)
		}
	}
}
