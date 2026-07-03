# Research — Tooling: `.gitignore` + secret scan + dry-run + retry budget — Component C7 (Goals G8, G9)

> Pass 3 (`research`) of the `credfence` spec work. Covers the uncommittable-secret surface (gitignore + pre-commit secret scan, hk-pbs1u), the plan-only daemon dry-run mode (hk-cebjc), and the global retry budget on the review-loop / no_progress path (hk-c1ah6). Grounded in incident assessment §"Genuinely lost" #3/#7/#10 and the live repo + code (verified 2026-05-31). Planning artifact; does not modify `specs/` or repo config.

## Research questions

- **RQ1.** Is `.env` gitignored today, and what is the existing `.gitignore` structure the rule slots into?
- **RQ2.** Is there an existing pre-commit hook / secret-scan harness to extend, or must one be introduced?
- **RQ3.** Is there a precedent for a "dry-run / plan-only" mode in harmonik that the daemon dry-run should mirror?
- **RQ4.** What does the review-loop / no_progress path enforce today, and where does a GLOBAL retry budget attach (vs the existing per-bead caps)?
- **RQ5.** Where do the G8/G9 spec NOTES land (which spec carries the dry-run + retry-budget normative text)?

## Findings

### F1 — `.env` is NOT gitignored; `.gitignore` has a clean per-concern block structure to extend (RQ1) — CONFIRMS hk-pbs1u

- `git check-ignore .env` -> exit 1 (verified live; the assessment's claim holds). No `.env`/`*.env` rule in `.gitignore`.
- `.gitignore` is organized as labeled per-concern blocks (kerf state, Beads ledger, Go build outputs, twin-binary artifacts, harmonik runtime state, dev tools, orchestrator worktrees). The credential rule slots in as a new labeled block, e.g.:
  ```
  # Credential material — never commit (credfence / credential-isolation.md)
  .env
  *.env
  !*.env.example
  ```
  The `!*.env.example` allow-back keeps a committable template if one is wanted. **This is a one-block additive edit, no restructuring.**
- The incident's vector was a repo-root `.env`. Gitignoring it is what makes the sanctioned `supervise start` source (credential-isolation C3 / F5 there) safe to reuse.

### F2 — No pre-commit hook harness exists today; a secret scan must be introduced minimally (RQ2)

- `.git/hooks/` contains only the default `.sample` files (no active hooks). There is no `.pre-commit-config.yaml` and no `.githooks/` dir. So there is **no existing hook harness to extend** — C7 introduces one.
- **Scope-control (problem-space §3 "no new infrastructure"):** the secret scan should be the MINIMAL thing that works — a single committed hook script (e.g. `.githooks/pre-commit` + `git config core.hooksPath .githooks`, OR a documented `pre-commit` framework entry) that greps staged content for the credential deny-list KEY NAMES and known key shapes (`sk-ant-...`), and rejects the commit. It MUST reference the SAME deny-list constant as the scrub (credential-isolation F3 / C1 single-source-of-truth), and MUST NOT print the matched value. **Flag for design: choose hook mechanism (raw `core.hooksPath` script vs `pre-commit` framework). Lean: a single in-repo `core.hooksPath` script — zero external dependency, consistent with the repo's no-extra-infra posture.**
- This is the one C7 piece that is tooling/repo-config, not a spec edit. The SPEC text for it is a short normative note (which spec carries it: see F5).

### F3 — Dry-run precedent EXISTS twice; the daemon plan-only mode should mirror it (RQ3) — KEY FINDING

Two live precedents make the daemon dry-run (hk-cebjc) a pattern-match, not a novel mechanism:
1. **`harmonik queue dry-run`** (`cmd/harmonik/main.go:255-256`, `queuecli.RunQueueDryRun`): "Validate a queue submission without executing (daemon must be running)" (`main.go:219`). This already validates a *queue submission* without dispatching. The daemon plan-only mode extends this idea from "validate one submission" to "preview the full set of intended spawns (N implementers + N reviewers across M beads) without launching `claude` or reading the live key."
2. **Orphan-sweep dry-run** (`internal/daemon/orphansweep.go:63`, gated by `HARMONIK_SWEEP_CLAUDE_WORKTREES != "1"`): an env-gated "report, do not act" default. The pattern (env/flag selects report-vs-act; report mode touches no live resource) is exactly what the daemon dry-run wants.

**Design recommendation:** the daemon `--dry-run`/plan-only mode prints the intended spawn plan (per bead: would-launch implementer + reviewer at model X) and exits / idles WITHOUT (a) launching any `claude`, (b) reading the credential source, or (c) emitting any spend. It mirrors `queue dry-run`'s "validate without execute" and the orphan-sweep's report-vs-act gating. **This also serves the safety story: an operator can preview "this will spawn 26 sessions" before a live key is ever touched** (assessment §"Genuinely lost" #10). **Flag for design: flag vs env (`--dry-run` flag is more discoverable; `HARMONIK_*` env matches orphan-sweep). Lean: a `--dry-run` flag on the daemon run path, documented in operator-nfr ON-004 inventory.**

### F4 — Review-loop has per-bead caps but NO global retry budget; that is the G9 gap (RQ4) — CONFIRMS hk-c1ah6

Today's per-bead controls (NOT a global budget):
- `iteration_cap_hit` + `no_progress_detected` (`internal/daemon/reviewloop_test.go:359,473`; `reviewloop_feedback_inject_hk7x7ea_test.go`): the review-loop caps iterations PER BEAD and detects no-progress (same diff hash -> `no_progress_detected` -> `run_failed`). This bounds ONE bead's review loop.
- `operator-nfr.md:147` ON-002 confirms these are "run-level terminations, not daemon-level exits" — per-run, not global.
- The queue pauses on `fail_count:4` (assessment §"Genuinely lost" #7) — also per-queue-group, not a global spend cap.

**The gap (G9):** none of these is a GLOBAL re-dispatch/retry budget. Each retry is a full paid `claude` session; a stuck bead amplifies spend through repeated paid retries up to its per-bead cap, and across beads there is no aggregate ceiling. The assessment notes "hk-63oh.38 caps only Cat-3b reconciliation" — so a *reconciliation* retry budget exists, but not a *review-loop / dispatch* one.

**Design recommendation:** the global retry budget is best expressed as a **count that feeds the unified meter's max-runs ceiling** (cognition-loop C4 / F4 there) — i.e. re-dispatches and review-loop iterations COUNT toward `runsToday` / max-runs, so paid retries draw down the same finite budget rather than being free. This avoids a *third* budget surface (problem-space §4 "no third budget surface") and makes the retry budget a facet of C4's max-runs rather than a standalone mechanism. Alternatively a dedicated `max-redispatches-per-bead` global default. **Lean: fold retry counting into C4's max-runs (each implementer/reviewer/resume launch is a "run" that draws the budget); add an informative note in cognition-loop that retries are budgeted via max-runs.** This is the cleanest, single-meter answer.

### F5 — Where the G8/G9 spec NOTES land (RQ5)

- **Dry-run (G8):** the daemon mode is a CLI/daemon behavior; its NORMATIVE description belongs as (a) a short note in `cognition-loop.md` (problem-space §6 mapped C7 dry-run to cognition-loop) AND (b) an ON-004 config-inventory entry in `operator-nfr.md` for the `--dry-run` flag. The launch-spec is unaffected (dry-run means no launch spec is executed).
- **Secret scan + gitignore (G8):** these are repo-config/tooling, not a foundation-spec contract. The SPEC anchor is a short note in `credential-isolation.md` (the deny-list is the scan's input; "the deny-list keys MUST NOT appear in committed artifacts" is a credential-isolation invariant). The actual `.gitignore` edit + hook script are IMPLEMENTATION tasks (pass-7), not spec text.
- **Retry budget (G9):** an informative note in `cognition-loop.md` that retries/re-dispatches draw the max-runs ceiling (F4). No new requirement ID needed if folded into C4's max-runs; if a standalone cap is chosen, it is a new CL- sub-requirement.

## Patterns to follow

- **One additive `.gitignore` block** with `!*.env.example` allow-back (F1).
- **Minimal in-repo `core.hooksPath` secret-scan script** referencing the SAME deny-list constant; never print matched values (F2).
- **Mirror `queue dry-run` + orphan-sweep report-vs-act** for the daemon plan-only mode; preview spawns without touching the live key (F3).
- **Fold the retry budget into C4's max-runs** (single meter) rather than a third budget surface (F4 / problem-space §4).
- **Spec NOTES are short**; the gitignore + hook are implementation tasks, not spec contracts (F5).

## Risks / conflicts

- **R1 (scope-creep, F2).** A full secret-management / `pre-commit`-framework setup would exceed "no new infrastructure" (problem-space §3). Keep the scan to a single grep-based hook. Design must resist a pluggable scanner abstraction.
- **R2 (retry-budget double-counting, F4).** If retries draw max-runs AND a separate per-bead cap, a bead could be killed by either; design must state precedence (max-runs is the global backstop; per-bead `iteration_cap` is the local one; whichever fires first wins). No conflict, but needs an explicit ordering note.
- **R3 (dry-run completeness).** A plan-only mode that does not exercise the real dispatch path could drift from actual behavior (the plan says N spawns but live differs). Mitigation: dry-run reuses the SAME planning code path up to the launch boundary, only skipping the exec — mirrors `queue dry-run`'s "validate via the real validator" approach. Flag for design.
- **R4 (no conflict).** All C7 changes are additive (a gitignore block, a new hook, a new flag, an informative note). Nothing existing is reversed.

## Open questions carried to design

- NEW (F2/R1): hook mechanism — lean in-repo `core.hooksPath` script, no framework.
- NEW (F3): dry-run flag vs env — lean `--dry-run` flag + ON-004 entry.
- NEW (F4/R2): retry budget — lean fold into C4 max-runs; pin precedence vs per-bead `iteration_cap`.
