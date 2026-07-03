# keeper-redesign — Problem Space

**Work:** keeper-redesign (spec jig)
**Codename label:** `codename:keeper-redesign`
**Umbrella bead:** hk-gffc (authoritative-identity + gauge-liveness + operator-attached refactor — break the ~69% fix-of-fix loop)
**Status when drafted:** problem-space

---

## What is changing, and why

The harmonik **keeper** is a per-orchestrator / per-crew context-fill watcher. It gauges a
long-lived Claude Code session's context usage and, when context fills, drives an
intent-preserving cycle: **session-handoff → /clear → /session-resume** — BEFORE the pane
overflows and stops accepting keystrokes. It is NOT the daemon; the daemon supervisor does
NOT auto-revive a keeper. A keeper is launched per session and bound to that session.

The keeper has a documented **~69% fix-of-fix regression history**: roughly two of every
three fixes to the identity/liveness state machine re-broke a previously-fixed case. A
33-agent adversarial deep-dive established that the **dominant** failure modes are
**UPSTREAM of the inject step**, not in the inject step itself:

- **gauge-never-live** — ~2,852 `no_gauge:stale` events: the `.ctx` gauge goes stale (or is
  never written) while the watched pane is demonstrably alive, so BOTH the warn and the act
  triggers die before the cycle can fire.
- **operator-attached false-suppress** — ~3,956 suppressions on the captain: the
  `OperatorAttached` check (a raw `tmux list-clients` attached-count) cannot distinguish an
  *idle human terminal* / *remote-control client* / *monitor* from an operator who is
  actively typing, so it permanently parks the captain in warn-only under the iOS / remote
  workflow.

By contrast, the inject step itself fails only ~0.5% of the time. The architecture is
**SOUND**. This is a **SIGNIFICANT REFACTOR of the upstream identity + liveness layers**, NOT
a replacement of the keeper.

The refactor's organizing thesis: the fix-of-fix loop is *caused by* a heuristic identity
state machine (latch / auto-clear / flap-cooldown / suppress / UUID-version guards /
uppercase guards) that tries to *infer* the session it should be bound to. Every new edge
case spawned a new heuristic branch, and each branch interacted with the others. We replace
inference with **authoritative identity passed at launch**, and we delete the heuristic
machinery outright so it cannot regress.

---

## Goals (what is true about the system afterward)

- **G1 — Authoritative identity.** The keeper binds its session identity from a UUIDv4
  **passed at launch** (`harmonik keeper --session-id <uuid4>`), written ONCE to `.managed`,
  and NEVER scraped or heuristically derived from the gauge file or the transcript filename.
- **G2 — Single writer.** Exactly ONE writer of `.managed` and exactly ONE gauge writer.
  `WriteManagedSessionFn` is called ONCE, at boot. No auto-clear, no re-latch, no flap/latch
  recovery loop.
- **G3 — Net LOC down for identity machinery.** The ~150-line heuristic identity block at
  `internal/keeper/watcher.go:664-888` PLUS its named config knobs PLUS the `keeper rebind`
  CLI are DELETED. The diff for identity machinery is a NET REMOVAL.
- **G4 — Live gauge.** The gauge has a liveness guarantee: when the gauge is stale or absent
  but the pane is alive, occupancy is derived from the transcript and `.ctx` is refreshed, so
  the warn/act triggers do not die on a live pane.
- **G5 — Activity-recency operator gate.** `OperatorAttached` is replaced by an
  activity-recency signal that distinguishes an idle human terminal / remote-control client /
  monitor from an operator who is actively typing, so the captain is no longer parked
  warn-only under the remote workflow.
- **G6 — Gauge-independent recovery.** A live pane whose gauge is dead can still be recovered
  via a gated, fail-closed force-restart path — recovery does not depend on the gauge being
  alive.
- **G7 — Defaults pinned.** The warn / act / force token + percent thresholds are UNCHANGED.

---

## Non-goals (explicitly OUT of scope)

- **NG1 — No threshold/band retune.** ZERO changes to warn/act/force/window threshold
  constants. This is an operator HARD-NO. (See constraint C1.)
- **NG2 — No new abstraction layer over identity.** A change that *adds* a layer on top of the
  heuristic block without DELETING the block has NOT broken the pattern and is explicitly out
  of scope. The fix must be a net deletion.
- **NG3 — No daemon changes.** The daemon, the daemon supervisor, and the dispatch/merge
  pipeline are untouched. The keeper is per-session.
- **NG4 — No inject-step redesign.** The handoff → /clear → /session-resume inject mechanics
  (paste, nonce-confirm, /clear, resume) are NOT being redesigned — they fail ~0.5% of the
  time and are not the dominant failure. Touch them only where identity/liveness inputs feed
  them.
- **NG5 — No schema migration.** `.ctx` and `.managed` file formats are unchanged except that
  `.managed` is now authoritatively the launch-passed lowercase UUIDv4.

---

## Constraints

- **C1 — DEFAULTS-PIN (operator HARD-NO).** The threshold constants MUST NOT move:
  **Act 300k / 0.85**, **Warn 270k / 0.70**, **Force +40k / 0.95** (i.e. Force absolute =
  Act + 40k, Force pct ceil = Act + 0.10). The operator routinely sees a **27% warn** on the
  status line; this is **CORRECT-BY-DESIGN** — the absolute-token gate fires on a **1M
  window**, and `--act-pct` is **INERT** on that window. Band-widening to "fix" the 27%
  display is a **BLOCKING FAILURE**. A defaults-PIN test asserts these constants unchanged.
  (Source memory: "No keeper-band retune" — widening warn/act band burns tokens + degrades
  perf; the warn-nag is a SIGNAL.)
- **C2 — Single-writer invariant.** Exactly one `.managed` writer, one gauge writer;
  `WriteManagedSessionFn` called once at boot.
- **C3 — Authoritative identity, never scraped.** Identity comes from the launch SID, read
  back from `.managed` (lowercase UUIDv4). The launch SID-mint mechanism is
  `SID="$(uuidgen)"` in `scripts/captain-tools/captain-launch.sh` (the repo source-of-truth);
  it must NEVER be re-derived from any copy, the gauge, or a transcript filename.
- **C4 — Per-session deploy.** Deploying a keeper change requires (a) `go install ./cmd/harmonik`
  AND (b) relaunching keeper-watched sessions so launchers pass the new `--session-id` flag.
  The daemon supervisor does not auto-revive keepers.
- **C5 — Build/test isolation.** The integration tier runs against a real throwaway tmux pane
  (`hksav-twin-`), behind `//go:build integration`. NO daemon. NO 30-minute scenario budget.
- **C6 — Net-LOC-down.** Identity machinery LOC must decrease. This is a verifiable gate, not
  a guideline.

---

## Success criteria (concrete + verifiable)

- **SC1 — Authoritative bind.** `harmonik keeper doctor --agent <name>` reports
  `BoundSessionID == <the UUIDv4 minted at launch>` and `.managed` contains that lowercase
  UUIDv4 — NOT a value scraped from a transcript filename or the gauge. (verifiable: doctor
  output + `.managed` content)
- **SC2 — Single write.** A unit test asserts `WriteManagedSessionFn` is invoked exactly ONCE
  over a full warn→act→clear→resume cycle (boot only); the auto-clear and re-latch call sites
  no longer exist. (verifiable: call-count assertion + grep for deleted call sites)
- **SC3 — Net LOC negative.** `git diff` shows `internal/keeper/watcher.go:664-888` heuristic
  block, the named config knobs, and `cmd/harmonik/keeper_cmd.go` `rebind` command DELETED;
  net identity-machinery LOC is negative. (verifiable: diff line accounting + the deletion
  checklist in 05-spec-drafts/keeper-identity-and-liveness.md)
- **SC4 — Defaults unchanged.** A defaults-PIN test asserts Act 300k/0.85, Warn 270k/0.70,
  Force +40k/0.95 are byte-for-byte unchanged. (verifiable: constant-assertion test, RED if
  any constant moves)
- **SC5 — Live gauge through idle + clear.** In a LIVE-SOAK by an INDEPENDENT verifier, a
  stale-gauge-on-live-agent is reproduced, then the gauge writer keeps `.ctx` fresh through a
  **>5-minute idle window** AND across a **/clear**. (verifiable: manual soak; gauge mtime
  stays current; `.ctx` survives /clear with equal occupancy capture before+after)
- **SC6 — No false operator-suppress.** Over a real warn→act→clear→resume cycle with an idle
  / remote-control client attached, `harmonik subscribe --types session_keeper_*` shows
  `cycle_complete` with NO gauge-NA stall and NO operator-attached false-suppress.
  (verifiable: event trace)
- **SC7 — Acceptance gate flips.** The extended `cycle_twin_e2e_integration_test.go` is RED on
  today's `main` and GREEN only after all P0/P1 beads land. (verifiable: RED-then-GREEN)

---

## Affected spec areas

- **Keeper identity & liveness** — the new normative spec (this work's primary draft):
  `specs/keeper-identity-and-liveness.md` (drafted here as
  `05-spec-drafts/keeper-identity-and-liveness.md`).
- **Keeper thresholds / gauge** — the existing keeper threshold + gauge contract; touched only
  to RESTATE the defaults-PIN (no value changes).
- **keeper CLI surface** — `harmonik keeper {enable|doctor|set-dispatching|clear-dispatching}`
  flag-parity (`--agent` flag vs positional); the `rebind` subcommand is REMOVED.
- **keeper hooks** — `keeper-stop-hook.sh` / `keeper-precompact-hook.sh` env-var unification
  onto `HARMONIK_AGENT` (retire `HARMONIK_KEEPER_AGENT`).
- **Launch path** — `scripts/captain-tools/captain-launch.sh` (UUIDv4 mint, source-of-truth)
  and crew `crewstart.go resolveSessionID` thread the SID into the keeper launch.

---

## Translations glossary (codes → plain English)

- **keeper** — per-session context-fill watcher that handoffs/clears/resumes a Claude session
  before its context pane overflows.
- **gauge / `.ctx`** — the file the keeper reads to learn current context occupancy.
- **`.managed`** — the file holding the session_id the keeper is bound to.
- **latch / auto-clear / flap / suppress** — the heuristic identity branches being deleted.
- **OperatorAttached** — current raw "is a tmux client attached?" check being replaced with
  activity-recency.
- **UUIDv4 / UUIDv7** — v4 = interactive captain/crew session; v7 = daemon-spawned implementer.
- **defaults-PIN** — the operator HARD-NO that threshold constants must not move.
- **net-LOC-down** — the proof gate: identity machinery must shrink, not grow.
