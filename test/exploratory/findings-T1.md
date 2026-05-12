# Exploratory Testing Wave — T1 Findings (Cold Start / Happy Path Variants)

**Tester:** T1 agent
**Date:** 2026-05-12
**Scope:** Cold start / happy path variants — 1-bead success, sequential runs, bead status after close, shape variants.
**Branch:** worktree-agent-ab80003254d6bc5f0

---

## Methodology

Binary built from source: `go build -o ./harmonik ./cmd/harmonik`

Tests were run in two modes:

1. **Binary mode** — `./harmonik --project <tmpdir>` directly.
2. **Library mode** — `go test ./internal/daemon/ -run TestMVHSmoke` using the
   smoke test's direct `daemon.Start` call with `BrPath` + `JSONLLogPath` +
   `HandlerBinary` wired in.

All project directories were fresh `git init` trees with `.harmonik/events/` and
`.harmonik/beads-intents/` pre-created as required by the smoke test pattern.

---

## F-T1-001 — Production binary never dispatches any beads (work loop not wired)

**Severity:** functional

**Repro:**
```sh
PROJ=$(mktemp -d)
git -C "$PROJ" init --initial-branch=main && git -C "$PROJ" config user.email "test@harmonik.local"
git -C "$PROJ" config user.name "Harmonik Test" && echo "test" > "$PROJ/README"
git -C "$PROJ" add README && git -C "$PROJ" commit -m "Initial"
(cd "$PROJ" && br init --prefix t1)
BR_ID=$(br --db "$PROJ/.beads/beads.db" create "my bead" --body "do the work" --silent)
./harmonik --project "$PROJ"
br --db "$PROJ/.beads/beads.db" show "$BR_ID"
# bead status is still "open"; exit code 0; no output
```

**Expected:** Daemon picks up the ready bead, dispatches it, closes it upon
handler exit-0, then waits for SIGINT.

**Actual:** Binary exits ~140 ms after launch with exit code 0, no stdout/stderr.
The bead remains `open`. The work loop never runs.

**Root cause:** `cmd/harmonik/main.go` constructs `daemon.Config` with only
`ProjectDir` set. `Config.BrPath` is left empty (no `exec.LookPath("br")` call).
`daemon.Start` skips the entire work loop when `BrPath == ""` (guard at
`daemon.go:251`). Comment at `main.go:89-91` explicitly says "no env-var
fallbacks, no config-file loading" but also omits the `LookPath` wiring.

**Impact:** The full happy path (bead → closed → JSONL events) is only reachable
through the Go test infrastructure (`TestMVHSmoke`), not the deployed binary.

---

## F-T1-002 — Production binary emits no JSONL events (JSONLLogPath not wired)

**Severity:** functional

**Repro:**
```sh
PROJ=$(mktemp -d)
git -C "$PROJ" init --initial-branch=main && git -C "$PROJ" config user.email "test@harmonik.local"
git -C "$PROJ" config user.name "Harmonik Test" && echo "test" > "$PROJ/README"
git -C "$PROJ" add README && git -C "$PROJ" commit -m "Initial"
mkdir -p "$PROJ/.harmonik/events"
./harmonik --project "$PROJ"
ls "$PROJ/.harmonik/events/"
# directory is empty; no events.jsonl created
```

**Expected:** `daemon_started` event written to
`<ProjectDir>/.harmonik/events/events.jsonl` per spec `event-model.md §6.2 EV-020`.

**Actual:** `events/` directory remains empty. No `events.jsonl` created.

**Root cause:** `cmd/harmonik/main.go` does not set `Config.JSONLLogPath`.
`daemon.Start` opens the JSONL writer only when `cfg.JSONLLogPath != ""`
(`daemon.go:159`). Not set in production binary, so no events are persisted.

**Note:** The `daemon_started` event IS emitted to the in-process bus (confirming
bus wiring is correct), but with no JSONL writer attached it is silently dropped.

---

## F-T1-003 — Production binary does not create .harmonik/events/ or .harmonik/beads-intents/

**Severity:** cosmetic

**Repro:**
```sh
PROJ=$(mktemp -d)
git -C "$PROJ" init --initial-branch=main && git -C "$PROJ" config user.email "test@harmonik.local"
git -C "$PROJ" config user.name "Harmonik Test" && echo "test" > "$PROJ/README"
git -C "$PROJ" add README && git -C "$PROJ" commit -m "Initial"
./harmonik --project "$PROJ"
find "$PROJ/.harmonik" -type d
# output: /tmp/.../  .harmonik/  (no events/ or beads-intents/ subdirs)
```

**Expected:** Binary creates all subdirectories it needs (`events/`,
`beads-intents/`) so operators can run against a clean project.

**Actual:** Only `.harmonik/` itself is created (plus `daemon.pid`). Subdirs
are absent.

**Root cause:** `daemon.Start` only calls `os.MkdirAll` for `.harmonik/` itself
(for the pidfile). Creation of `events/` and `beads-intents/` is left to the
caller. The smoke test pre-creates both; the production binary does not.

**Impact:** If JSONLLogPath were wired, `eventbus.OpenJSONLWriter` would fail
immediately with "no such file or directory" because `events/` is absent.

---

## F-T1-004 — Production binary exits silently with no operator feedback

**Severity:** cosmetic

**Repro:**
```sh
PROJ=$(mktemp -d)
git -C "$PROJ" init --initial-branch=main && git -C "$PROJ" config user.email "test@harmonik.local"
git -C "$PROJ" config user.name "Harmonik Test" && echo "test" > "$PROJ/README"
git -C "$PROJ" add README && git -C "$PROJ" commit -m "Initial"
time ./harmonik --project "$PROJ"
# real  ~0m0.139s; exit 0; no stdout; no stderr
```

**Expected:** Startup message or blocking run indicating daemon is active.

**Actual:** Silent exit in ~140 ms. Operator running this against a project with
ready beads gets no indication that nothing happened.

**Root cause:** Same as F-T1-001 — no work loop. Without it the binary completes
all startup steps instantly and returns. No "daemon is running" message, no "br
not found" warning, no indication of idle vs. active state.

---

## Happy Path via Library (TestMVHSmoke) — CONFIRMED PASSING

Verified via `go test ./internal/daemon/ -run TestMVHSmoke -v -count=1`:

- 1 ready bead + `daemon.Start` (with `BrPath`, `JSONLLogPath`, `HandlerBinary`
  all wired) → claims bead, creates git worktree, spawns handler, waits for exit
  0, closes bead, emits events to JSONL.
- JSONL log contains 4 events: `daemon_started`, `daemon_orphan_sweep_completed`,
  `run_started`, `run_completed`.
- Bead status after run: `closed`.
- Run completes in ~1.35 s including git worktree creation.
- All 21 daemon package tests pass.

**Idempotency of closure:** `TestDaemonStart_PidfileBlocksSecondInvocation`
confirms a second `daemon.Start` against the same project dir (while first holds
the flock) is correctly rejected with an error.

**Shape variants (ASCII, multi-paragraph, no description):** Fixture seed creates
all 5 body variants; all are correctly returned by `br ready --format json`. The
work loop is body-agnostic — bead content is not read during dispatch.

---

## Summary

The T1 happy path is **reachable only via the Go test infrastructure**, not the
production binary. `cmd/harmonik/main.go` is missing three wiring calls present
in the smoke test fixture:

| Missing | Where needed | Impact |
|---|---|---|
| `exec.LookPath("br")` → `cfg.BrPath` | main.go | Work loop never runs |
| Canonical JSONL path → `cfg.JSONLLogPath` | main.go | No events persisted |
| Handler binary resolution → `cfg.HandlerBinary` | main.go | Would default to `claude` if loop ran |

The underlying library (`daemon.Start` with full `Config`) correctly implements
the complete happy path. The gap is entirely at the CLI composition root.
