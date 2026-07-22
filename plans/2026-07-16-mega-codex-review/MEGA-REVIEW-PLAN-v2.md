# Mega Codex Review — Unified Execution Plan (v2 · FINAL)

> Single execution plan merging the three input drafts:
> - `codex-mechanism-RESEARCH.md` — verified mechanism for driving Codex as a review lane.
> - `review-decomposition-DRAFT.md` — the review-unit decomposition across risk tiers.
> - `coverage-strategy-DRAFT.md` — per-subsystem coverage map + ranked gaps + M6 composition.
>
> **v2** folds every fix from both adversarial reviews (both APPROVE-WITH-CHANGES, no BLOCKs):
> `MEGA-REVIEW-PLAN-REVIEW-mechanism.md` (F1–F7) and `MEGA-REVIEW-PLAN-REVIEW-coverage.md` (#1–#7).
> See the **Review-resolution table** immediately below for finding→resolution mapping.
>
> Read-only planning artifact. No code changes. Execution is **gated** (see §1 sequencing constraint).

---

## 0. Review-resolution table

Every fix-list item from both adversarial reviews and how v2 resolves it. Package LOC / file claims
below were re-verified against the live tree (`ls internal/workspace/`, `ls cmd/harmonik/*.go`) on
2026-07-16.

### Mechanism / feasibility review (`…-REVIEW-mechanism.md`)

| # | Sev | Finding | Resolution in v2 |
|---|---|---|---|
| F1 | MAJOR | Original review read the token as `plan_type:"free"`; **operator-confirmed 2026-07-16 the Codex account is on a ChatGPT SUBSCRIPTION** (the "free tier" read was a stale/other-account token claim). Still: no *measured* headroom, and no mid-sweep abort protocol. | §2.1 reframed to "ChatGPT-subscription auth, headroom empirically pilot-verified"; §6 pilot is a **HARD go/no-go gate** measuring 429 threshold + reset window + fan-out multiplier (insurance regardless of plan); §2.6 adds a **mid-sweep checkpoint/abort/resume protocol**; §7 Q3/Q4 rewritten; fallback stated. |
| F2 | MINOR | `gpt-5.4-mini` "conserve subscription usage" is unverified and a category error (plan-quota isn't per-token) | §2.1 **drops** the "conserve subscription usage" rationale; mini is now a *pilot-gated, verify-first* option judged only by the F1 quota metric — dropped if quota is per-request. |
| F3 | MAJOR | Per-worker `forced_login_method="chatgpt"` is the durable backstop; env-unset is the weaker guard | §2.1 makes per-`CODEX_HOME` `forced_login_method="chatgpt"` **materialization MANDATORY**, driver-asserted fail-closed per chunk; env-unset demoted to secondary. |
| F4 | MAJOR | Missing/empty/unparseable findings file must be a FAILED chunk, not "zero findings" | §2.4 merge driver: only a **positively-parsed result object** counts as reviewed; file-absent/empty/non-schema-valid → CHUNK-FAILED (re-queue/surface). |
| F5 | MAJOR | Dedupe must GROUP, never DROP — over-merge discards a second distinct bug | §2.4 dedupe is now **non-destructive grouping**: cluster duplicates, retain every original record; confidence = cluster cross-lane membership. |
| F6 | MINOR | Cross-lane-agreement confidence degenerates for the ~15 single-lane units | §2.4 states **two confidence regimes**: BOTH-lane = cross-lane agreement; single-lane = severity + adjudication, **not** down-weighted for being single-lane. |
| F7 | MINOR | `--ephemeral` is on the plan's exec line but was NOT on the research's run | §2.1 exec line reconciled with the **actually-run** research invocation (no `--ephemeral`); `--ephemeral` moved to the pilot as an explicitly-verify-first flag paired with per-worker `CODEX_HOME`. |

### Coverage / completeness review (`…-REVIEW-coverage.md`)

| # | Sev | Finding | Resolution in v2 |
|---|---|---|---|
| 1 | CRITICAL | `internal/workspace` merge/conflict/lease core (~4,382 LOC) in NO unit; pkg is 6,465 LOC | **New RU-04b "Workspace lifecycle"** added, Tier 2, scheduled **before Wave 1 fires**; §2.7 retracts the "every package maps to a unit / no silent gaps" claim and replaces it with a verified manifest. |
| 2 | HIGH | `cmd/harmonik` file→RU mapping unspecified for ~3–4k LOC | §3 Wave 3 replaces "+ smaller top-level cmds" with an **explicit per-RU file manifest**, diff'd against `ls cmd/harmonik/*.go` at fire time; **eval-harness CLI** and **gate/verdict CLI** called out as named sub-buckets. |
| 3 | MEDIUM | reconcile/brcli false-close seam (fabricated done-status) split RU-12 (Claude) + RU-18 (Codex) across waves | **New RU-12x** pulls the reconcile-close + brcli-terminal-transition seam forward to Tier 1/2, **BOTH-lane**, sibling to runbridge. |
| 4 | MEDIUM | `twinparity` is M6 WS3-F1 code, not keeper | Moved out of RU-13 into the **§5 M6-verification track** with the explicit "does the equivalence library reject a mutated stream?" mandate (round-1 caught a load-bearing false-negative). |
| 5 | MEDIUM | RU-22 scenario-prune collides with already-landed M6 WS1.2/1.3 gate | §3 RU-22 rewritten: prune list **must be diff'd against what `test-scenario`/`check-full` now compile** at fire time; no deletion of files the new gate runs. |
| 6 | MEDIUM | Take a position on Q9 (RU-24 assets, RU-25 spec drift) | **Decision forced (§2.7 / §3 Wave 5):** RU-24 **includes** executable assets (shell scripts + templates); RU-25 **added** as a light spec-vs-code drift/coverage pass. Markdown-skill drift is **RESOLVED = DEFER (operator, 2026-07-16)** — out of this sweep's scope; it is the agent-config-reviewer's job (see §7 O1). |
| 7 | LOW | §4.5 mislabels hook/policy as "M6-covered"; apparent RU-14 contradiction | §4.5 retitled to separate "already-STRONG unit coverage — don't add tests" from "M6-harness territory," and notes RU-14 still does a **correctness read** even where coverage is left alone. |

**Preserved from v1 (both reviews affirm):** both top census hazards in Wave 1 (`runbridge.go` in RU-01;
tmux paste *landing* in RU-05); the ADD-NOW vs DEFER-TO-M6 split; the M6 non-duplication boundary rule;
the BOTH-lane budget escape hatch. The sequencing constraint (execution fires **after** giant-retirement
lands) is unchanged (§1).

---

## 1. Frame & goal

The operator wants a **thorough, program-wide code review** of harmonik (~202k prod LOC across
`internal/` + `cmd/`) that **also verifies test coverage lands in the right places** — a two-legged review:

- **Correctness leg** — find real bugs (concurrency/goroutine leaks, resource lifecycle, nil/error
  paths, spec drift, over-abstraction) across every subsystem, **with no silent gaps** — a claim v2 now
  backs with a verified package/file manifest (§2.7), after v1's version of it was shown false for
  `internal/workspace` and under-specified for `cmd/harmonik`.
- **Coverage leg** — for each unit, verify the tests exercise **real product behavior at the right
  seam**, not fakes/theater. The load-bearing question is *"does the green here mean the product
  behaves, or that a fake behaved?"* Statement coverage % is an **input, not the verdict**.

**Codex is a MANDATORY, non-negotiable review lane** (operator requirement). It runs alongside a
Claude lane; the highest-risk units get **both** lanes for adversarial cross-check.

### Sequencing constraint (operator ordering) — UNCHANGED

**Execution happens AFTER the giant-retirement lands.** The giant-retirement work
(`plans/2026-07-16-giant-retirement/` — boot-config + socket-router carves out of the daemon god-package)
restructures exactly the largest, highest-risk review targets (RU-01, RU-06). Reviewing them before
the carve lands would review code that is about to move. This plan is **staged and armed now**; its
fan-out fires once giant-retirement is merged. The chunk map (§2/§3) is re-validated against the
post-retirement tree at fire time (daemon/core sub-chunk boundaries in particular).

> The fire-time re-validation is also where RU-04b's boundary, the `cmd/harmonik` file manifest, the
> RU-22 prune list (vs the live `test-scenario` set), and the M6 shipped-vs-planned check all get
> reconciled. These are **pre-fire tasks**, not mid-sweep discoveries.

> As-built caveats (from `review-decomposition-DRAFT.md`): M4 (remote rebuild) and M1 (delete
> test-theater) have NOT landed; M2 (agent-input substrate) and M3 (run-state-machine) are partially in
> flight. The review sees the **current** tree — both the old ack-free channels AND the new protocol
> drivers are live and both must be reviewed.

---

## 2. Review lanes

Two producer lanes, one schema, a merge/dedupe step. Both lanes emit the **identical JSON finding
schema** (§2.3) so a unit reviewed by both yields a cross-checkable pair.

### 2.1 Codex lane (MANDATORY) — `codex exec`

**Mechanism (VERIFIED against codex-cli 0.142.5).** One `codex exec` invocation per chunk, model
pinned, read-only sandbox, JSON output schema. Codex **reads the files itself** inside the sandbox —
you do NOT stuff source into the prompt; you point it at a scope.

**Account reality (operator-confirmed 2026-07-16 — F1).** The Codex account on this box is on a
**ChatGPT subscription** (operator-confirmed). The earlier review's "free tier" read
(`chatgpt_plan_type:"free"`) was a **stale/other-account token claim** and is superseded — do not plan
against a free-tier ceiling. Codex *runs* fine (the research's `apptap` sweep succeeded). What is still
unmeasured is real **headroom**: a 55–65-chunk sweep at 40k–120k tokens/chunk fanned 4–6 wide is a
completely different load than one 17k-token smoke run, and a subscription's message/rate quota has a
window ceiling regardless of plan. **The pilot verifies real headroom/limits empirically before the
full sweep** (§6) — sweep throughput is a to-be-measured feasibility input, not a solved question.

**Exec line (reconciled with the actually-run research invocation — F7).** The research's verified,
end-to-end successful run did **not** include `--ephemeral`:

```bash
codex exec -m gpt-5.5 \
  --cd /Users/gb/github/harmonik \
  --sandbox read-only \
  --skip-git-repo-check \
  --output-schema schema.json \
  --output-last-message findings-<chunk>.json \
  "You are a senior Go reviewer. Review ONLY the files under <scope> for correctness bugs,
   resource leaks, concurrency hazards, error-handling gaps, and spec drift. Read the files
   yourself. You MAY read specs/, STATUS.md, docs/foundation/.../subsystem-organization.md for
   context. Return findings strictly matching the provided JSON schema; empty array if none."
```

- **Model:** `-m gpt-5.5` is REQUIRED — the built-in default (`gpt-5.6-sol`) is broken on 0.142.5
  (HTTP 400). `gpt-5.4-mini` exists in `models_cache` but **was never run** and its quota benefit is
  unverified: on a ChatGPT-plan account the meter is message/rate-quota, **not** per-token API dollars,
  so a smaller model may save nothing. The "conserve subscription usage" rationale is **dropped** (F2).
  The mini tier is a *pilot-gated* option only — used for leaf/skim packages **iff** the pilot confirms
  (a) it works and (b) it consumes less against the F1-established quota metric. If quota is per-request,
  drop the mini tier (it adds quality risk for no saving).
- **`--ephemeral` + per-worker `CODEX_HOME`** (the fan-out contention mitigation) are **verify-first in
  the pilot** (§6/§7 Q2) — the exact planned invocation with these flags must be shown to still emit a
  schema-valid file before the sweep carries the non-ephemeral run's success over to it (F7).

#### AUTH GUARDRAIL (load-bearing — do not skip)

**The Codex lane MUST run under ChatGPT-subscription auth with a CLEAN env. NO `OPENAI_API_KEY`, NO
`CODEX_API_KEY`.** A stray key silently bills the metered API pool — there was a real API-pool burn
incident (`project_flywheel_apikey_burn`).

**Two guards, ranked (F3).** The billing guard (`codexbillingguard.go:212 assertChatGPTPlan`) asserts
only (a) `forced_login_method = "chatgpt"` in `config.toml` and (b) `auth.json` has no populated
`OPENAI_API_KEY`. It does NOT inspect `chatgpt_plan_type` (subscription vs otherwise both pass). It
guards against *API-pool billing*, not against *quota/throttle* (that's the F1 headroom question). Of
the two guards:

1. **MANDATORY — durable backstop.** Every `CODEX_HOME` the sweep uses (shared or per-worker) **MUST
   contain `forced_login_method = "chatgpt"` before any `codex exec`.** The driver **materializes and
   asserts** this per chunk (mirror `runCodexBillingGuard` — reuse the posture/code) and **fails closed**
   if absent. This is what makes codex itself refuse to fall back to API-key login
   (`codexbillingguard.go:16-17`). A per-worker `CODEX_HOME` that copies `auth.json` but forgets
   `config.toml` re-opens the exact `project_flywheel_apikey_burn` failure — so config materialization is
   not optional.
2. **Secondary — current-shell hygiene.** `unset OPENAI_API_KEY CODEX_API_KEY` cleans only the *current*
   shell (a different shell / sourced profile can re-introduce a key), so it is the weaker guard and
   never the sole one.

Pre-flight per chunk: `codex doctor` + `auth.json`-key-empty check + config-materialization assert.
Run a `codex doctor` before any big fan-out.

### 2.2 Claude lane — sub-agents over the same chunk map

The Claude lane runs its own sub-agents (via this repo's Agent/orchestrator tooling) over the same
unit list, emitting the same schema. It is stronger on **spec-alignment, cross-subsystem reasoning, and
"is this over-built / is this real" judgment** — it carries the AGENT_INDEX/STATUS/skills context and
the `agent-reviewer` rubric (spec drift, unwanted-abstraction, bead/codename match). It owns the
architectural seams, the giant cross-cutting concerns, and **adjudication of Codex's single-lane
findings** (every Codex finding still needs confirmation before it becomes a bead).

### 2.3 Shared finding schema (both lanes)

Object with `findings[]`; each finding: `file, line, severity(critical|high|medium|low|nit), category,
title, detail, suggested_fix` (all required). `additionalProperties:false` is MANDATORY on every object
(OpenAI strict mode returns HTTP 400 without it). Add per-record `reviewer` (`codex`/`claude`) and
`chunk` fields. The schema mirrors the `ubs` `file:line:col + fix` shape the project already reads. Full
schema in `codex-mechanism-RESEARCH.md` §1.

### 2.4 Merge / dedupe (build fresh, ~100–150 lines)

A driver script (no existing "review orchestrator") that enumerates the chunk map, fans out `codex exec`
per chunk (cap concurrency 4–6), collects results, and clusters both lanes' findings. Three correctness
rules the reviews made load-bearing:

- **Failed chunk ≠ clean (F4).** Per chunk the driver distinguishes: **file-absent/empty →
  CHUNK-FAILED (re-queue / surface)**; **present-but-not-schema-valid → CHUNK-FAILED**; valid
  `{"findings":[]}` → genuine clean. It logs the codex exit code + `--json` token/turn telemetry per
  chunk so a truncated/compacted (shallow) review is visible. **No chunk counts as reviewed without a
  positively-parsed result object.** This prevents the single most dangerous outcome — a 429'd/400'd/
  crashed chunk reading as "clean" — i.e. a false-green inside a review whose entire purpose is hunting
  false-greens.
- **Group, never drop (F5).** Dedupe is **non-destructive clustering**, not deletion. Duplicates
  (fuzzy key: `file` + nearby `line` + normalized `title`) collapse into a **group** that **retains
  every original record** (both lanes, all line/title variants) under one ranked entry. Nothing is ever
  removed — only clustered — so a second distinct bug at nearby lines in a god-function (e.g.
  `workloop.go` @ 8098 LOC) is never silently discarded. The underlying records stay auditable; a human
  spot-check reviews the *groups*, not a lossy merge.
- **Two confidence regimes (F6).** (a) **BOTH-lane units** (RU-01/02/03/04/04b/05/08/09/12x): confidence
  = cluster cross-lane membership (cross-lane hit = high). (b) **Single-lane units** (~15 of them): rank
  by severity + adjudication only, and their findings are **NOT down-weighted merely for being
  single-lane** — there is no second lane to agree, and a miss by the sole lane is an unrecoverable gap,
  so "single-lane → low confidence" would mis-rank most of the codebase. If budget allows, promote one
  or two structurally-risky single-lane units to BOTH.

Findings file as beads (once the daemon is back on) or a `findings.md` table. Every merged Codex finding
still needs Claude-lane adjudication before it becomes a bead.

### 2.5 Lane assignment

- **BOTH (adversarial cross-check)** — highest-risk units where the census verdict is Rebuild or a live
  data-integrity bug: **RU-01, RU-02, RU-03, RU-04, RU-04b, RU-05, RU-08, RU-09, RU-12x**.
- **Codex (X)** — self-contained / protocol / mechanical / newly-written code: **RU-07, RU-09b, RU-11,
  RU-13, RU-14, RU-16b, RU-17, RU-18, RU-20, RU-21**.
- **Claude (C)** — structural/architectural judgment, cross-file reasoning: **RU-06, RU-10, RU-10b,
  RU-12, RU-15, RU-16a, RU-19, RU-22, RU-25**.
- **RU-23** grab-bag split across whichever lane has free capacity (each sub-package independent).
- **RU-24** executable assets fold into RU-16b's asset verbs (Codex); markdown-skill drift is DEFERRED
  (operator, 2026-07-16) to the agent-config-reviewer — out of this sweep (§7 O1).

### 2.6 Mid-sweep checkpoint / abort / resume protocol (NEW — F1)

Even on a ChatGPT subscription, the sweep runs against a **message/rate quota whose real headroom is
unknown until the pilot measures it**, so the driver must survive hitting a window limit *during* a wave
without corrupting the "reviewed" ledger (good insurance regardless of plan):

1. **Checkpoint on completion.** Each chunk that produces a positively-parsed result (§2.4) is recorded
   as DONE in a durable manifest (chunk-id → result-path + telemetry). This manifest is the resume unit.
2. **Detect sustained throttle.** On repeated 429s beyond the pilot-measured retry budget, the driver
   **stops** (does not spin on retries that a hard per-window quota cannot beat) and marks in-flight
   chunks as PENDING (never DONE, never "clean").
3. **Surface + abort cleanly.** Emit which chunks are DONE / PENDING / FAILED and the observed reset
   window, so the operator sees exactly how far Wave-N got.
4. **Resume.** A re-run reads the manifest and only re-queues PENDING/FAILED chunks — no double-billing
   of quota, no re-review of DONE chunks.

Fallback if the subscription quota cannot sustain the sweep (Codex is operator-MANDATORY, so the whole
plan's throughput depends on this lane): serialize the sweep and spread it across days, or upgrade the
plan (pilot's reset-window measurement tells us which). Stated here so it is a decision, not a surprise.

### 2.7 Coverage manifest — the "no silent gaps" claim, verified (NEW — coverage #1/#2)

v1 asserted *"every `internal/*` and `cmd/*` package maps to a unit."* That claim was **false** for
`internal/workspace` and **under-specified** for `cmd/harmonik`. v2 retracts the blanket sentence and
replaces it with two verified facts (checked against the live tree 2026-07-16):

- **`internal/workspace` = 6,465 prod LOC / ~31 prod files.** v1's RU-04 named only the 5 *remote*
  files (`remotematerialize.go`, `createworktree.go`, `reviewverdict.go`, `autostatusmarker.go`,
  `diffhash.go`). The remaining **~4,382 LOC** — worktree lifecycle, merge dispatch, three conflict-
  resolution modules + escalation, lease locking, WIP capture, interrupt state, crash evidence — was in
  **no unit**. Now covered by **RU-04b** (§3 Wave-1/2). This is exactly the git-concurrency +
  resource-lifecycle + terminal-decision surface the correctness leg exists to audit — not peripheral.
- **`cmd/harmonik` = ~26k prod LOC.** v1 left specific files to "+ smaller top-level cmds," orphaning
  coherent subsystems. §3 Wave 3 now carries an **explicit per-RU file manifest**, diff'd against
  `ls cmd/harmonik/*.go` at fire time so every top-level `.go` lands in exactly one RU, with the
  **eval-harness CLI** and **gate/verdict CLI** named as their own sub-buckets.

The only intentional exclusion is the markdown agent-skill drift pass (RU-24 markdown portion),
**DEFERRED (operator, 2026-07-16)** to the agent-config-reviewer — out of this code/coverage sweep
(§7 O1). Everything else — including embedded shell scripts/templates (RU-24 executable portion) and
spec-vs-code drift (RU-25) — is in a unit.

---

## 3. Risk-tiered execution schedule

Legend — Tier: **1** Critical · **2** High · **3** Medium · **4** Test-theater · **5** Supporting.
Lane: **C** Claude · **X** Codex · **BOTH** adversarial cross-check. Execute top-down; tiers may overlap,
but **Tier 1 is the first and highest-priority wave**.

### Wave 1 — Tier 1 (Critical). The ack-free channels + the god-function + the new spine + the fabricated-done-status seams. All BOTH-lane.

| ID | Unit | Scope | ~LOC | Lane |
|---|---|---|---|---|
| RU-01 | Daemon workloop / god-function | `workloop.go` (8098), `runbridge.go`, `dispatchsegment.go`, `stategather.go`, `runshell.go`, `eagerfill_em063.go` | ~10k | BOTH |
| RU-02 | DOT cascade + review/gate loop | `dot_cascade.go` (2668), `reviewloop.go` (2159), `dot_gate.go`, `verdictexecutor_rc025a.go`, `sub_workflow_runner.go` | ~6.5k | BOTH |
| RU-03 | Run-state-machine seam (M3) | `runexec` (1158), `mergeq` (146), `run` (128), `runexectest` (719) | ~2.2k | BOTH |
| RU-04 | Remote / SSH substrate | `remotematerialize.go`, `createworktree.go`, `reviewverdict.go`, `autostatusmarker.go`, `diffhash.go`, `workers` (2139), the 92 `runner==nil/!=nil` dual-path sites | ~5k | BOTH |
| RU-05 | tmux input channel (old, still live) | `tmuxsubstrate.go` (2769), `pasteinject.go` (2671), `lifecycle/tmux/` (5562), `keeper/tmuxresolve.go` — **SPLIT** RU-05a (daemon files) / RU-05b (`lifecycle/tmux/`) | ~11k | BOTH (split) |
| RU-12x | **Fabricated-done-status close seam (NEW — coverage #3)** | reconcile-close path in `internal/lifecycle` (the `noChange`-subsumption close that fabricated `hk-2hfyt`; Class B fires ~83×/session) **+** brcli terminal transition (`brcli/terminaltransition_bi010.go`, `intentlogwrite.go`) | ~2k | BOTH |

**RU-12x rationale (coverage #3).** `runbridge.go` (fabricated done-status; hk-2hfyt closed with fix
absent) is correctly in RU-01/Tier-1/BOTH and ADD-NOW #1. The **same bug class** — a close path that can
*fabricate done-status* — lives in the **reconcile** path (RU-12, Claude-only, Tier 2) and the **brcli
terminal-transition** path (RU-18, Codex-only, Tier 3). Splitting one live data-integrity bug across two
single lanes and two waves is internally inconsistent with cross-checking runbridge for exactly this. v2
**pulls both halves forward** into RU-12x (Wave 1, Tier 1, BOTH), sibling to runbridge. RU-12 and RU-18
retain the rest of their scope; the close/terminal-transition seam is carved out to RU-12x.

Top-level ordering within Wave 1: RU-01 → RU-04 → **RU-04b** → RU-05 → RU-12x → RU-03 → RU-09
(Tier-2, pulled forward for the live lost-update) → RU-08/RU-07.

### Wave 2 — Tier 2 (High). New M2 code, data-integrity, workspace lifecycle, big Simplify packages.

| ID | Unit | ~LOC | Lane |
|---|---|---|---|
| RU-04b | **Workspace lifecycle — merge/conflict/lease/worktree (NEW — coverage #1)** | ~4.4k | BOTH |
| RU-09 | Queue subsystem (`rpc.go` two-writer path, `HandlerAdapter`) | ~4.5k | BOTH |
| RU-08 | Substrate / Handler contract seam (`substrate`, `handler`, `handlercontract`) | ~9.4k | BOTH |
| RU-07 | Codex substrate vertical, M2 new (`codexdriver`/`codexinput`/`codexreactor`/`codexwire`/`codexdigitaltwin`/`codextest`) | ~8k | X |
| RU-06 | Daemon god-struct + composition root (`daemon.go` 85-field `workLoopDeps`, `projectconfig.go`, `socket.go`, …) | ~11k | C |
| RU-10 | Core event registry surface (`eventreg`, `pertypecompat` dead table, `eventtype.go`) | ~5k | C |
| RU-11 | Event bus (`busimpl.go`, `jsonlwriter.go`) | ~2.2k | X |
| RU-12 | Lifecycle sweeps + reconcile (startup/orphansweep/stalewatch/draindetect) — **reconcile-close seam carved out to RU-12x** | ~9k | C |
| RU-13 | Keeper (`watcher.go`, `step.go`, `cycle.go`, `keepertwin`) — **`twinparity` MOVED to §5 M6-track (coverage #4)** | ~8k | X |

**RU-04b scope (~4,382 LOC, verified file-by-file against `ls internal/workspace/`):**
`agenttask_chb028.go` (649), `conflictresolution_wm024.go` (353), `claudetrust_wm040b.go` (425),
`orphansweep.go` (253, workspace's own), `claudesettings_wm040a.go` (404), `leaselock.go` (253,
cross-process mutex primitive), `workspace.go` (246), `sessionmetadatasidecar_wm063.go` (218),
`discoverworktrees.go` (217), `gitignorehygiene.go` (215), `mergedispatch_wm018a.go` (208),
`interruptstate_wm040.go` (176), `implementerref_wm022.go` (149), `conflictescalation_wm023.go` (136),
`wipcapture_rc019.go` (132), `crashevidence.go` (120), `lookupworkspace.go` (92),
`conflictresolution_wm022a.go` (65), `integrationbranch.go` (45), `taskbranch.go` (26), plus
`worktreepath.go`/`refname.go`/`sessionlogdir.go`/`gitversion.go`/`errors.go`/`doc.go` residue not in
RU-04. **BOTH-lane** because `mergedispatch`/`leaselock`/`conflictresolution` are terminal git
decisions + a cross-process lock — data-integrity-adjacent, the exact concurrency + git-resource-
lifecycle class the correctness leg is chartered for, and a different failure class than RU-04's
SSH/ack-free transport lens. **Scheduled before Wave 1 fires** as a boundary task (§1); executes in
Wave 2 (or pulled into Wave 1's tail if capacity allows).

> RU-06 overlaps giant-retirement's socket-router + boot-config carves — review the **post-retirement**
> shape.

### Wave 3 — Tier 3 (Medium). CLI, workflow engine, adapters, skim units.

**`cmd/harmonik` explicit file manifest (coverage #2).** Every `.go` under `cmd/harmonik/` maps to exactly
one RU below. This manifest is **diff'd against `ls cmd/harmonik/*.go` at fire time** and any new file is
assigned before the wave runs. Two coherent subsystems that v1 orphaned under "+smaller cmds" are named
as their own sub-buckets:

- **Eval-harness CLI (~1.2k, RU-16b):** `eval_cmd.go` (448), `eval_metrics_cmd.go` (387),
  `eval_report_cmd.go` (236), `eval_guardrails_lygpp.go` (93).
- **Gate / verdict CLI (~2.3k, RU-16a — this is the review-gate control surface itself):**
  `decisions.go` (851), `decisions_k4.go` (538), `confirm_verdict.go` (229), `veto_verdict.go` (172),
  `write_review_verdict_cmd.go` (154), `greenlight_cmd.go` (125), `goalkeeper_cmd.go` (216).

| ID | Unit | Scope / file manifest | ~LOC | Lane |
|---|---|---|---|---|
| RU-09b | Queue CLI | queue verbs | ~3.9k | X |
| RU-10b | Core type/payload defs (skim — classification, not line-audit) | — | ~26k | C (skim) |
| RU-14 | Hook system (M5 seams: `hook`/`hooksystem`/`hookrelay`/`policy`/`orchestrator`) | — | ~2.5k | X |
| RU-15 | Workflow graph engine (`workflow`, `workflow/dot`, `workflowvalidator`, `goalstate`) — over-abstraction | — | ~5k | C |
| RU-16a | CLI — comms + core + **gate/verdict control surface** | `comms.go`, `main.go`, `run.go`, `run_via_daemon.go`, `harness.go`, `subscribe.go`, `handler.go`, `graph.go`, `state_cmd.go`, `substrate_select.go`, `smoke.go` + **gate/verdict CLI bucket above** | ~9k | C |
| RU-16b | CLI — keeper/init/assets/lifecycle verbs + **eval-harness** | keeper/init/asset/crew/captain/start/digest/schedule/sleepwake/sentinel_cmd/ops_monitor_cmd/dashboard_cmd/reconcile/migrate_rc_prefix_cmd/branch_reap_cmd/release_cmd/usage/watcherreap/project_hash + **eval-harness bucket above** + **RU-24 executable assets** | ~9k | X |
| RU-17 | Supervise + daemon lifecycle | `supervise_cmd.go`, `workers_boot.go`, `promote_cmd.go`, `remote_control_prefix_cmd.go` | ~4.5k | X |
| RU-18 | Beads adapter (`brcli`) — **terminal-transition seam carved out to RU-12x** | remaining brcli surface | ~3.5k | X |

> At fire time, `ls cmd/harmonik/*.go` is diff'd against RU-16a/16b/17's manifests; any unassigned file
> is a fire-blocker for Wave 3, resolved before the wave runs. No file falls through "+smaller cmds."

### Wave 4 — Tier 4 (Test-theater). Classification passes (keep-vs-delete), not line-audit.

| ID | Unit | ~LOC | Lane |
|---|---|---|---|
| RU-20 | Test theater — `operatornfr` (classify; still LIVE in default suite) | ~14.7k | X (classify) |
| RU-21 | Test theater — `specaudit` (classify; 129/132 already `//go:build specaudit`-tagged, 3 stragglers) | ~37.9k | X (classify) |
| RU-22 | Scenario harness + integration — **prune list gated on live `test-scenario` set (coverage #5)** | ~13k | C (classify) |
| RU-19 | Twin harnesses (skim) | ~16k | C (skim) |

**RU-22 rewrite (coverage #5).** v1 said "keep the harness + ~11 real files, prune the ~37 structural-
corpus files." But **M6 WS1.2/1.3 already landed** (COORD c054, commit `4caa9822`): `check-full` now
delegates its scenario line to `test-scenario`, which **includes `./internal/daemon/...`** and makes the
remote-substrate E2E a real gate. Pruning scenario files the newly-wired `check-full` compiles/runs would
be a **regression**. So: RU-22's prune list **must be diff'd at fire time against exactly what
`test-scenario` / `check-full` now pull in** — nothing the live gate compiles or runs is deleted. This is
the RU-specific instance of the general "re-check which WS merged" rule (§5).

### Wave 5 — Tier 5 (Supporting) + assets + spec-drift.

| ID | Unit | ~LOC | Lane |
|---|---|---|---|
| RU-23 | Supporting grab-bag (`agentmanifest`, `apptap`, `branching`, `cognition`, `crew`, `dashboard`, `digest`, `presence`, `release`, `replay`, `schedule`, `sentinel`, `sessiondata`, `structuredlog`, `usage`, `watch`, `testhelpers`, probes) | ~18k | split C/X |
| RU-24 | **Embedded assets (coverage #6, Q9 resolved).** `cmd/harmonik/assets/` — **4 shell scripts (~395 LOC) + 4 `.tmpl` templates** are executable product code (a scaffold-script bug is a real runtime bug) → **IN SCOPE**, folded into RU-16b's asset verbs. The **15 skill `.md` files** (agent-behavioral contract) → **DEFERRED (operator, 2026-07-16)** to the agent-config-reviewer, out of this sweep (§7 O1). | ~0.4k code + 15 md | X |
| RU-25 | **Spec-vs-code drift pass (coverage #6, Q9 resolved).** A light **cross-cutting** pass: walk `specs/`, confirm each normative `MUST` has a live implementation. Per-unit spec-drift (in the Codex/Claude prompts) only catches drift *inside a reviewed file* — it cannot catch an **orphaned spec** or a normative clause **never built**. This inventory-direction check is the highest-leverage cross-cutting pass for a spec-first repo and is NOT covered by reading `specs/` "as input." | cross-cut | C |

**Q9 decision (coverage #6).** The operator said *the whole system*. v2 **forces the call**: executable
assets (RU-24 scripts + templates) and the spec-drift pass (RU-25) are **IN**. The **markdown
agent-skill drift** portion of RU-24 is **RESOLVED = DEFER (operator, 2026-07-16)** — it belongs to the
agent-config-reviewer (which fires on skill/config drift), not this code/coverage sweep (§7 O1).

---

## 4. Coverage arm

For **every** review unit (including RU-04b, RU-12x, RU-24, RU-25), the reviewer runs the checklist from
`coverage-strategy-DRAFT.md` (seam check → theater check → skip-to-green check → high-risk-path check →
number sanity) and emits, per sub-unit: `{STRONG|WEAK|THEATER|FAKE-ANCHORED|MISSING} · evidence
file:line · MUST-fix? y/n`. A unit **fails the coverage gate** if any MUST check fails. Coverage % is
recorded but never the verdict.

### 4.1 The through-line

Every place that is **ack-free or terminal-decision** (runbridge, tmux landing, ssh transport, live
input-ack, **and now the reconcile/brcli close seam — RU-12x**, **and the workspace merge/lease seam —
RU-04b**) is exactly where coverage is fake-anchored or missing — because those are precisely the paths a
unit test with a fake *cannot* prove. That is the case for M6's controlled harness (§5), not a thing to
patch with more unit tests.

### 4.2 ADD-NOW (pure-logic gaps — the review can call for a direct unit test now)

Ranked by risk:
1. **`runbridge.go` — dedicated bridge test (HIGHEST VALUE).** Shell-event → machine-event → `br`
   close/reopen mapping; the seam where the census caught a *fabricated* done-status (hk-2hfyt closed with
   the fix absent). Currently rides entirely on `beadRunOne` @ 63.7%; no `runbridge_test.go` exists. Fast
   unit test with existing `substrate.Twin` + `FakeClock`. → RU-01. (Sibling seam RU-12x gets the same
   fabricated-done-status scrutiny on the reconcile/brcli side.)
2. **tmux adapter argv/exec tests** for `SendKeysEnter`, `SendKeysQuit`, `CapturePane`, `WriteToPane` (no
   adapter-level test today). Same fake-binary technique as `osadapter_test.go`. Closes an untested-
   product gap (does NOT prove *landing* — that is M6/WS3). → RU-05.
3. **`EmitWorkerOfflineEvent` real-body test** — replace the field-echo theater in
   `remote_substrate_b11_test.go` with a real-emit test via a recording bus. → RU-04.
4. **`runshell.go::fireOnCancel` (0%)** + daemon 0-25% funcs touching terminal/merge
   (`maybeEmitEpicCompleted` 25%, `runMergeFmtCheck` 21%). → RU-01.
5. **`agentlaunch/SpawnKeeperWindow` body** (untested). **Note (coverage "gets right"):** its body spawns
   a tmux window, so a "body test" needs the same **fake-`tmux`-binary** technique as #2 — it is still
   legitimately ADD-NOW, but call the fake-binary dependency explicitly rather than treating it as a plain
   body test. → RU-13-adjacent.
6. **queue two-writer path + `HandlerAdapter`** — confirm/raise from 68.8%. → RU-09.

### 4.3 DEFER-TO-M6 (needs the controlled harness — do NOT hand-roll a fake)

- tmux paste **landing** on a not-ready TUI → M6 **WS3-Claude parity** + real-agent **WS4**.
- real ssh transport failure modes (exit-255, offline, tunnel-bind) → M6 **WS2 docker E2E** + **WS1.3**.
- live codex **input-direction** anchor → M6 **WS3-codex** live re-capture (`CODEX_LIVE`).
- live core-loop bead→terminal on real agents → M6 **WS4**.

### 4.4 DELETE, don't cover

- `operatornfr` theater (~24-27 files, ~9-10k LOC) → M1-2/M1-3 (operator-gated). Its green is the
  problem, not a thing to extend.
- `specaudit` 3 untagged stragglers → tag them or fold into the specaudit lint.
- `internal/scenario` structural-corpus files → collapse to the harness + the real ones, **but only after
  diffing against the live `test-scenario`/`check-full` set (RU-22, coverage #5)** — do not delete a file
  the landed WS1.2/1.3 gate now compiles.
- `core-eventreg` production-dead decode/validate + 388-line `pertypecompat` table → delete, and its
  coverage with it (do not "improve" coverage of code with zero live consumers).

### 4.5 Already-STRONG unit coverage vs M6-harness territory — DO NOT DUPLICATE (retitled — coverage #7)

Two distinct "leave the coverage alone" categories, separated so the RU-14 apparent contradiction
disappears:

**(a) Already-STRONG *pre-existing unit* coverage — don't ADD tests (but the correctness leg still READS
the code):**
- **hook/policy seams** — ALL STRONG today (`hook` 95.5%, `policy` 98.5%, `hookrelay` 86.6%, `hooksystem`
  82.4%). This is pre-existing unit coverage, **not** M6-harness coverage. **RU-14 still does a
  correctness read** of this code — "leave alone" means *don't write more tests*, not *don't review*.
- **run terminal spine / state machine** (`runexec` 95.6%, `mergeq` 87.0%, `runexectest`, `orchestrator`
  89.9%) — STRONG; keep the floor, do not rebuild.

**(b) M6-harness territory (WS1–WS5: scenario gate, twin parity, core-loop-proof, assessor, risk-tiering)
— §5 verifies M6 closes these; the review does not rebuild them.** The four Rebuild/Delete units
(`test-bloat`, `tmux-io` input channel, `remote`, `core-eventreg` dead surface): adding coverage to code
about to be deleted/rebuilt is waste that raises the sunk-cost bar against the rebuild
(`coverage-strategy` §6).

---

## 5. Composition with M6 (controlled-testing harness) — do NOT duplicate

`plans/2026-07-13-code-revamp/M6-PLAN.md` is the controlled-testing harness milestone. The fake-anchored/
missing gaps in §4 are **exactly what M6 is built to close.** This review does not rebuild them — it
points at M6 and **verifies M6's own coverage is real** (that M6's new tests are not themselves
fake-anchored).

| Coverage gap | M6 workstream that closes it | This review's job |
|---|---|---|
| tmux paste *landing* untestable by unit | WS3 twin↔real parity (Claude-A/B/C/D) + WS3-F1 equivalence library + "2 irreducibly-real stages: splash-dismiss + physical-Enter" | Verify the parity harness's equivalence library actually **fails on a mutated stream**; do NOT write another fake tmux test. |
| **`internal/twinparity` equivalence library (MOVED from RU-13 — coverage #4)** | WS3-F1 (commit `d01e27f8`, landed 2026-07-16 — the twin↔real parity equivalence library, **not** keeper logic) | **Verify the equivalence library rejects a mutated stream.** COORD c054 records WS3-F1's round-1 review caught a **load-bearing false-negative** (terminal-spine order inversions + non-journaled kinds would have *accepted* mutated streams) — re-check exactly that. Filing it as "keeper, Codex, Tier 2" misframes it; it belongs here. |
| ssh transport / localhost skip-to-green | WS1.3 (`HARMONIK_REQUIRE_REMOTE_E2E=1` → `t.Fatalf`) + WS2 dockerized remote E2E (host-independent) | Confirm WS1.3 is wired and WS2 replaces the skip-to-green tier; the review flags the skip, M6 fixes it. |
| live core-loop (real agent bead→terminal) | WS4 revived `core-loop-proof` (forced LT leg, zero-PENDING gate) | Confirm the forced command exists and is loud-PENDING (never false-green); do not build a parallel E2E. |
| operatornfr/scenario theater still green | WS5-1 (assessor: reasoned judgment, NOT bead-count) + this review's deletion list | Review supplies the deletion list; M6 supplies the assessor that stops trusting the green count. |
| coverage-vs-risk tiering | WS1.5 risk-tiering rule (Tier-0/1/2 path-glob floor) | This plan's §3 risk table + §2 risk map should **seed** WS1.5's tiering; keep them consistent. |

**Boundary rule:** if a gap is a *controlled-harness* gap (real agent, real transport, real tmux), it
belongs to M6 — the review verifies M6 closes it. If a gap is a *pure-logic* gap (runbridge decision,
untested adapter argv), the review calls for a direct unit test now (§4.2).

> M6 is partially landed (recent commits: WS1.2/1.3, WS3-F1 equivalence library, WS3-Claude-A wire-tap
> seam). **At fire time, re-check which WS have merged** so the review verifies the *shipped* M6 tests,
> not the planned ones. This same re-check gates the RU-22 prune list (§3, coverage #5) — do not delete
> scenario files the landed WS1.2/1.3 `check-full` now compiles.

---

## 6. Sequencing summary

1. **Now:** stage this plan; build the merge/dedupe driver + schema + prompt preamble (with the §2.4
   failed-chunk / group-don't-drop / two-regime rules and the §2.6 checkpoint-abort-resume protocol
   baked in); run the **calibration pilot as a HARD go/no-go gate** (§7 Q1–Q4):
   - one ~5k-LOC package for context-pressure/depth,
   - a 4-way fan-out smoke test of the **exact planned invocation** (with `--ephemeral` + per-worker
     `CODEX_HOME` + materialized `forced_login_method`),
   - a metered 5–10-chunk projection that measures **requests-until-first-429, the reset window
     (per-min / per-5h / per-day), and whether 4–6-wide fan-out trips the limit faster than serial**,
   - one chunk on `gpt-5.4-mini` to confirm it works AND saves quota per the measured metric (else drop
     the mini tier).
   **Gate:** if the subscription's measured headroom cannot sustain the sweep, decide the fallback
   (serialize + spread over days, or upgrade plan) before firing — Codex is MANDATORY, so the whole
   plan's throughput hinges on this. The pilot stays a hard go/no-go gate regardless of plan.
2. **Pre-fire boundary tasks** (before Wave 1): finalize **RU-04b** scope against `ls internal/workspace/`;
   build the **`cmd/harmonik` file manifest** and diff it against `ls cmd/harmonik/*.go`; connect the
   **RU-22 prune list** to the live `test-scenario`/`check-full` set; re-check shipped M6 WS.
3. **Gate:** wait for **giant-retirement** to land (operator ordering).
4. **Re-validate** the daemon/core sub-chunk map against the post-retirement tree.
5. **Fire Wave 1 (Tier 1)** — BOTH-lane on the ack-free channels + god-function + M3 spine + the
   fabricated-done-status seams (RU-01, RU-04, RU-04b tail, RU-05, RU-12x, RU-03).
6. **Waves 2→5** by tier; the driver checkpoints per chunk and can abort/resume cleanly on throttle
   (§2.6); adjudicate Codex single-lane findings via the Claude lane; file merged (grouped, never dropped)
   findings as beads (daemon on) or `findings.md`.
7. **Coverage arm runs per-unit throughout**, feeding ADD-NOW targets to direct fixes and DEFER-TO-M6
   gaps to M6 verification.

---

## 7. Open questions (all resolved)

All of v1's open questions — including the one former operator nod (O1) — are now **resolved**.

### O1 — markdown agent-skill drift pass: RESOLVED = DEFER (operator, 2026-07-16)

- **O1 — markdown agent-skill drift pass (RU-24 markdown portion): DEFERRED, out of this sweep's scope.**
  The 15 embedded skill `.md` files are agent-behavioral contract, not runtime code. Skill/config `.md`
  drift is the **agent-config-reviewer's** job — that tier-2 reviewer fires on config drift (CLAUDE.md /
  AGENTS.md / settings.json / skill-registry) and owns "does each skill still match the code it
  documents." It is **not** part of this code/coverage correctness sweep. RU-24's **executable** assets
  (shell scripts + templates) stay IN and RU-25 (spec-vs-code drift) stays IN — both already-decided; only
  the markdown-skill-drift portion is deferred to the agent-config-reviewer. No remaining operator gate.

### Resolved in v2 (was v1 §7)

- Q1 context-pressure, Q2 `CODEX_HOME` contention, Q3 cost ceiling, Q4 rate limits → folded into the **hard
  go/no-go pilot gate** (§6) with explicit measurables (F1); plan framing corrected to ChatGPT
  subscription (operator-confirmed 2026-07-16) with headroom pilot-verified empirically.
- Q5 diff-vs-static → static `codex exec` sweep is primary; `codex review --base main` is a secondary
  incremental pass. Confirmed.
- Q6 merge quality → resolved by §2.4 group-don't-drop + failed-chunk≠clean (F4/F5).
- Q7 RU-05/RU-21 oversize → RU-05 split a/b (§3); RU-21 stays one mechanical classification pass.
- Q8 RU-01 oversize → two-agent read with divided line ranges on `workloop.go`; the giant-retirement carve
  may shrink it (re-validate at fire time).
- Q9 assets/spec-drift → **decided**: RU-24 executable assets IN, RU-25 spec-drift IN; the markdown
  portion is O1, RESOLVED = DEFER (operator, 2026-07-16) to the agent-config-reviewer.
- Q10 BOTH-lane budget → escape hatch retained: if capacity is tight, demote RU-08/RU-09 to single-lane
  (they are Simplify, not Rebuild) and keep BOTH on the Rebuild-verdict + fabricated-done-status units
  (RU-01/02/03/04/04b/05/12x).
- Coverage Q11 name alignment, Q12 `beadRunOne` 63.7% branch diff, Q13 specaudit stragglers, Q14
  `HARMONIK_REQUIRE_REMOTE_E2E` CI lane, Q15 queue 68.8% ADD-vs-DELETE → all per-unit reviewer tasks,
  unchanged; the confidence-regime and failed-chunk rules (§2.4) remove the false-green risk they carried.

---

## Appendix — full RU list (v2)

**Tier 1 (Wave 1, all BOTH):** RU-01, RU-02, RU-03, RU-04, RU-05 (a/b), **RU-12x (NEW)**.
**Tier 2 (Wave 2):** **RU-04b (NEW, BOTH)**, RU-09 (BOTH), RU-08 (BOTH), RU-07 (X), RU-06 (C),
RU-10 (C), RU-11 (X), RU-12 (C, close-seam carved to RU-12x), RU-13 (X, `twinparity` moved to §5).
**Tier 3 (Wave 3):** RU-09b (X), RU-10b (C skim), RU-14 (X), RU-15 (C), RU-16a (C, +gate/verdict CLI),
RU-16b (X, +eval-harness +RU-24 executable assets), RU-17 (X), RU-18 (X, terminal-transition carved to
RU-12x).
**Tier 4 (Wave 4):** RU-20 (X), RU-21 (X), RU-22 (C, prune gated on live `test-scenario`), RU-19 (C skim).
**Tier 5 (Wave 5):** RU-23 (split C/X), **RU-24 (NEW, X — executable assets in; markdown = O1, DEFERRED to agent-config-reviewer)**,
**RU-25 (NEW, C — spec-vs-code drift cross-cut)**.
**Moved to §5 M6-verification track:** `internal/twinparity` (was inside RU-13).

New units vs v1: **RU-04b, RU-12x, RU-24, RU-25.**
