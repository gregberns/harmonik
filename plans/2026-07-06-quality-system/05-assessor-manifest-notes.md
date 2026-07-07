# Assessor manifest — authoring notes (2026-07-06)

Authored the `assessor` agent-manifest per SYNTHESIS §4/4b/4c. Files created (NOT wired into any
launcher; no instance spawned):

- `.harmonik/agents/assessor/soul.md` — identity (I am / I do / I do NOT / I escalate to admiral).
- `.harmonik/agents/assessor/operating.md` — wake ritual, merge-gate (LT+XT+CR), deploy-gate (GATE-0),
  corpus growth, verdict+self-terminate, skills, bounds.
- `.harmonik/agents/assessor/manifest.yaml` — schema per `internal/agentmanifest/manifest.go`.

Validation: `harmonik agent check assessor` → `ok`. `harmonik agent brief --agent assessor --override`
composes cleanly (parent-intent graft = admiral).

## manifest.yaml key structure used
Same top-level keys as admiral/captain/crew/watch (nothing invented):
`type · cardinality{min,max} · harness · identity{soul,parent_intent} · context[]{ref,as,presence} ·
triggers[]{id,source,enabled,...} · handoff{channel} · keeper{thresholds} · lifecycle{self_restart} ·
tools_dir · markers{never_emits[]}`.

Values chosen:
- `cardinality: {min: 0, max: n}` — 0..n, crew-shaped (per-epic, self-terminating), matches `crew`.
- `parent_intent: admiral` — reports UP the admiral chain (admiral = gate authority).
- `harness: claude` — CR/XT judgment work; matches every existing manifest.
- `context`: boot(skill,injected) · operating.md(instruction,injected) · agent-comms(skill,injected) ·
  beads-cli(skill,retrieved) · scripts/scratch-daemon.sh(doc,retrieved). Deliberately EXCLUDES
  harmonik-dispatch + orchestrator-rules — the assessor does NOT dispatch fleet work.
- `markers.never_emits: [queue_submit:main, crew_start]` — same guardrails as `crew`.

## Fields I was UNSURE about (flag for reviewer / admiral)
1. **triggers** — used two: `{id: spawn, source: manual}` (admiral spawns per-epic) + `{id: gate,
   source: comms}` (receives the gate directive / stays on the bus). `manual` is a valid source per
   the loader enum (queue|cron|interval|event|comms|manual|operator), but no existing manifest uses
   `manual` — crew uses `queue`, watch uses `event`. If the launcher keys spawn off a specific source,
   this may need adjusting. **Verify against how the spawner reads triggers.**
2. **lifecycle.self_restart: true** — set true so a *keeper* mid-gate restart re-hydrates (crew-parity).
   BUT the assessor *deliberately* self-terminates after posting its verdict. Semantic tension: does
   `self_restart: true` cause the supervisor to relaunch it into a loop after that intentional exit?
   If the runtime treats deliberate `/quit` == crash, this should be `false`. **Needs admiral/runtime
   confirmation of exit-vs-restart semantics.**
3. **context — no crew-launch skill.** crew is keeper-rehydratable via `crew-launch`; I did NOT include
   it because that skill is queue-loop-scoped and would mislead. The re-hydration ritual is instead
   inlined in operating.md §"On wake". If a shared boot/keeper skill is expected in context, add it.
4. **scratch-daemon reference as `doc`.** `scripts/scratch-daemon.sh` is a script, referenced with
   `as: doc, presence: retrieved` (path-bearing literal, resolves from repo root). If a testbed doc
   (e.g. a future `docs/.../daemon-testbed-design.md`) becomes the canonical reference, swap it in.

## How an assessor is spawned (the intended path — NOT yet wired)
1. Crew posts `--topic gate` when its epic branch is fully closed.
2. Captain verifies + relays to admiral.
3. Admiral spawns an assessor ON the branch (crew-start-style launch: `harmonik agent brief --agent
   assessor` composes the boot doc; a mission handoff carries `{branch, epic_id, gate}`).
4. Assessor runs LT+XT+CR (merge-gate) or the isolated e2e (deploy-gate), files `found-by:assessor`
   beads, posts PASS/BLOCK on `--topic gate`, self-terminates.
5. Admiral holds the single human epic→main PR + the deploy decision until PASS.

Lane-2 small merges stay agent-light (scheduled `fast-follow.sh`) — no assessor spawn.

## Open questions for the admiral
- Confirm the trigger source(s) the launcher expects for an admiral-spawned, per-epic crew member
  (manual vs comms vs a new source) — see flag #1.
- Confirm exit-vs-restart semantics so `self_restart` is correct for a self-terminating agent (flag #2).
- Confirm the mission-handoff schema for `{branch, epic_id, gate}` (mirrors the crew C3 mission schema?).
- Confirm whether the deterministic block should filter `found-by:*` (any source) or `found-by:assessor`
  only — SYNTHESIS §4 says the block is "the set of open P0/P1 `found-by:*` beads on the branch"; I
  encoded `found-by:*` for the block, `found-by:assessor` for what the assessor itself files.

## Independent review (verdict)

Reviewer: fresh-eyes agent (did NOT author). Ground truth: `internal/agentmanifest/manifest.go`
(schema + validator), `brief.go` (boot-doc renderer), `check.go`, `internal/watch/markers.go`
(the only `never_emits` consumer), `cmd/harmonik/agent.go`. Reference manifests: admiral, captain
(via crew), watch, crew.

**Verdict: APPROVE** — no defects; no edits applied.

### Schema conformance
Every key + enum the manifest uses is real and correctly typed against `manifest.go`:
`type/cardinality{min,max=n}/harness/identity{soul,parent_intent}/context[]{ref,as,presence}/
triggers[]{id,source,enabled}/handoff{channel}/keeper{thresholds}/lifecycle{self_restart}/
tools_dir(null)/markers{never_emits[]}`. `as ∈ {instruction,skill,doc}` ✓, `presence ∈
{injected,retrieved,embodied}` ✓ (validator L229-234), `source ∈ {…,manual,…}` ✓ (L231-234).
`harmonik agent check assessor` → `ok`; `harmonik agent brief --agent assessor --override` → exit 0,
composes with parent-intent graft = admiral. All refs resolve: `boot` (`_skills/boot`),
`scripts/scratch-daemon.sh` (exists, path-bearing from repo root).

### Structural comparison (crew-shaped) — PASS
`cardinality {0,n}` matches crew; `parent_intent: admiral` (correct — admiral is the gate authority,
not captain); `lifecycle.self_restart: true` and `markers.never_emits: [queue_submit:main, crew_start]`
match crew exactly. Context correctly EXCLUDES `harmonik-dispatch` + `orchestrator-rules` (assessor
does not dispatch fleet work) — verified those two appear in crew/watch context but not here.

### The 4 flags — resolved with evidence
1. **`triggers.source: manual`** — VALID and safe. `manual` is in the loader enum
   (`manifest.go:233`, `allowedTriggerSource`). Critically, NO runtime keys spawn off trigger source:
   the only consumer of `Triggers` is `brief.go:74-77,364-376`, which merely *renders* the source as
   descriptive text into the boot doc. No spawner/supervisor reads it. `manual` is the semantically
   correct label for "admiral spawns per-epic." Keep as-is.
2. **`lifecycle.self_restart: true`** — CORRECT value. `SelfRestart` is consumed by NO runtime
   respawn loop anywhere in `internal/`+`cmd/` (only appears at its declaration, `manifest.go:119`).
   There is no supervisor that treats a deliberate `/quit` as a crash and loop-respawns off this field.
   `true` documents keeper-restart-in-place parity (restarted, not duplicated) for a mid-gate context
   refill — matching crew/watch/admiral. The end-of-gate self-terminate is a separate act, not governed
   by this field. No loop-respawn risk. Keep `true`.
3. **Omit `crew-launch`, inline rehydration in operating.md** — SOUND. Keeper rehydration is driven by
   the keeper skill + `/session-resume` re-reading operating.md via the brief, not by `crew-launch`
   (which is queue-loop-scoped and would mislead). The `## On wake (fresh start or keeper restart —
   same ritual)` block in operating.md covers it. Nothing breaks.
4. **`found-by:*` block vs `found-by:assessor` filing** — CONSISTENT with SYNTHESIS §4b (block = "open
   P0/P1 `found-by:*` beads on the branch"; assessor files under `found-by:assessor`). Conceptually
   sound: the union query includes assessor's own findings plus any other-source findings. NOTE (not a
   defect): `br list --label` does exact-match, not glob — `--label "found-by:*"` will NOT expand. At
   gate-time the assessor must enumerate the known `found-by:` sources via `--label-any` (or
   post-filter), not pass a literal `*`. This is an operating-detail the assessor resolves at runtime;
   the manifest/soul notation is correct.

### Charter fidelity vs SYNTHESIS §4b — PASS (no drift)
All six encoded faithfully: merge-gate (LT+XT+CR, soul + operating §Merge-gate); deploy-gate/GATE-0 +
24h reliability rule (operating §Deploy-gate step 2); corpus ownership+growth (soul + operating §Grow
the regression corpus); XT break-testing fan-out (operating §Merge-gate step 3); deploy-readiness
report (operating §Verdict step 1); PASS/BLOCK over comms `--topic gate` → admiral, then self-terminate
(operating §Verdict steps 2-3); independence-from-captain (soul "I do NOT / grade a lane I helped
build", operating §Bounds "Independence is load-bearing"). The admiral=authority / captain=builder /
assessor=executor split is respected (soul "I do NOT decide when the gate fires").

### Items for the admiral/operator (not blocking)
- Flag-4 note: define how the assessor enumerates `found-by:*` at gate-time given `br` has no label
  glob (use `--label-any` over the known sources). Encode in the gate mechanics when wired.
- Author's open questions remain valid to confirm at wire-up: mission-handoff schema for
  `{branch, epic_id, gate}` (mirror crew C3 schema); whether Lane-2 fast-follow ever needs an assessor.
- Manifest is authored but NOT wired to any launcher / no instance spawned — as intended for this phase.
