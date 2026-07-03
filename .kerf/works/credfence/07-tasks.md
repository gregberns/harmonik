# Implementation Tasks — `credfence` (Credential & spend safety fence)

> Breaks the seven spec changes (05-changelog.md / 05-spec-drafts/) into implementation tasks. Each task names the spec sections it implements, its deliverables, concrete acceptance criteria, and its dependencies. Every task is reconciled against an existing `codename:credfence` bead (`br list --label codename:credfence`, 15 beads). Tasks with NO backing bead are flagged **GAP** in §Task→Bead reconciliation — per the work brief, no beads are created here; the GAPs are recorded for the orchestrator to file.

## Legend
- **Spec refs** point at `05-spec-drafts/<file>.md` (the drafts that become `specs/<file>.md` on finalize).
- **Code anchors** are from `docs/flywheel/2026-05-30-lifecycle-feasibility-and-gaps.md` (verified against the live tree this pass).
- Bead IDs in **plain English**: see §Task→Bead reconciliation for the full glossary.

---

## A. Foundation tasks (core types — must land first)

### T-CORE-1 — Extend the `BudgetScope` enum with `handler_account`
- **What:** Add `BudgetScopeHandlerAccount BudgetScope = "handler_account"` to `internal/core/budgetscope.go`; add it to the `Valid()` switch and to the `UnmarshalText` accepted set + error message. Without this, any `budget_exhausted{budget_scope=handler_account}` payload is **rejected** by `UnmarshalText` as an unknown scope (live `budgetscope.go:39-72` accepts only the three original values).
- **Spec sections:** control-points.md CP-022 (§4.5) + `ENUM BudgetScope` (§6.1.4) + `BudgetPayload` RECORD comment (§6.1.4) + §7 example-YAML.
- **Deliverables:** `internal/core/budgetscope.go` (+ its `_test.go`).
- **Acceptance:** `BudgetScope("handler_account").Valid()` is true; round-trips through `MarshalText`/`UnmarshalText`; the three original values still validate; an unknown value still errors. The four spec sites all list `handler_account`.
- **Depends on:** none.
- **Bead:** **GAP** (no bead). Foundational prerequisite for hk-k3f8g; see reconciliation note G-1.

### T-CORE-2 — Extend `BudgetExhaustedEventPayload` for the account-scoped variant
- **What:** In `internal/core/budgetevents_hqwn59.go`, add optional `BudgetScope *BudgetScope`, `SpentUSD *float64`, `CapUSD *float64` fields; make `RunID`, `SessionID`, `AttemptedDispatchCost` optional for the account-scoped (run-agnostic) variant. `Valid()` must accept a payload that carries `budget_scope=handler_account` + `spent_usd`/`cap_usd` with NO `run_id`/`attempted_dispatch_cost`, while still requiring `run_id`+`attempted_dispatch_cost` for the per-run (agent-runner S04) variant. Governed by event-model §6.4 additive-field rule.
- **Spec sections:** event-model.md §8.4.3 row + INFORMATIVE producer-set note.
- **Deliverables:** `internal/core/budgetevents_hqwn59.go` (+ `_test.go`); event-registry entry if registered (`eventreg_hqwn59.go`).
- **Acceptance:** an account-scoped payload (`{budget_scope:handler_account, spent_usd, cap_usd}`, no run_id) passes `Valid()`; a per-run payload missing `run_id` still fails `Valid()`; existing per-run payloads are unaffected (backward compatible).
- **Depends on:** T-CORE-1 (uses `BudgetScopeHandlerAccount`).
- **Bead:** **GAP** (no bead). Foundational prerequisite for hk-k3f8g; see reconciliation note G-2.

---

## B. Credential-isolation tasks

### T-CRED-1 — Credential env deny-list constant + scrub at the `ClaudeEnvVars` boundary
- **What:** Define the deny-list set `{ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN, CLAUDE_CODE_OAUTH*}` ONCE as a code constant; in the `ClaudeEnvVars`/CHB-006 child-env assembly (anchor `internal/daemon/osadapter.go:497` `buildClaudeLaunchSpec`, and the `ClaudeEnvVars` builder), strip every deny-list key — symmetric with the existing `HARMONIK_SECRET_*` strip. tmux `-e` is **additive** over the inherited server env, so the assembly must actively override/remove the key, not merely omit it. Add the substrate-handoff assertion (CI-004) as a test/debug-build check, not a second scrub.
- **Spec sections:** credential-isolation.md CI-002, CI-003, CI-004; claude-launchspec.md §4 `baseEnv` + assembly step 5.
- **Deliverables:** deny-list constant (one file in `internal/daemon/` or `internal/core/`); edits to the child-env assembly path; the assertion hook at the substrate handoff.
- **Acceptance:** a constructed child env, given all three deny-list keys present in `BaseEnv`, contains none of them; a non-deny-list var threaded through `BaseEnv` survives (proves keyed strip, not blanket). No deny-list key reaches the spawned `claude` via the tmux `-e` path.
- **Depends on:** none.
- **Bead:** **hk-f2nm1**.

### T-CRED-2 — Named regression test locking the scrub invariant
- **What:** A named regression test asserting "no daemon-spawned `claude` ever receives a credential env deny-list key." Assertion is by-key (absence), and MUST NOT print/log/emit any credential value.
- **Spec sections:** credential-isolation.md CI-004a + CI-INV-002.
- **Deliverables:** a `_test.go` in the daemon package.
- **Acceptance:** test fails if the scrub is removed; passes with T-CRED-1; emits no secret value.
- **Depends on:** T-CRED-1.
- **Bead:** **hk-4g32m**.

### T-CRED-3 — Scoped-injection env builder for the Pi holder process
- **What:** Replace blanket `os.Environ()` inheritance on the Pi-launch paths with an explicit allow-list base + the credential added explicitly. Anchors: `cmd/harmonik/supervise/shim.go:103` (`syscall.Exec(resolved, command, os.Environ())`) and the supervisor process-exec path (`supervisor.go` ~:336). The `attach` path is CARVED OUT (CI-005a) — the operator's own shell legitimately holds the credential.
- **Spec sections:** credential-isolation.md CI-001, CI-005, CI-005a.
- **Deliverables:** scoped-env builder; edits to `shim.go` + supervisor exec; attach path left unchanged (carve-out).
- **Acceptance:** the Pi process env is built from a scoped base (not full parent env); the daemon and `claude` children receive no deny-list key; the attach path still delivers the operator's own credential.
- **Depends on:** T-CRED-1 (shares the deny-list constant for the "daemon/children get none" assertion).
- **Bead:** **hk-uiu98**.

### T-CRED-4 — `supervise start` credential injection source (precedence + fail-closed)
- **What:** `harmonik supervise start` injects the credential into the Pi holder process from a non-committed scoped source. Precedence: operator env export > gitignored repo-root `.env` (read ONLY by `supervise start`) > fail-closed error. The `.env` MUST NOT be read by the daemon or unioned into any child env. Register the source as an ON-004 config-inventory knob.
- **Spec sections:** credential-isolation.md CI-006; operator-nfr.md ON-004b + ON-008a.
- **Deliverables:** `.env` reader scoped to `supervise start`; precedence + fail-closed logic; ON-004b inventory wiring.
- **Acceptance:** fresh `supervise start` with no export but a gitignored `.env` authenticates the Pi; with neither source it fails closed (clear error, no silent auth failure); the daemon env at the same boot has no deny-list key.
- **Depends on:** T-CRED-3 (injects via the scoped builder).
- **Bead:** **hk-fo9zz**.

### T-CRED-5 — `.gitignore` + pre-commit secret scan
- **What:** Add `.env` / `*.env` patterns to `.gitignore`; add a pre-commit secret-scan hook that greps for the deny-list KEYS by name (never logging a matched value). `git check-ignore .env` currently exits 1 — a stray `git add -A` would commit the live key.
- **Spec sections:** credential-isolation.md CI-007 (deny-list CI-002 is the scan's input).
- **Deliverables:** `.gitignore` entries; pre-commit hook + its registration.
- **Acceptance:** `git check-ignore .env` exits 0; the pre-commit hook blocks a commit that would introduce a deny-list key's value; the hook prints no secret value.
- **Depends on:** none (the scan references the CI-002 deny-list by name; independent of T-CRED-1's runtime strip).
- **Bead:** **hk-pbs1u**.

---

## C. Spend-governance tasks (unified meter)

### T-SPEND-1 — Daemon-side unified spend meter + max-runs ceiling + `budget_exhausted` emit
- **What:** Build the unified meter summing (a) Pi turn cost AND (b) daemon-spawned `claude` implementer/reviewer/resume cost (consumed from `budget_accrual` events, event-model §8.4.2) against ONE shared per-day cap. Enforce a per-day max-runs ceiling counting `run_started` events (loss-proof backstop). On exhaustion (USD ratio ≥ 1.0 OR runsToday ≥ max-runs), emit `budget_exhausted{budget_scope=handler_account, spent_usd, cap_usd}` to the shared event bus so the existing handler-pause policy (`internal/daemon/handlerpause_policy_37zy8.go`) pauses the `claude` handler. Day-boundary reset of USD + max-runs together. (Literal incident root cause: `budget.ts`/CL-090 metered only Pi turns; the 26+ daemon-spawned sessions were invisible.)
- **Spec sections:** cognition-loop.md CL-090, CL-090a, CL-INV-006; event-model.md §8.4.3; handler-pause.md HP-012 + §11a; operator-nfr.md ON-004c + ON-004d.
- **Deliverables:** the daemon-side meter (new file in `internal/daemon/`); the `budget_exhausted` emit wired to the bus; the max-runs counter; the Pi-side feed in `.pi/extensions/flywheel/budget.ts` if the meter is split Pi/daemon.
- **Acceptance:** daemon `budget_accrual` events drive cumulative spend to the cap and the meter emits the account-scoped `budget_exhausted`; the handler-pause policy fires and no new `claude` session launches; separately, reaching max-runs halts dispatch with zero USD over-spend.
- **Depends on:** T-CORE-1, T-CORE-2 (the emitted event uses the new scope value + payload fields).
- **Bead:** **hk-k3f8g**.

### T-SPEND-2 — Finite default per-day budget cap + explicit unlimited opt-out
- **What:** Flip the default per-day cap from the inert `Infinity` (live `.pi/extensions/flywheel/index.ts:64`) to a finite default (recommended 20 USD). `FLYWHEEL_BUDGET_USD_PER_DAY=unlimited` is the explicit operator opt-out for no cap. Deliberate safer-default behavior change (incident: `Infinity` made `budget.ts` ratio()→0 and disabled the entire halt ladder).
- **Spec sections:** cognition-loop.md CL-090 (finite-default flip); operator-nfr.md ON-004c.
- **Deliverables:** `index.ts` default change; the `unlimited` sentinel parse; ON-004c inventory wiring.
- **Acceptance:** with no operator setting, the cap is finite and a high-spend run trips `budget-paused`; with `FLYWHEEL_BUDGET_USD_PER_DAY=unlimited`, no budget trip fires.
- **Depends on:** T-SPEND-1 (the cap value feeds the meter).
- **Bead:** **hk-60csa**.

### T-SPEND-3 — Retry/re-dispatch spend draws the max-runs ceiling (no third budget surface)
- **What:** Ensure each review-loop iteration / re-dispatch / `no_progress` retry counts a `run_started` toward the max-runs ceiling (CL-090a) — paid retries draw the same finite budget rather than being free; there is NO separate retry budget. The per-bead `iteration_cap_hit`/`no_progress_detected` local bound and the global max-runs backstop both apply, whichever fires first.
- **Spec sections:** cognition-loop.md CL-090c (informative).
- **Deliverables:** confirm/adjust that retry spawns emit `run_started` and are counted by T-SPEND-1's max-runs counter; no new budget construct.
- **Acceptance:** N review-loop retries advance runsToday by N; the max-runs ceiling halts a retry storm with no separate retry-budget config.
- **Depends on:** T-SPEND-1.
- **Bead:** **hk-c1ah6** (see reconciliation note R-1 — implement per CL-090c, do NOT add a separate retry budget).

---

## D. Operator-knob tasks (model tiers, baseline, dry-run)

### T-OP-1 — Pi model-tier operator env override (`FLYWHEEL_MODEL_TIER1/2/3`)
- **What:** Replace the hardcoded `claude-opus-4-7-20260219` literal (live `.pi/extensions/flywheel/router.ts:84`) with operator-tunable tiers `FLYWHEEL_MODEL_TIER1/2/3`; the judgment tier (tier-3) defaults to Sonnet, with Opus gated behind explicit opt-in. Register as ON-004e.
- **Spec sections:** cognition-loop.md CL-090b (informative); operator-nfr.md ON-004e.
- **Deliverables:** `router.ts` tier reads; default-Sonnet + Opus-opt-in; ON-004e inventory wiring.
- **Acceptance:** with no override, the judgment tier resolves to Sonnet; `FLYWHEEL_MODEL_TIER3=<opus>` opts into Opus; no hardcoded opus literal remains.
- **Depends on:** none.
- **Bead:** **hk-rljho**.

### T-OP-2 — Daemon `claude` baseline-model operator default
- **What:** Expose a single operator-facing default for the daemon-spawned `claude` baseline model (currently changeable only by editing Go or per-project config, no hot-reload). Cost lever symmetric with the Pi knob. Hot-reload is SHOULD-not-MUST (ON-004f); surface left to implementation.
- **Spec sections:** cognition-loop.md CL-090b; operator-nfr.md ON-004f.
- **Deliverables:** env/config knob for the daemon `claude` baseline; ON-004f inventory wiring.
- **Acceptance:** an operator can set the daemon `claude` baseline model via env/config without editing Go; the default is documented in the config inventory.
- **Depends on:** none.
- **Bead:** **hk-c5oxy**.

### T-OP-3 — `--dry-run` / plan-only daemon mode
- **What:** A daemon `--dry-run`/plan-only mode that previews the intended spawn set (per bead: would-launch implementer + reviewer at model X, across M beads) WITHOUT launching any `claude`, reading the credential source (CI-006), or emitting spend. Mirrors `harmonik queue dry-run`'s validate-without-execute behavior.
- **Spec sections:** cognition-loop.md CL-090d (informative); operator-nfr.md ON-004g.
- **Deliverables:** the `--dry-run` flag + plan-printer in the daemon dispatch path; ON-004g inventory wiring.
- **Acceptance:** `--dry-run` prints the planned spawns and exits without launching `claude`, without reading the credential, and without any `budget_accrual`/`run_started` emission.
- **Depends on:** none (read-only preview; reads the queue/ready set, not the meter).
- **Bead:** **hk-cebjc**.

---

## E. Validation / acceptance-test tasks (required before Ready)

### T-TEST-1 — Scenario test: spawned `claude` never receives a deny-list key
- **What:** End-to-end (twin or real-claude) scenario: a `harmonik run` spawns a `claude` implementer with all three deny-list keys present in the daemon `BaseEnv`; assert the constructed child env strips every deny-list key while a non-deny-list var survives. By-key assertion, never prints a value.
- **Spec sections:** credential-isolation.md CI-003/CI-004a/CI-INV-002.
- **Depends on:** T-CRED-1 (locked by T-CRED-2).
- **Bead:** **hk-24d72** [scenario-test].

### T-TEST-2 — Exploratory test: `supervise start` authenticates Pi from gitignored `.env` with no daemon leak
- **What:** Operator-surface (CLI) test: with no operator export but a gitignored repo-root `.env` holding the key, a fresh Pi boot authenticates from that file (CI-006 precedence); at the same boot the daemon process env and any spawned `claude` child env contain zero deny-list keys.
- **Spec sections:** credential-isolation.md CI-006/CI-001/CI-INV-001.
- **Depends on:** T-CRED-3, T-CRED-4.
- **Bead:** **hk-96s75** [exploratory-test].

### T-TEST-3 — Scenario test: unified meter halts on per-day cap and on max-runs
- **What:** End-to-end scenario: the flywheel loop driving a daemon with a finite `--budget-usd-per-day` and finite max-runs. Validate (a) daemon `budget_accrual` drives spend to the cap → `budget_exhausted{handler_account}` → HP-012 pauses the `claude` handler → `LoopStatus=budget-paused`; (b) reaching max-runs halts dispatch with zero USD over-spend. Terminal: `budget_exhausted{handler_account}` + `handler_paused` for `claude` + `budget-paused`; no further `run_started` after the trip.
- **Spec sections:** cognition-loop.md CL-090/CL-090a/CL-INV-006; handler-pause.md HP-012/§11a; event-model.md §8.4.3.
- **Depends on:** T-CORE-1, T-CORE-2, T-SPEND-1.
- **Bead:** **hk-c7lxc** [scenario-test].

### T-TEST-4 — Exploratory test: finite default cap + `FLYWHEEL_BUDGET_USD_PER_DAY=unlimited` opt-out
- **What:** Operator-surface test: with NO operator setting, the cap is finite (not Infinity) and a high-spend run trips `budget-paused`; with `FLYWHEEL_BUDGET_USD_PER_DAY=unlimited` (explicit opt-out), no budget trip fires. Confirms the deliberate safer-default flip.
- **Spec sections:** cognition-loop.md CL-090 finite-default; operator-nfr.md ON-004c.
- **Depends on:** T-SPEND-1, T-SPEND-2.
- **Bead:** **hk-0p9so** [exploratory-test].

---

## Dependency Graph (DAG — no cycles)

```
T-CORE-1 (BudgetScope enum)
   └─> T-CORE-2 (BudgetExhaustedEventPayload fields)
          └─> T-SPEND-1 (unified meter + max-runs + emit)
                 ├─> T-SPEND-2 (finite default + unlimited opt-out)
                 ├─> T-SPEND-3 (retries draw max-runs)
                 ├─> T-TEST-3 (scenario: meter halts)            [also needs T-CORE-1/2]
                 └─> T-TEST-4 (explore: finite default)          [also needs T-SPEND-2]

T-CRED-1 (deny-list constant + scrub)
   ├─> T-CRED-2 (regression test)
   ├─> T-CRED-3 (scoped Pi-env builder)
   │      └─> T-CRED-4 (supervise start injection source)
   │             └─> T-TEST-2 (explore: supervise start)         [also needs T-CRED-3]
   └─> T-TEST-1 (scenario: deny-list strip)                       [also needs T-CRED-2]

T-CRED-5 (.gitignore + secret scan)      — independent root
T-OP-1  (Pi model tiers)                 — independent root
T-OP-2  (daemon claude baseline)         — independent root
T-OP-3  (--dry-run mode)                 — independent root
```

All edges point from prerequisite to dependent; no back-edges. Every dependent's prerequisites are present in the graph.

## Parallelization Plan

Three independent chains plus four singletons can run concurrently:

- **Wave 1 (parallel roots):** T-CORE-1, T-CRED-1, T-CRED-5, T-OP-1, T-OP-2, T-OP-3.
- **Wave 2:** T-CORE-2 (after T-CORE-1); T-CRED-2 + T-CRED-3 (after T-CRED-1).
- **Wave 3:** T-SPEND-1 (after T-CORE-2); T-CRED-4 (after T-CRED-3); T-TEST-1 (after T-CRED-2).
- **Wave 4:** T-SPEND-2 + T-SPEND-3 (after T-SPEND-1); T-TEST-2 (after T-CRED-4); T-TEST-3 (after T-SPEND-1).
- **Wave 5:** T-TEST-4 (after T-SPEND-2).

The credential chain (T-CRED-*) and the spend chain (T-CORE/T-SPEND-*) share no files except the deny-list constant, which T-CRED-1 owns and T-CRED-3 reads — so the two chains are independent after Wave 1. The operator-knob singletons (T-OP-1/2/3) and the `.gitignore` task (T-CRED-5) touch disjoint files (`router.ts`, daemon baseline config, daemon flag path, `.gitignore`/hooks) and conflict with nothing.

**No undeclared cross-parallel dependency:** T-SPEND-1 depends on T-CORE-1/2 (declared); T-CRED-4 depends on T-CRED-3 (declared); the test tasks declare their implementation prerequisites. Nothing in a parallel wave writes a file another parallel task in the same wave writes.

---

## Task → Bead reconciliation

Glossary (plain English) and 1:1 mapping. Every implementation/test task maps to an existing `codename:credfence` bead EXCEPT the two foundation tasks, flagged **GAP**.

| Task | Bead | Bead plain-English title |
|---|---|---|
| T-CORE-1 | **GAP (G-1)** | — (no bead: `BudgetScope` Go-enum `handler_account` value) |
| T-CORE-2 | **GAP (G-2)** | — (no bead: `BudgetExhaustedEventPayload` account-scoped fields) |
| T-CRED-1 | hk-f2nm1 | scrub `ANTHROPIC_API_KEY` (+`ANTHROPIC_AUTH_TOKEN`, `CLAUDE_CODE_OAUTH*`) from spawned claude child env |
| T-CRED-2 | hk-4g32m | regression test: spawned claude never receives `ANTHROPIC_API_KEY` |
| T-CRED-3 | hk-uiu98 | inject the API key to Pi ONLY via a Pi-scoped source (not blanket `os.Environ`) |
| T-CRED-4 | hk-fo9zz | `supervise start` injects the key from a non-committed scoped source |
| T-CRED-5 | hk-pbs1u | `.env`/`*.env` in `.gitignore` + pre-commit secret scan |
| T-SPEND-1 | hk-k3f8g | daemon-side per-day-USD + max-runs ceiling; emit `budget_exhausted` to pause dispatch |
| T-SPEND-2 | hk-60csa | finite default `FLYWHEEL_BUDGET_USD_PER_DAY`; explicit unlimited opt-out |
| T-SPEND-3 | hk-c1ah6 | global re-dispatch/retry budget on the review-loop / `no_progress` path (see R-1) |
| T-OP-1 | hk-rljho | Pi model tier override `FLYWHEEL_MODEL_TIER1/2/3`; tier-3 Sonnet default, Opus opt-in |
| T-OP-2 | hk-c5oxy | single operator default for the daemon's claude baseline model |
| T-OP-3 | hk-cebjc | `--dry-run`/plan-only daemon mode |
| T-TEST-1 | hk-24d72 | scenario: spawned claude never receives a deny-list key |
| T-TEST-2 | hk-96s75 | explore: `supervise start` authenticates Pi from gitignored `.env`, no daemon leak |
| T-TEST-3 | hk-c7lxc | scenario: unified meter halts on per-day cap and on max-runs |
| T-TEST-4 | hk-0p9so | explore: finite default cap + `FLYWHEEL_BUDGET_USD_PER_DAY=unlimited` opt-out |

All 15 credfence beads are mapped exactly once. The two test-task conventions are satisfied: scenario beads hk-24d72 (credential) + hk-c7lxc (spend), exploratory beads hk-96s75 (credential) + hk-0p9so (spend); each is an explicit task with declared dependencies on the implementation tasks it validates. Neither this work nor its implementation beads may close until these four test beads close.

### GAPs (no bead — record for the orchestrator to file; NOT created here)

- **G-1 — `BudgetScope` Go-enum `handler_account` value (T-CORE-1).** `internal/core/budgetscope.go`'s `Valid()`/`UnmarshalText` accept only `{per_role, per_run, per_state}` and **reject** unknown values (no silent fallback, by design). The control-points CP-022 amendment adds `handler_account`; without the matching Go-enum constant, any `budget_exhausted{budget_scope=handler_account}` payload is rejected as an unknown scope, breaking the seam. Foundational prerequisite for hk-k3f8g but NOT named in hk-k3f8g's description (which anchors to the daemon meter + handlerpause consumer wiring). Suggested: `chore`/`task`, P0, blocks hk-k3f8g.
- **G-2 — `BudgetExhaustedEventPayload` account-scoped fields (T-CORE-2).** Live `internal/core/budgetevents_hqwn59.go`'s `BudgetExhaustedEventPayload` has no `budget_scope`/`spent_usd`/`cap_usd` fields and `Valid()` REQUIRES `run_id`+`attempted_dispatch_cost`. The event-model §8.4.3 amendment makes those optional for the run-agnostic account-scoped variant and adds the three optional fields. Without this, the cognition-loop meter cannot emit a valid account-scoped `budget_exhausted`. Foundational prerequisite for hk-k3f8g, not named in it. Suggested: `task`, P0, blocks hk-k3f8g.

  > Both GAPs could alternatively be folded INTO hk-k3f8g's scope (they are the core-type half of "emit `budget_exhausted{handler_account}`"). The orchestrator should EITHER (a) file two P0 core-type beads blocking hk-k3f8g, OR (b) explicitly broaden hk-k3f8g's description to include the `budgetscope.go` + `budgetevents_hqwn59.go` core-type edits. **Recommended: (a)** — the core-type changes touch `internal/core/` (a different review surface + depguard component) than hk-k3f8g's daemon/Pi meter work, and warrant their own focused review.

### Reconciliation notes (bead-vs-spec mismatches)

- **R-1 — hk-c1ah6 title vs CL-090c.** The bead title says "add a global re-dispatch/retry budget." The spec (cognition-loop CL-090c) deliberately does NOT add a third budget surface: retries draw the existing per-day **max-runs** ceiling (CL-090a). The implementing agent should follow CL-090c (fold retries into max-runs accounting), NOT introduce a separate retry budget. The bead's INTENT (bound retry-as-spend-amplifier) is satisfied by CL-090c; only the mechanism differs. No bead edit required, but the implementer must read CL-090c before coding.
- **R-2 — operator-nfr config-inventory entries (ON-004b–g, ON-008a) have no standalone beads, by design.** These are config-inventory *documentation* requirements implemented as a side-effect of their functional beads: ON-004b→hk-fo9zz (T-CRED-4), ON-004c→hk-60csa (T-SPEND-2), ON-004d→hk-k3f8g (T-SPEND-1), ON-004e→hk-rljho (T-OP-1), ON-004f→hk-c5oxy (T-OP-2), ON-004g→hk-cebjc (T-OP-3), ON-008a→hk-fo9zz (T-CRED-4). Each functional task's deliverables include its ON-004x inventory wiring. No separate beads needed; not a GAP.
- **R-3 — Pure spec-text amendments with no code beyond G-1/G-2.** control-points CP-022 note, event-model §8.4.3 INFORMATIVE note, handler-pause HP-012 note/§11a/§13-RESOLVED, and the cognition-loop §2/§9 coordination text are normative-spec edits landed at `kerf finalize` (copying drafts to `specs/`), not separate implementation tasks. The only CODE these amendments force is G-1 (the enum value) and G-2 (the payload fields); HP-012's controller behavior is unchanged (live `handlerpause_policy_37zy8.go` already trips on `budget_exhausted`). So no additional implementation bead is needed for the spec-text-only amendments.

## Changelog coverage check (every 05-changelog.md entry has ≥1 implementing task)

| Changelog entry | Implementing task(s) |
|---|---|
| credential-isolation.md NEW (CI-001..CI-007 + invariants) | T-CRED-1 (CI-002/003/004), T-CRED-2 (CI-004a), T-CRED-3 (CI-001/005/005a), T-CRED-4 (CI-006), T-CRED-5 (CI-007); tests T-TEST-1/T-TEST-2 |
| cognition-loop.md CL-090 unified meter + CL-090a max-runs | T-SPEND-1; test T-TEST-3 |
| cognition-loop.md CL-090 finite default flip | T-SPEND-2; test T-TEST-4 |
| cognition-loop.md CL-090b model knobs | T-OP-1 (Pi tiers), T-OP-2 (daemon baseline) |
| cognition-loop.md CL-090c retries-draw-max-runs | T-SPEND-3 |
| cognition-loop.md CL-090d dry-run | T-OP-3 |
| handler-pause.md HP-012 note / §11a / §13-RESOLVED | spec-text (landed at finalize); CODE half = G-1 (T-CORE-1); HP-012 controller unchanged (R-3) |
| control-points.md CP-022 scope enum + note | T-CORE-1 (Go enum); spec-text note at finalize |
| event-model.md §8.4.3 producer + optional fields | T-CORE-2 (Go payload); spec-text note at finalize |
| claude-launchspec.md §4 baseEnv + §6 references scrub note | T-CRED-1 (the scrub it documents); spec-text at finalize |
| operator-nfr.md ON-004b–g + ON-008a | T-CRED-4 (ON-004b/ON-008a), T-SPEND-2 (ON-004c), T-SPEND-1 (ON-004d), T-OP-1 (ON-004e), T-OP-2 (ON-004f), T-OP-3 (ON-004g) — R-2 |
| Implementation-task anchors (changelog §) | the `.gitignore`/scan→T-CRED-5; scrub+test→T-CRED-1/2; scoped Pi-env→T-CRED-3; supervise-start→T-CRED-4; meter+default+max-runs→T-SPEND-1/2; model knobs→T-OP-1/2; dry-run+retry→T-OP-3/T-SPEND-3 |

Every changelog entry has at least one implementing task. The two spec-text-only amendments (control-points note, event-model note, handler-pause note) land at `kerf finalize`; their only forced code is captured by the G-1/G-2 foundation tasks.
