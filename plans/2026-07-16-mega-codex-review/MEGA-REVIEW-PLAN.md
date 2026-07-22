# Mega Codex Review — Unified Execution Plan

> Single execution plan merging the three input drafts:
> - `codex-mechanism-RESEARCH.md` — verified mechanism for driving Codex as a review lane.
> - `review-decomposition-DRAFT.md` — ~24 review units across 5 risk tiers.
> - `coverage-strategy-DRAFT.md` — per-subsystem coverage map + ranked gaps + M6 composition.
>
> Read-only planning artifact. No code changes. Execution is **gated** (see §1 sequencing constraint).
> Where the drafts disagree or leave a gap, it is carried to **§7 Open questions** rather than papered over.

---

## 1. Frame & goal

The operator wants a **thorough, program-wide code review** of harmonik (~202k prod LOC across
`internal/` + `cmd/`) that **also verifies test coverage lands in the right places** — i.e. it is a
two-legged review:

- **Correctness leg** — find real bugs (concurrency/goroutine leaks, resource lifecycle, nil/error
  paths, spec drift, over-abstraction) across every subsystem, with no silent gaps.
- **Coverage leg** — for each unit, verify the tests exercise **real product behavior at the right
  seam**, not fakes/theater. The load-bearing question is *"does the green here mean the product
  behaves, or that a fake behaved?"* Statement coverage % is an **input, not the verdict**.

**Codex is a MANDATORY, non-negotiable review lane** (operator requirement). It runs alongside a
Claude lane; the highest-risk units get **both** lanes for adversarial cross-check.

### Sequencing constraint (operator ordering)

**Execution happens AFTER the giant-retirement lands.** The giant-retirement work
(`plans/2026-07-16-giant-retirement/` — boot-config + socket-router carves out of the daemon god-package)
restructures exactly the largest, highest-risk review targets (RU-01, RU-06). Reviewing them before
the carve lands would review code that is about to move. This plan is **staged and armed now**;
its fan-out fires once giant-retirement is merged. The chunk map (§2/§3) is re-validated against the
post-retirement tree at fire time (the daemon/core sub-chunk boundaries in particular).

> Note the as-built caveats in `review-decomposition-DRAFT.md`: M4 (remote rebuild), M1 (delete
> test-theater) have NOT landed; M2 (agent-input substrate) and M3 (run-state-machine) are partially
> in flight. The review sees the **current** tree — both the old ack-free channels AND the new
> protocol drivers are live and both must be reviewed.

---

## 2. Review lanes

Two producer lanes, one schema, a merge/dedupe step. Both lanes emit the **identical JSON finding
schema** (§2.3) so a unit reviewed by both yields a cross-checkable pair.

### 2.1 Codex lane (MANDATORY) — `codex exec`

**Mechanism (VERIFIED against codex-cli 0.142.5).** One `codex exec` invocation per chunk, model
pinned, read-only sandbox, JSON output schema. Codex **reads the files itself** inside the sandbox —
you do NOT stuff source into the prompt; you point it at a scope.

```bash
codex exec -m gpt-5.5 \
  --cd /Users/gb/github/harmonik \
  --sandbox read-only \
  --skip-git-repo-check \
  --ephemeral \
  --output-schema schema.json \
  --output-last-message findings-<chunk>.json \
  "You are a senior Go reviewer. Review ONLY the files under <scope> for correctness bugs,
   resource leaks, concurrency hazards, error-handling gaps, and spec drift. Read the files
   yourself. You MAY read specs/, STATUS.md, docs/foundation/.../subsystem-organization.md for
   context. Return findings strictly matching the provided JSON schema; empty array if none."
```

- **Model:** `-m gpt-5.5` is REQUIRED — the built-in default (`gpt-5.6-sol`) is broken on 0.142.5
  (HTTP 400). `gpt-5.4-mini` for leaf/skim packages to conserve subscription usage.
- **`--ephemeral`** + optional per-worker `CODEX_HOME` to avoid SQLite/WAL contention under fan-out.
- **Complementary diff pass:** `codex review --base main` reviews only the ~106-commit delta vs main
  (prose output, no `--output-schema`). Use as a secondary incremental pass, NOT the primary sweep.

#### AUTH GUARDRAIL (load-bearing — do not skip)

**The Codex lane MUST run under ChatGPT-subscription auth with a CLEAN env. NO `OPENAI_API_KEY`,
NO `CODEX_API_KEY`.** A stray key in the shell silently bills the metered API pool — there was a real
API-pool burn incident (`project_flywheel_apikey_burn`). Before any run:

```bash
unset OPENAI_API_KEY CODEX_API_KEY   # or use a per-worker CODEX_HOME with forced_login_method="chatgpt"
```

Verify `~/.codex/auth.json` has `auth_mode` set and `~/.codex/config.toml` has
`forced_login_method = "chatgpt"`. This mirrors harmonik's own `codexbillingguard.go` (force chatgpt,
fail-closed on plan) + `codexlaunchspec.go` credential-deny-keys (strip API keys). Reuse the *posture*,
not necessarily the code. Run `codex doctor` before a big fan-out to confirm auth/runtime health.

### 2.2 Claude lane — sub-agents over the same chunk map

The Claude lane runs its own sub-agents (via this repo's Agent/orchestrator tooling) over the same
unit list, emitting the same schema. It is stronger on **spec-alignment, cross-subsystem reasoning,
and "is this over-built / is this real" judgment** — it carries the AGENT_INDEX/STATUS/skills context
and the `agent-reviewer` rubric (spec drift, unwanted-abstraction, bead/codename match). It owns the
architectural seams, the giant cross-cutting concerns, and **adjudication of Codex's single-lane
findings** (every Codex finding still needs confirmation before it becomes a bead).

### 2.3 Shared finding schema (both lanes)

Object with `findings[]`; each finding: `file, line, severity(critical|high|medium|low|nit),
category, title, detail, suggested_fix` (all required). `additionalProperties:false` is MANDATORY on
every object (OpenAI strict mode returns HTTP 400 without it). Add per-record `reviewer`
(`codex`/`claude`) and `chunk` fields. The schema mirrors the `ubs` `file:line:col + fix` shape the
project already reads. Full schema in `codex-mechanism-RESEARCH.md` §1.

### 2.4 Merge / dedupe (build fresh, ~100 lines)

A driver script (no existing "review orchestrator") that: enumerates the chunk map, fans out
`codex exec` per chunk (cap concurrency 4–6), collects `findings-*.json`, concatenates both lanes'
arrays, and dedupes on a fuzzy key (`file` + nearby `line` + normalized `title`). **Cross-lane hits →
high-confidence; single-lane → needs adjudication.** Rank by severity then cross-lane agreement.
Findings file as beads (once the daemon is back on) or a `findings.md` table. The merge needs a human
spot-check pass (over/under-merge risk — §7).

### 2.5 Lane assignment (from the decomposition)

- **BOTH (adversarial cross-check)** — the 7 highest-risk units where the census verdict is Rebuild
  or a live data-integrity bug: **RU-01, RU-02, RU-03, RU-04, RU-05, RU-08, RU-09**.
- **Codex (X)** — self-contained / protocol / mechanical / newly-written code: **RU-07, RU-09b, RU-11,
  RU-13, RU-14, RU-16b, RU-17, RU-18, RU-20, RU-21**.
- **Claude (C)** — structural/architectural judgment, cross-file reasoning: **RU-06, RU-10, RU-10b,
  RU-12, RU-15, RU-16a, RU-19, RU-22**.
- **RU-23** grab-bag split across whichever lane has free capacity (each sub-package independent).

---

## 3. Risk-tiered execution schedule

Legend — Tier: **1** Critical · **2** High · **3** Medium · **4** Test-theater · **5** Supporting.
Lane: **C** Claude · **X** Codex · **BOTH** adversarial cross-check. Execute top-down; a wave must
clear (findings collected + adjudicated) before dropping to the next tier is *not* required — tiers
may overlap — but **Tier 1 is the first and highest-priority wave**.

### Wave 1 — Tier 1 (Critical). The ack-free channels + the god-function + the new spine. All BOTH-lane.

| ID | Unit | Scope | ~LOC | Lane |
|---|---|---|---|---|
| RU-01 | Daemon workloop / god-function | `workloop.go` (8098), `runbridge.go`, `dispatchsegment.go`, `stategather.go`, `runshell.go`, `eagerfill_em063.go` | ~10k | BOTH |
| RU-02 | DOT cascade + review/gate loop | `dot_cascade.go` (2668), `reviewloop.go` (2159), `dot_gate.go`, `verdictexecutor_rc025a.go`, `sub_workflow_runner.go` | ~6.5k | BOTH |
| RU-03 | Run-state-machine seam (M3) | `runexec` (1158), `mergeq` (146), `run` (128), `runexectest` (719) | ~2.2k | BOTH |
| RU-04 | Remote / SSH substrate | `remotematerialize.go`, `createworktree.go`, `reviewverdict.go`, `workers` (2139), the 92 `runner==nil/!=nil` dual-path sites | ~5k | BOTH |
| RU-05 | tmux input channel (old, still live) | `tmuxsubstrate.go` (2769), `pasteinject.go` (2671), `lifecycle/tmux/` (5562), `keeper/tmuxresolve.go` — **SPLIT** RU-05a (daemon files) / RU-05b (`lifecycle/tmux/`) | ~11k | BOTH (split) |

Top-level ordering within Wave 1 (from the decomposition's risk ranking): RU-01 → RU-04 → RU-05 →
RU-03 → RU-09 (Tier-2 but pulled forward for the live lost-update) → RU-08/RU-07.

### Wave 2 — Tier 2 (High). New M2 code, data-integrity, big Simplify packages.

| ID | Unit | ~LOC | Lane |
|---|---|---|---|
| RU-09 | Queue subsystem (`rpc.go` two-writer path, `HandlerAdapter`) | ~4.5k | BOTH |
| RU-08 | Substrate / Handler contract seam (`substrate`, `handler`, `handlercontract`) | ~9.4k | BOTH |
| RU-07 | Codex substrate vertical, M2 new (`codexdriver`/`codexinput`/`codexreactor`/`codexwire`/`codexdigitaltwin`/`codextest`) | ~8k | X |
| RU-06 | Daemon god-struct + composition root (`daemon.go` 85-field `workLoopDeps`, `projectconfig.go`, `socket.go`, …) | ~11k | C |
| RU-10 | Core event registry surface (`eventreg`, `pertypecompat` dead table, `eventtype.go`) | ~5k | C |
| RU-11 | Event bus (`busimpl.go`, `jsonlwriter.go`) | ~2.2k | X |
| RU-12 | Lifecycle sweeps + reconcile (startup/orphansweep/stalewatch/draindetect) | ~11k | C |
| RU-13 | Keeper (`watcher.go`, `step.go`, `cycle.go`, `keepertwin`, `twinparity`) | ~9k | X |

> RU-06 overlaps giant-retirement's socket-router + boot-config carves — review the **post-retirement**
> shape.

### Wave 3 — Tier 3 (Medium). CLI, workflow engine, adapters, skim units.

| ID | Unit | ~LOC | Lane |
|---|---|---|---|
| RU-09b | Queue CLI | ~3.9k | X |
| RU-10b | Core type/payload defs (skim — classification, not line-audit) | ~26k | C (skim) |
| RU-14 | Hook system (M5 seams: `hook`/`hooksystem`/`hookrelay`/`policy`/`orchestrator`) | ~2.5k | X |
| RU-15 | Workflow graph engine (`workflow`, `workflow/dot`, `workflowvalidator`, `goalstate`) — review for over-abstraction | ~5k | C |
| RU-16a | CLI — comms + core commands (`comms.go`, `main.go`, `run.go`, `harness.go`) | ~9k | C |
| RU-16b | CLI — keeper/init/assets/lifecycle verbs | ~9k | X |
| RU-17 | Supervise + daemon lifecycle | ~4.5k | X |
| RU-18 | Beads adapter (`brcli`) | ~3.75k | X |

### Wave 4 — Tier 4 (Test-theater). Classification passes (keep-vs-delete), not line-audit.

| ID | Unit | ~LOC | Lane |
|---|---|---|---|
| RU-20 | Test theater — `operatornfr` (classify; still LIVE in default suite) | ~14.7k | X (classify) |
| RU-21 | Test theater — `specaudit` (classify; 129/132 already `//go:build specaudit`-tagged, 3 stragglers) | ~37.9k | X (classify) |
| RU-22 | Scenario harness + integration (keep harness + ~11 real files, prune corpus) | ~13k | C (classify) |
| RU-19 | Twin harnesses (skim) | ~16k | C (skim) |

### Wave 5 — Tier 5 (Supporting).

| ID | Unit | ~LOC | Lane |
|---|---|---|---|
| RU-23 | Supporting grab-bag (`agentmanifest`, `apptap`, `branching`, `cognition`, `crew`, `dashboard`, `digest`, `presence`, `release`, `replay`, `schedule`, `sentinel`, `sessiondata`, `structuredlog`, `usage`, `watch`, `testhelpers`, probes) | ~18k | split C/X |

---

## 4. Coverage arm

For **every** review unit, the reviewer runs the §3-checklist from `coverage-strategy-DRAFT.md`
(seam check → theater check → skip-to-green check → high-risk-path check → number sanity) and emits,
per sub-unit: `{STRONG|WEAK|THEATER|FAKE-ANCHORED|MISSING} · evidence file:line · MUST-fix? y/n`.
A unit **fails the coverage gate** if any MUST check fails. Coverage % is recorded but never the verdict.

### 4.1 The through-line

Every place that is **ack-free or terminal-decision** (runbridge, tmux landing, ssh transport, live
input-ack) is exactly where coverage is fake-anchored or missing — because those are precisely the
paths a unit test with a fake *cannot* prove. That is the case for M6's controlled harness (§5), not
a thing to patch with more unit tests.

### 4.2 ADD-NOW (pure-logic gaps — the review can call for a direct unit test now)

Ranked by risk:
1. **`runbridge.go` — dedicated bridge test (HIGHEST VALUE).** Shell-event → machine-event → `br`
   close/reopen mapping; the seam where the census caught a *fabricated* done-status (hk-2hfyt closed
   with the fix absent). Currently rides entirely on `beadRunOne` @ 63.7%; no `runbridge_test.go`
   exists. Fast unit test with existing `substrate.Twin` + `FakeClock`. → RU-01.
2. **tmux adapter argv/exec tests** for `SendKeysEnter`, `SendKeysQuit`, `CapturePane`, `WriteToPane`
   (no adapter-level test today). Same fake-binary technique as `osadapter_test.go`. Closes an
   untested-product gap (does NOT prove *landing* — that is M6/WS3). → RU-05.
3. **`EmitWorkerOfflineEvent` real-body test** — replace the field-echo theater in
   `remote_substrate_b11_test.go` with a real-emit test via a recording bus. → RU-04.
4. **`runshell.go::fireOnCancel` (0%)** + daemon 0-25% funcs touching terminal/merge
   (`maybeEmitEpicCompleted` 25%, `runMergeFmtCheck` 21%). → RU-01.
5. **`agentlaunch/SpawnKeeperWindow` body** (untested). → RU-13-adjacent.
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
- `internal/scenario` 37 structural-corpus files → collapse to the harness + the 5 real ones.
- `core-eventreg` production-dead decode/validate + 388-line `pertypecompat` table → delete, and its
  coverage with it (do not "improve" coverage of code with zero live consumers).

### 4.5 M6-covered areas — DO NOT DUPLICATE (mark explicit)

- **hook/policy seams** — ALL STRONG today (`hook` 95.5%, `policy` 98.5%, `hookrelay` 86.6%,
  `hooksystem` 82.4%). **Leave alone.**
- **run terminal spine / state machine** (`runexec` 95.6%, `mergeq` 87.0%, `runexectest`,
  `orchestrator` 89.9%) — STRONG; keep the floor, do not rebuild.
- The four **Rebuild/Delete** units (`test-bloat`, `tmux-io` input channel, `remote`, `core-eventreg`
  dead surface): adding coverage to code about to be deleted/rebuilt is waste that raises the
  sunk-cost bar against the rebuild (`coverage-strategy` §6).

---

## 5. Composition with M6 (controlled-testing harness) — do NOT duplicate

`plans/2026-07-13-code-revamp/M6-PLAN.md` is the controlled-testing harness milestone. The
fake-anchored/missing gaps in §4 are **exactly what M6 is built to close.** This review does not
rebuild them — it points at M6 and **verifies M6's own coverage is real** (that M6's new tests are
not themselves fake-anchored).

| Coverage gap | M6 workstream that closes it | This review's job |
|---|---|---|
| tmux paste *landing* untestable by unit | WS3 twin↔real parity (Claude-A/B/C/D) + WS3-F1 equivalence library + "2 irreducibly-real stages: splash-dismiss + physical-Enter" | Verify the parity harness's equivalence library actually **fails on a mutated stream**; do NOT write another fake tmux test. |
| ssh transport / localhost skip-to-green | WS1.3 (`HARMONIK_REQUIRE_REMOTE_E2E=1` → `t.Fatalf`) + WS2 dockerized remote E2E (host-independent) | Confirm WS1.3 is wired and WS2 replaces the skip-to-green tier; the review flags the skip, M6 fixes it. |
| live core-loop (real agent bead→terminal) | WS4 revived `core-loop-proof` (forced LT leg, zero-PENDING gate) | Confirm the forced command exists and is loud-PENDING (never false-green); do not build a parallel E2E. |
| operatornfr/scenario theater still green | WS5-1 (assessor: reasoned judgment, NOT bead-count) + this review's deletion list | Review supplies the deletion list; M6 supplies the assessor that stops trusting the green count. |
| coverage-vs-risk tiering | WS1.5 risk-tiering rule (Tier-0/1/2 path-glob floor) | This plan's §3 risk table + §2 risk map should **seed** WS1.5's tiering; keep them consistent. |

**Boundary rule:** if a gap is a *controlled-harness* gap (real agent, real transport, real tmux), it
belongs to M6 — the review verifies M6 closes it. If a gap is a *pure-logic* gap (runbridge decision,
untested adapter argv), the review calls for a direct unit test now (§4.2).

> M6 is partially landed (recent commits: WS1.2/1.3, WS3-F1 equivalence library, WS3-Claude-A
> wire-tap seam). At fire time, re-check which WS have merged so the review verifies the *shipped*
> M6 tests, not the planned ones.

---

## 6. Sequencing summary

1. **Now:** stage this plan; build the merge/dedupe driver + schema + prompt preamble; run the
   calibration pilot (§7 Q1/Q3 — one ~5k-LOC package + a 4-way fan-out smoke test + a 5-10 chunk cost
   projection).
2. **Gate:** wait for **giant-retirement** to land (operator ordering).
3. **Re-validate** the daemon/core sub-chunk map against the post-retirement tree.
4. **Fire Wave 1 (Tier 1)** — BOTH-lane on the ack-free channels + god-function + M3 spine.
5. **Waves 2→5** by tier; adjudicate Codex single-lane findings via the Claude lane; file merged
   findings as beads (daemon on) or `findings.md`.
6. **Coverage arm runs per-unit throughout**, feeding ADD-NOW targets to direct fixes and DEFER-TO-M6
   gaps to M6 verification.

---

## 7. Open questions (carried forward for the adversarial reviewers)

Merged from all three drafts; unresolved and load-bearing.

**Mechanism / cost (from RESEARCH §7):**
1. **Context-pressure per chunk [UNVERIFIED].** 17k tokens for a 145-line file was measured; a
   multi-thousand-LOC package was NOT. Codex may compact mid-review and go shallow. **Needs a
   calibration run on one ~5k-LOC package before committing chunk sizes.**
2. **Parallel `CODEX_HOME` contention [UNVERIFIED].** SQLite/WAL-sharing risk is reasoned, not tested
   under load. Validate `--ephemeral` and/or per-worker `CODEX_HOME` with a 4-way fan-out smoke test.
3. **Cost ceiling [UNVERIFIED].** Full-repo token/subscription cost is extrapolated (~55-65 chunks ×
   40k-120k tokens). Run a metered 5-10-chunk pilot and project before the full sweep.
4. **Rate limits on ChatGPT-plan Codex [UNVERIFIED].** Unknown ceiling; a big concurrent sweep may
   throttle. Mitigate with concurrency cap (4-6) + retries.
5. **Diff vs static scope.** Does "whole system" mean static-everything (`codex exec` sweep) or
   delta-since-main (`codex review --base main`)? Likely both, exec-sweep primary — confirm.
6. **Merge quality [UNVERIFIED].** Fuzzy `file+line+title` dedupe may over/under-merge; needs a human
   spot-check. Every merged Codex finding needs adjudication before becoming a bead.

**Decomposition / scope (from DECOMPOSITION §"Open calibration questions"):**
7. **RU-05 / RU-21 exceed the 6k target.** RU-05 (11k) should split into RU-05a (daemon tmux files) /
   RU-05b (`lifecycle/tmux/`) — reflected in §3. RU-21 (specaudit 37.9k) as one mechanical
   classification pass, or split by file-glob? Confirm.
8. **RU-01 at ~10k** is above target but `workloop.go` (8098) resists mid-function splitting. Two-agent
   read of the same file with divided line ranges, or a package split? (Interacts with giant-retirement
   — the carve may shrink it.)
9. **Assets/skills (RU-24) and spec-drift (RU-25) are GAPS.** `cmd/harmonik/assets/*` (embedded agent
   skill markdown, shell scripts) and repo-root docs/`specs/` are NOT covered by a Go-code review unit.
   **Confirm whether "the whole system" includes shipped agent-skill/scaffold content (add RU-24) and a
   spec-vs-code drift pass (add RU-25), or exclude them as non-code.** `specs/` is read AS INPUT either
   way, not audited.
10. **BOTH-lane budget.** 7 units get two reads. If lane capacity is tight, demote RU-08 or RU-09 to a
    single lane (they are Simplify, not Rebuild) and keep BOTH only on the 5 Rebuild-verdict units.

**Coverage (from COVERAGE-STRATEGY §8):**
11. **Decomposition-name alignment.** Coverage §7 keys to the census area names; §3 here keys to RU-IDs.
    Confirm the mapping in `coverage-strategy` §7 stays consistent with the RU-IDs (it does today — but
    re-check if any RU boundaries move at fire time).
12. **`beadRunOne` @ 63.7%** — is the untested ~36% dead/peripheral, or a real uncovered path? A
    reviewer must diff covered-vs-uncovered branches before accepting the floor as adequate.
13. **specaudit 3 untagged stragglers** (`ar025_agent_type_regex_test.go`,
    `hqwn57_eventbus_interface_test.go`, `sh_inv005_declarative_loadable_test.go`) — keep in the default
    suite (they check real product constants) or tag them for a truly theater-free green? Judgment call.
14. **`HARMONIK_REQUIRE_REMOTE_E2E=1` CI lane** on an sshd-capable runner *before* WS2 docker lands, to
    kill the localhost skip-to-green immediately? (M6 WS1.3-vs-WS2 sequencing.)
15. **queue 68.8% gap** — is it the retired/dead `HandlerAdapter` grab-bag (delete) or live queue logic
    (cover)? Determines whether it is a §4.2 (ADD) or §4.4 (DELETE) item.
