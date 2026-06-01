package core

import (
	"testing"
)

// ---- RC-019: WIPCapture type ----

func TestRC019_WIPCapture_Valid_RequiresNonEmptyWorktreePath(t *testing.T) {
	t.Parallel()

	w := WIPCapture{WorktreePath: "/some/path"}
	if !w.Valid() {
		t.Error("RC-019: WIPCapture with non-empty WorktreePath should be valid")
	}

	w.WorktreePath = ""
	if w.Valid() {
		t.Error("RC-019: WIPCapture with empty WorktreePath should be invalid")
	}
}

func TestRC019_WIPCapture_HasWIP_EmptyCaptureReturnsFalse(t *testing.T) {
	t.Parallel()

	w := WIPCapture{WorktreePath: "/some/path"}
	if w.HasWIP() {
		t.Error("RC-019: empty WIPCapture (no status, no untracked) should report HasWIP=false")
	}
}

func TestRC019_WIPCapture_HasWIP_NonEmptyStatusReturnsTrue(t *testing.T) {
	t.Parallel()

	w := WIPCapture{
		WorktreePath:       "/some/path",
		GitStatusPorcelain: "M  file.go\n",
	}
	if !w.HasWIP() {
		t.Error("RC-019: WIPCapture with non-empty GitStatusPorcelain should report HasWIP=true")
	}
}

func TestRC019_WIPCapture_HasWIP_UntrackedFilesReturnsTrue(t *testing.T) {
	t.Parallel()

	w := WIPCapture{
		WorktreePath:   "/some/path",
		UntrackedFiles: []string{"new-file.go"},
	}
	if !w.HasWIP() {
		t.Error("RC-019: WIPCapture with UntrackedFiles should report HasWIP=true")
	}
}

func TestRC019_WIPCapture_FileNameConstants(t *testing.T) {
	t.Parallel()

	// The canonical file names in the wip-capture/ directory must match the
	// names documented in the spec (RC-019).
	if WIPCaptureStatusFile == "" {
		t.Error("RC-019: WIPCaptureStatusFile constant must be non-empty")
	}
	if WIPCaptureDiffFile == "" {
		t.Error("RC-019: WIPCaptureDiffFile constant must be non-empty")
	}
	if WIPCaptureUntrackedFile == "" {
		t.Error("RC-019: WIPCaptureUntrackedFile constant must be non-empty")
	}

	// File names must be distinct.
	names := []string{WIPCaptureStatusFile, WIPCaptureDiffFile, WIPCaptureUntrackedFile}
	seen := make(map[string]bool)
	for _, n := range names {
		if seen[n] {
			t.Errorf("RC-019: duplicate WIP capture file name %q", n)
		}
		seen[n] = true
	}
}

func TestRC019_WIPCapture_ZeroValueIsInvalid(t *testing.T) {
	t.Parallel()

	var w WIPCapture
	if w.Valid() {
		t.Error("RC-019: zero-value WIPCapture must be invalid (WorktreePath empty)")
	}
}
