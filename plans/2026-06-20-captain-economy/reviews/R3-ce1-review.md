# R3 — Independent Review of hk-039z (CE1): captain skill lean + de-conflict + boot-digest

**Reviewer:** R3 (fresh-eyes, adversarial)
**Date:** 2026-06-20
**Branch:** `worktree-agent-a8a6a994955e7bb8f` (commit 8ccae3e6) vs main 3f60cf23
**Method:** read-only diff + grep verification of every implementer claim; no checkout/mutate.

## VERDICT: APPROVE-WITH-NITS

The change is correct, in-scope, self-consistent, and ships working scripts. No blocking
defects. Three nits and one scope-gap, all non-blocking, listed below.

---

## Verified claims (all PASS)

1. **Scope discipline — PASS.** Files touched: `captain/{STARTUP,SKILL,SHUTDOWN}.md`,
   `crew-launch/SKILL.md`, `keeper/SKILL.md`, `scripts/{captain-boot-digest.sh,
   crew-boot-digest.sh,README-boot-digest.md}`. **NO `.go` file touched.** **NO keeper
   threshold VALUE changed** — keeper SKILL lines 85-86/330-332 (270000/300000/340000)
   are unchanged by the diff; only flag SYNTAX (`--warn-pct`→`--warn-abs-tokens`) and
   positional→`--agent` were edited. The one out-of-set edit is exactly the admitted
   `crew-launch/SKILL.md:46` dangling-ref fix (`~/.claude/captain-tools/` → in-repo
   `scripts/crew-boot-digest.sh`) — benign, one line, matches the M4/M5 portability theme.

2. **M1 (band) — PASS.** No `--warn-pct`/`--act-pct` survives AS A CAPTAIN BAND
   INSTRUCTION. Residual pct mentions are all legitimate: keeper/SKILL.md flag-reference
   tables + the SHUTDOWN:195 `--act-pct 90` full-cycle SAFETY caveat (pre-existing,
   Phase-2 crew warning, not a captain arming instruction). Canonical
   `--warn-abs-tokens 200000 --act-abs-tokens 215000` used consistently across all four
   files. **Band value confirmed against the real source of truth:**
   `scripts/captain-tools/captain-launch.sh:55-56` defaults `CAP_WARN_ABS=200000 /
   CAP_ACT_ABS=215000` and arms them at line 117. Matches.

3. **M8 (`$HARMONIK_AGENT`) — PASS, variable is DEFINED.** All hardcoded `--from captain`
   command literals replaced with `--from "$HARMONIK_AGENT"`. Residual `--from captain`
   strings are all inside identity-guard prose ("an uncommissioned --from captain freezes
   the fleet"), not executable literals — except `major-issue-fanout/SKILL.md:69`, which
   is OUT OF SCOPE for this bead (not an edited file). **`$HARMONIK_AGENT` is reliably set
   in a captain session:** `captain-launch.sh:72/103` sets `-e "HARMONIK_AGENT=$CAP_NAME"`
   on the tmux session, and the `harmonik captain start` Go path does the same
   (`cmd/harmonik/captain.go:69`). Commands will NOT break.

4. **M2 (review-gate) — PASS.** The primary check is now a `reviewer_verdict`-per-
   `run_completed.run_id` join (STARTUP:301-318 bash loop). The top-level `workflow_mode`
   grep survives ONLY inside an explicit "do NOT do this — false GREEN" explanation, and
   correctly notes the field nests under `.payload.workflow_mode`. The `/loop 12m` tick
   (STARTUP:479) carries the same verdict-join, not the old grep. jq is syntactically
   valid (loop + `--arg`).

5. **M4/M5 (boot cost) — PASS.** STARTUP:98-107 adds an explicit "DO NOT full-read
   AGENT_INDEX/STATUS/TASKS at boot" block; the digest is MANDATORY ("the boot path, not
   an optional shortcut", STARTUP:117-130); the redundant inline raw-command blocks in
   Step 2 and Step 4 were DELETED and replaced by a single "reference only — do NOT re-run
   wholesale" list (net -156 lines). Both new scripts pass `bash -n`. Scripts reference
   only in-repo/HK_PROJECT-relative paths and degrade gracefully (jq-guarded, fallback to
   plain output, daemon-down tolerant). The keeper cheatsheet's referenced
   `scripts/captain-tools/keeper-restart-verified.sh` EXISTS and is tracked on-branch.

6. **No new contradictions / dangling refs — PASS.** §A reduced to the lane MODEL with a
   live cross-ref to `captain-lanes.md`; SHUTDOWN Step 5a/check-6 correctly re-pointed to
   write/read `captain-lanes.md` (which HAS the `active_lanes`/`operator_initiatives`/
   `parked`/`next_lane_pipeline` sections SHUTDOWN now names). keeper SKILL §407 reconciled
   to point at STARTUP Step 6 instead of the deleted §A snapshot. No "see §X" left dangling.

7. **No operator-directive regression — PASS.** project.yaml `forbidden_actions` still
   bans WIDENING the band; the skills now correctly say HARD-NO is widening-only and
   LOWERING (the 200k/215k band) is operator-directed — consistent with the 2026-06-19
   directive. Nothing contradicts the fill-every-lane scale-out posture.

---

## Nits (non-blocking)

- **NIT-1 (scope gap, MED):** The embedded asset mirror `cmd/harmonik/assets/skills/
  captain/{STARTUP,SKILL,SHUTDOWN}.md` was NOT updated. These templates were IN SYNC with
  the old `.claude/skills/captain/*` before this change (verified by diff) and are what
  `harmonik init` copies to freshly-initialized projects. They are now stale — the boot-cost
  and de-conflict improvements will not reach newly-init'd deployments. Arguably out of
  scope for a single doc bead, but worth a follow-up bead so the asset copy doesn't drift.

- **NIT-2 (path inconsistency, LOW):** `keeper-restart-verified.sh` and the boot-digest are
  referenced via the in-repo `scripts/...` path (portable), but `captain-launch.sh` is still
  referenced via the global `~/.claude/captain-tools/captain-launch.sh` in 3 spots
  (STARTUP:374, SKILL:719, SHUTDOWN:389) even though it ALSO exists in-repo at
  `scripts/captain-tools/captain-launch.sh`. Both paths exist on this box so nothing breaks;
  it's a residual inconsistency the portability theme didn't fully sweep.

- **NIT-3 (aspirational ref, LOW):** STARTUP:345/385 and the keeper cheatsheet cite
  "`.harmonik/config.yaml` `keeper:` block" as a band source of truth, but that file does
  not exist in the repo. `captain-launch.sh` (named first, authoritative) is the real and
  correct source. Harmless — framed as one of two possible sources — but the config.yaml
  half is currently fictional.

---

## Bottom line
Pure-doc + shell, no Go, no threshold-value change, `$HARMONIK_AGENT` verified live, all
12 M-issues addressed as claimed, scripts syntactically clean and portable. Safe to merge.
The one thing I'd open a follow-up bead for is NIT-1 (asset-mirror drift).
