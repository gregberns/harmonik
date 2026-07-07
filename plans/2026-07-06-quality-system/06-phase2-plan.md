# Phase 2 — Deterministic Testbed — Build-Plan Hand-off
*admiral → captain · quality-system initiative · 2026-07-06 · PLANNING ONLY (no kerf works / beads / branches created here)*

> Grounding corrections vs the source plans (confirmed against the repo):
> - **Chunk 2 is NOT greenfield.** A substantial twin/scenario foundation already exists in-tree from
>   `remote-test-pyramid`: `cmd/harmonik-twin-{claude,codex,generic,session}/` (`scriptdriver.go` +
>   `scenarios.go` + `wire.go`), the `internal/scenario/` harness, spec `specs/scenario-harness.md` +
>   `specs/handler-contract.md §4.8.HC-036 / §4.6.HC-026a`, and the `twins/generic-twin` binary. Chunk 2
>   EXTENDS this seam — it must not rebuild it.
> - **No Dockerfile exists anywhere** → chunk 3's Layer-0 is genuinely greenfield.
> - Isolation primitive exists: `scripts/scratch-daemon.sh` + `scripts/smoke-scratch.sh`
>   (`guard_path` / `assert_not_supervised`).

## Phase-2 goal
Make the daemon's orchestration loop **deterministically and token-free testable** by building the Layer-1
scripted twin and the Layer-0 Docker substrate on top of the Phase-1 core-loop contract, then porting the
bug corpus into permanent regression scenarios. When Phase 2 lands, every incident class learned in
production becomes a repeatable green/red scenario that runs with zero Claude tokens.

## Entry precondition (must be TRUE before Phase 2 starts)
1. **`core-loop-proof` merged to `main`** — the live-verify acceptance matrix ({claude,codex,pi}×{local,remote})
   is green for the non-Claude rows, and it has **pinned down the exact protocol contract + assertion library**
   the twin must imitate (correct-harness / correct-model / HEAD-advanced / provider-reachable /
   `review.json`-fed-back / bead-terminal, asserted from the **event stream**, not stdout). Hard dependency for
   both chunks 2 and 3.
   - **Concrete go/no-go (make this testable, not a judgment call):** chunk 1 MUST land the assertion helper as
     a named, exported Go package at a stable path (chunk-1 names it — e.g. `internal/coreloop/assert` or
     wherever core-loop-proof puts it) with green CI. Phase 2 does not start until that package path exists and
     its tests pass — that existence+green is the precondition check, so the twin isn't built against a moving
     target.
2. **`remote-test-pyramid` closed/squared** — its twin+runner-seam foundation is already in-tree. Chunk 2
   EXTENDS this seam; it must not re-invent it.
3. **assessor gate live** — the `assessor` manifest (`.harmonik/agents/assessor/…`, already APPROVE'd) wired
   into a launcher and the deterministic `found-by:*` merge-gate/deploy-gate mechanics working, so each
   Phase-2 integration branch can actually be gated. (admiral's carry-in — see below.)

If (1) is not merged, Phase 2 does not start — chunks 2 and 3 both consume its assertion library.

---

## Chunk 2 — `scripted-twin` (Layer-1, serial after chunk 1)

**Purpose.** Turn the core loop from "live-verify only" into a **deterministic, token-free** regression
surface: a scripted stand-in injected where the daemon spawns `claude`, speaking the exact protocol chunk 1
nailed down.

**Concrete deliverables / artifacts.**
- Extend the existing twin seam, do NOT fork it: `cmd/harmonik-twin-claude/scriptdriver.go` + `scenarios.go`
  + `wire.go`, driven by twin-script YAML at `<fixture-root>/<scenario>/twin-scripts/<role>.yaml` (schema per
  `specs/handler-contract.md §4.8.HC-036a`, scripted-heartbeat carve-out §4.6.HC-026a).
- New scenario fixture: **concurrent same-file merge race** — 2 slots, two twins commit the SAME FILE, so the
  loser must fail safe `non_ff_merge`. Fixture lives at repo-root **`scenarios/<group>/<name>.yaml`** (NORMATIVE
  per `specs/scenario-harness.md` — the harness MUST NOT load scenarios from anywhere else; `internal/scenario/`
  is the Go harness package, NOT a fixture dir), with twin-scripts beside it at
  `<fixture-root>/<scenario>/twin-scripts/<role>.yaml`. Register the path in the §10.1 conformance floor
  (`internal/scenario/conformancecorpus_test.go`).
- **Twin injection seam (already exists — do NOT hand-roll):** inject the twin via `agent_overrides` in the
  scenario file (`specs/scenario-harness.md` SH-008–SH-011; SH-009 hash-check binding + SH-INV-003
  search-path-prefix guard). Point the crew at this rather than a new spawn hook.
- An assertion helper consuming chunk-1's event-stream assertion library (correct terminal transition,
  `non_ff_merge` on the loser).

**Acceptance criteria (tie to corpus).**
- Reproduces the **shared-file merge-race** class deterministically: two twins commit the SAME FILE → loser
  fails safe `non_ff_merge`, no corrupt merge, both beads reach a terminal transition. (This is the
  local-deterministic same-file class — NOT the remote worktree-CREATE race of hk-5qp7z/hk-lt091, which is a
  git-worktree-add HEAD-resolution race on a shared REMOTE worker repo and is out of scope for a local twin;
  it needs the remote substrate / multi-worker path, deferred.)
- Runs green against the **real daemon** with **zero Claude tokens**, repeatably across a `scratch-daemon.sh`
  clean reset; the daemon completes the run with **no special-casing** of the twin.

**Dependencies.** Chunk 1 (protocol contract + assertion library). **Runs concurrently with chunk 3.**
**Integration branch.** `epic/scripted-twin`. **Size.** **M**, splittable.

---

## Chunk 3 — `scratch-substrate` (Layer-0, PARALLEL with chunk 2)

**Purpose.** Give the daemon a **controllable, disposable substrate** — environment as a dial — so
environment-class incidents become reproducible and cross-test contamination under fleet load disappears.

**Concrete deliverables / artifacts (GREENFIELD — no Dockerfile today).**
- `Dockerfile` at repo root (or `deploy/testbed/Dockerfile`) — a disposable image the daemon runs inside,
  reusing the `scratch-daemon.sh` guard rails (`guard_path`, `assert_not_supervised`) so the production daemon
  is never touched.
- A launcher script, e.g. `scripts/substrate-up.sh` (naming to match `smoke-scratch.sh` conventions),
  delivering **clean reset** (identical disposable image per scenario) + the **disk dial** (quota/tmpfs sizing
  to force ENOSPC).
- **Disk-cache-wipe scenario** ported onto the substrate (the hk-5uezz / hk-44ab2 reaper-mid-build path — the
  shared go-build-cache wipe). Fixture at repo-root `scenarios/<group>/<name>.yaml`; register in
  `internal/scenario/conformancecorpus_test.go`.
- **CPU / network / clock dials stubbed as documented extension points only** (built out later in Phase-3
  `adversarial-corpus`) — do not build them here.

> **Platform note (do not overclaim fidelity).** The substrate image is Linux; production runs on the local
> Mac mini (darwin) — the harness already carries a `networksandbox_darwin.go` vs `_linux.go` split. The disk
> dial (shared go-build-cache wipe → reactive reap) is platform-portable, so chunk 3 validates the **disk-dial
> mechanism on Linux** and does NOT claim darwin-fidelity. Platform-sensitive dials (net/clock) stay Phase 3.

**Acceptance criteria (tie to corpus).**
- Reproduces **C5 env/disk** deterministically: the disk dial provably triggers the **reactive reap + clean
  retry** → `merge_build_failed` path recovers (hk-5uezz, hk-44ab2).
- Two back-to-back scenario runs from a clean reset show **zero cross-test contamination** (fixes today's
  flaky-scenario problem — value even before twins).
- Runs with **zero Claude tokens**.

**Dependencies.** Chunk 1. Independent of chunk 2. **Runs concurrently with chunk 2.**
**Integration branch.** `epic/scratch-substrate`. **Size.** **M**, splittable.

---

## Chunk 4 — `twin-replay` (Layer-1 replay, serial after chunk 2)

**Purpose.** Prove the twin is **faithful, not just plausible**: capture one real Claude Code session and
replay it deterministically through the twin seam at zero token cost.

**Concrete deliverables / artifacts.**
- **Capture recorder** — records a real Claude Code session (tool calls, timings, outputs, HEAD advances)
  into a **replay-fixture format** (NDJSON/YAML capture beside the twin-scripts, e.g.
  `<fixture-root>/<scenario>/twin-scripts/<role>.replay.ndjson`, aligned to the existing `wire_ndjson_test.go`
  wire format in `cmd/harmonik-twin-claude/`).
- **Replay driver** — a new mode in `scriptdriver.go` that plays a capture deterministically through the same
  seam.
- **One recorded-then-replayed fixture** proving fidelity.

**Acceptance criteria (tie to corpus).**
- A recorded real session replays deterministically and the daemon reaches the **same terminal outcome** as
  the live capture, **zero tokens on the replay run** (the one-time capture is the only token spend — gate it
  behind the same flag as chunk-1's Claude row).
- Exercises the **C8 real-Claude-worktree-startup** class in replay form where feasible (agent_ready gate
  captured once, replayed free) — partial coverage; full C8 stays a Phase-1 live concern.

**Dependencies.** Chunk 2. **Off chunk 2** — slots onto the twin crew as soon as chunk 2 merges, in parallel
with chunk 3. **Integration branch.** `epic/twin-replay`. **Size.** **M**, splittable.

---

## Failure-corpus port — what becomes deterministic in Phase 2

Phase-1 `core-loop-proof` already covers, **live**, the top-5 coverage gaps (C4 model-selection, C2
remote-vs-local, C6 provider wire-format, C7 queue-submit field-fidelity, C8 real-Claude startup). Those stay
**live** acceptance rows — Phase 2 does NOT re-implement them deterministically.

Phase 2 ports these corpus incidents into **deterministic** scenarios:

| Corpus incident | Cluster | Lands in chunk | Deterministic assertion |
|---|---|---|---|
| Shared-file merge race (local same-file class; NOT the remote worktree-create race hk-5qp7z/hk-lt091) | C3 | **scripted-twin** | loser fails safe `non_ff_merge`, no corrupt merge |
| Disk cache-wipe / reaper-mid-build (hk-5uezz, hk-44ab2) | C5 | **scratch-substrate** | disk dial → reactive reap + clean retry, not hard fail |
| Recorded real session fidelity (baseline for C1/C8) | C1/C8 | **twin-replay** | daemon reaches identical terminal outcome, zero replay tokens |

Deferred to **Phase 3 `adversarial-corpus`** (need twin perturbation overlays + chunk-3's CPU/net/clock
dials): reviewer-pane death / 8-min HB gate (hk-xkou8, hk-4hso5, hk-up1pk), stranded in_progress auto-reset,
concurrent-slot cold-start 150s agent_ready hold (hk-5z1f0), malformed `review.json` ErrMalformed salvage
(hk-vv10r), mid-flight cancel phantom-run, flagless-REQUEST_CHANGES wedge (hk-thbbv), rebase-dropped-commits
reopen (hk-vbv3b, hk-whru3). Named here only so the captain does NOT pull them into Phase 2.

---

## Concurrency / sequencing for the captain
Serial spine: **1 → (2 ‖ 3) → 5 → 6, with 4 off 2.** In Phase-2 scope:
- After chunk 1 (Phase 1) merges, staff **two concurrent crews**: one on `epic/scripted-twin` (agent seam),
  one on `epic/scratch-substrate` (container/dials) — disjoint surfaces.
- The moment `scripted-twin` merges, the twin crew picks up `epic/twin-replay` in parallel with the still-running
  `scratch-substrate` crew.
- Phase 2 ends when chunks 2, 3, 4 are each gated PASS and merged to `main`. Chunks 5–6 are Phase 3 — do not
  start them here.

## Rule reminders (every Phase-2 chunk)
- **Build-in-own-worktree + integration-branch → then main.** Each chunk = `epic/<codename>`; beads merge to
  that branch; `main` reached by **one human PR per chunk** after the assessor gate PASSes. Never cd into a
  worktree; never build on `main`.
- **Prefer the non-Claude fleet path** (pi / deepseek) under the token crunch. Layers 0–1 are plain tooling and
  **must not depend on Claude tokens to run**. Only token spend in Phase 2: chunk-1's flag-gated Claude row
  (Phase 1) and chunk-4's one-time capture (flag-gated).
- **24-hour rule.** Build with the **current live daemon binary**; a new build replaces the live daemon only
  **after** passing this system (assessor deploy-gate / GATE-0).
- **Isolation is proven, not assumed.** Reuse `scripts/scratch-daemon.sh` + `smoke-scratch.sh` guard rails; the
  production/fleet daemon is never stopped.
- **Assessor gate fires at each epic boundary** — crew posts `--topic gate` when the branch is fully closed →
  captain verifies + relays to admiral → admiral spawns assessor → PASS/BLOCK on open P0/P1 `found-by:*` beads
  (query with `--label-any` over the known `found-by:` sources; `br list --label` is EXACT-match, `found-by:*`
  does NOT glob-expand).

## What admiral still owns before hand-off
- **assessor must be LIVE from Phase 1** — wired into a launcher (not just an APPROVE'd manifest), with the
  deterministic `found-by:*` block query working (`--label-any`) and the mission-handoff schema settled.
- **assessor "GOOD ENOUGH" severity framework** (SYNTHESIS §4b TODO) — see `07-assessor-severity-framework.md`.
- **Confirm chunk-1 pinned the protocol contract** the twin imitates before releasing chunks 2/3 to the captain
  — if the assertion library isn't stable, the twin is built against a moving target.
- Admiral plans **Phase 3** (`adversarial-corpus` + XT fan-out + `chaos-generator`) while the captain builds
  Phase 2.
