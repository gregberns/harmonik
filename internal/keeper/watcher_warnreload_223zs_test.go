package keeper

// watcher_warnreload_223zs_test.go — T4 (hk-keeper-delivery-mtime-reread-223zs):
// the SK-034 mtime-gated per-tick re-read of keeper.warn_messages. Unit-tests
// maybeReloadWarnMessages directly (no full Run loop): the re-read is stat-gated
// (unchanged mtime never re-parses), applies ONLY the four warn-text fields
// (thresholds untouched → scoping), and a rejected re-read keeps the last-good
// text while still advancing the mtime so a later fix re-reads.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func warnReloadFixtureWatcher(t *testing.T, fn func() (WarnMessageTexts, error)) (*Watcher, string) {
	t.Helper()
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".harmonik")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	cfgPath := filepath.Join(cfgDir, "config.yaml")
	// Content is irrelevant here — the parse is faked via fn; only the mtime matters.
	if err := os.WriteFile(cfgPath, []byte("schema_version: 1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	w := &Watcher{cfg: WatcherConfig{
		ProjectDir:           dir,
		ReloadWarnMessagesFn: fn,
		// A non-warn field that MUST survive a warn_messages reload (scoping proof).
		WarnAbsTokens: 180_000,
	}}
	return w, cfgPath
}

func setMtime(t *testing.T, path string, tm time.Time) {
	t.Helper()
	if err := os.Chtimes(path, tm, tm); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
}

func TestMaybeReloadWarnMessages_MtimeGatedAndScoped_223zs(t *testing.T) {
	ctx := context.Background()

	var calls int
	ret := WarnMessageTexts{}
	var retErr error
	w, cfgPath := warnReloadFixtureWatcher(t, func() (WarnMessageTexts, error) {
		calls++
		return ret, retErr
	})

	// Seed the mtime at an initial value (mirrors Run() seedConfigMtime).
	t0 := time.Now().Add(-time.Hour).Truncate(time.Second)
	setMtime(t, cfgPath, t0)
	w.seedConfigMtime()

	// (1) Unchanged mtime → stat-gated, no re-parse.
	w.maybeReloadWarnMessages(ctx)
	if calls != 0 {
		t.Fatalf("unchanged mtime must not re-parse; got %d calls", calls)
	}

	// (2) mtime advances → re-parse applies ALL four warn-text fields.
	ret = WarnMessageTexts{
		DefaultWarnText:    "d-v1",
		ActionableWarnText: "harmonik keeper restart-now a-v1",
		LeaderDeferText:    "l-v1",
		CrewDeferText:      "c-v1",
	}
	t1 := t0.Add(time.Minute)
	setMtime(t, cfgPath, t1)
	w.maybeReloadWarnMessages(ctx)
	if calls != 1 {
		t.Fatalf("advanced mtime must re-parse once; got %d calls", calls)
	}
	if w.cfg.DefaultWarnText != "d-v1" || w.cfg.ActionableWarnText != "harmonik keeper restart-now a-v1" ||
		w.cfg.LeaderDeferText != "l-v1" || w.cfg.CrewDeferText != "c-v1" {
		t.Fatalf("warn texts not applied: default=%q actionable=%q leader=%q crew=%q",
			w.cfg.DefaultWarnText, w.cfg.ActionableWarnText, w.cfg.LeaderDeferText, w.cfg.CrewDeferText)
	}
	// Scoping: a non-warn field is NEVER touched by the warn_messages reload.
	if w.cfg.WarnAbsTokens != 180_000 {
		t.Errorf("scoping violated: WarnAbsTokens changed to %d during warn_messages reload", w.cfg.WarnAbsTokens)
	}

	// (3) Same mtime again → no re-parse (still 1 call).
	w.maybeReloadWarnMessages(ctx)
	if calls != 1 {
		t.Fatalf("unchanged mtime after a reload must not re-parse; got %d calls", calls)
	}

	// (4) mtime advances but the re-read is REJECTED (unknown key) → keep last-good
	// text, still advance the mtime so a later fix re-reads.
	ret = WarnMessageTexts{DefaultWarnText: "d-v2"} // would-be new value, must NOT apply
	retErr = errors.New("keeper.warn_messages.bogus: unknown key")
	t2 := t1.Add(time.Minute)
	setMtime(t, cfgPath, t2)
	w.maybeReloadWarnMessages(ctx)
	if calls != 2 {
		t.Fatalf("advanced mtime must attempt a re-parse; got %d calls", calls)
	}
	if w.cfg.DefaultWarnText != "d-v1" {
		t.Errorf("rejected re-read must keep last-good text; got %q", w.cfg.DefaultWarnText)
	}

	// (5) A subsequent VALID edit (new mtime) re-reads despite the prior reject —
	// proves the mtime advanced on the rejected read.
	ret = WarnMessageTexts{DefaultWarnText: "d-v3"}
	retErr = nil
	t3 := t2.Add(time.Minute)
	setMtime(t, cfgPath, t3)
	w.maybeReloadWarnMessages(ctx)
	if w.cfg.DefaultWarnText != "d-v3" {
		t.Errorf("valid re-read after a reject must apply; got %q", w.cfg.DefaultWarnText)
	}
}

func TestMaybeReloadWarnMessages_NoopWhenUnwired_223zs(t *testing.T) {
	ctx := context.Background()

	// nil ReloadWarnMessagesFn → no live reload, no panic.
	w := &Watcher{cfg: WatcherConfig{ProjectDir: t.TempDir(), DefaultWarnText: "startup"}}
	w.maybeReloadWarnMessages(ctx)
	if w.cfg.DefaultWarnText != "startup" {
		t.Errorf("nil fn must not change text; got %q", w.cfg.DefaultWarnText)
	}

	// fn wired but config file absent → no-op (startup texts stand).
	var calls int
	w2 := &Watcher{cfg: WatcherConfig{
		ProjectDir:           t.TempDir(), // .harmonik/config.yaml does not exist
		DefaultWarnText:      "startup",
		ReloadWarnMessagesFn: func() (WarnMessageTexts, error) { calls++; return WarnMessageTexts{}, nil },
	}}
	w2.seedConfigMtime()
	w2.maybeReloadWarnMessages(ctx)
	if calls != 0 {
		t.Errorf("absent config must not call the reload fn; got %d calls", calls)
	}
	if w2.cfg.DefaultWarnText != "startup" {
		t.Errorf("absent config must keep startup text; got %q", w2.cfg.DefaultWarnText)
	}
}
