# Credential & Spend Safety Fence — Decomposition

> Pass 2 (`decompose`) of the `credfence` spec work. Maps the nine goals from `01-problem-space.md` to spec components with their normative changes, dependencies, and sequencing. Grounded in the 2026-05-30 incident assessment and the live specs (`cognition-loop.md` CL-090, `handler-pause.md` HP-012 + §13 deferred #3, `control-points.md` CP-001..005 Budget primitive, `claude-launchspec.md`, `operator-nfr.md`). This is a planning artifact; it does not modify `specs/`.

## 1. Decomposition strategy

The work splits along the **two safety surfaces** the incident exposed, plus a thin **operator-knob / uncommittable-secret** surface that rides both:

- **Surface A — Credential isolation** (who may hold the key; scrub at every spawn boundary; scoped injection). One NEW spec owns the cross-cutting contract because the obligation spans three subsystems (daemon env assembly, Pi process launch, tmux substrate handoff) and none of them is the natural owner. Goals G1, G2, G3.
- **Surface B — Unified spend governance** (one meter over Pi turns + daemon `claude` sessions; finite default; max-runs; hard-halt into handler-pause; retry budget). This EXTENDS `cognition-loop.md` CL-090 (the existing budget kill-switch) rather than forking a new budget surface, and threads the exhaustion event through the existing `handler-pause` consumer. Goals G4, G5, G6, G9.
- **Surface C — Operator knobs + uncommittable secret + dry-run** (model-tier env overrides, daemon baseline default, `.gitignore` + secret scan, plan-only mode). Smaller additive changes that harden the cost/secret posture; they touch `operator-nfr.md` and a repo-config / tooling surface. Goals G7, G8.

**Component-to-spec map** (one component per coherent spec change; components C1–C7):

| # | Component | Goals | Beads | Spec change? | Owner spec |
|---|---|---|---|---|---|
| C1 | Credential-holder contract + deny-list | G1 | hk-uiu98 | **YES (NEW)** | `credential-isolation.md` (new) |
| C2 | Scrub-at-spawn-boundary guarantee + regression test | G2 | hk-f2nm1, hk-4g32m | **YES (NEW + claude-launchspec note)** | `credential-isolation.md` (new) + `claude-launchspec.md` |
| C3 | Pi-scoped injection + `supervise start` injection source | G3 | hk-uiu98, hk-fo9zz | **YES (NEW + operator-nfr note)** | `credential-isolation.md` (new) + `operator-nfr.md` |
| C4 | Unified spend meter (Pi + daemon `claude`) + finite default + max-runs | G4, G5 | hk-k3f8g, hk-60csa | **YES (rewrite CL-090)** | `cognition-loop.md` |
| C5 | Hard-halt wiring (`budget_exhausted` → handler-pause) + `budget_scope` field | G6 | hk-k3f8g | **YES (cross-check + control-points amendment)** | `handler-pause.md` + `control-points.md` |
| C6 | Operator model-tier + daemon-baseline knobs | G7 | hk-rljho, hk-c5oxy | **YES (operator-nfr + CL note)** | `operator-nfr.md` + `cognition-loop.md` |
| C7 | Uncommittable secret + dry-run + retry budget | G8, G9 | hk-pbs1u, hk-cebjc, hk-c1ah6 | additive (config/tooling + spec notes) | repo `.gitignore`/tooling + `cognition-loop.md` |

The one cross-component coupling worth flagging up front: **C4 and C5 are two halves of one budget story** — C4 defines the meter and what trips it; C5 defines what the trip *does*. They must be co-designed (the event C4 emits is the event C5's consumer reacts to), but they land in different specs (CL-090 vs handler-pause/control-points). Sequence C4 before C5, or co-design.

## 2. Affected Existing Specs

### `specs/cognition-loop.md` (C4, C6, C7)

- **Change summary:** Rewrite/extend CL-090 (per-day budget kill-switch) into a unified spend meter covering BOTH Pi turns AND daemon-spawned `claude` sessions, with a finite default and a max-runs ceiling; add informative notes for the operator model-tier knob (C6), dry-run mode and retry budget (C7).
- **Requirements:**
  - CL-090 meters BOTH (a) Pi's own model turns AND (b) daemon-spawned `claude` implementer/reviewer sessions against ONE shared cap; the spec says how a daemon `claude` session's approximate USD is attributed to the shared meter (OQ-1: lean — daemon emits approximate `run_cost`, Pi's meter sums Pi turns + run costs).
  - CL-090 adds a **max-runs** ceiling alongside the per-day-USD ceiling (a simpler backstop against cost-estimate error).
  - CL-090's default cap is FINITE (e.g. 20 USD) with explicit operator opt-out for unlimited; the inert `Infinity` default (`.pi/extensions/flywheel/index.ts:62`, `budget.ts` `?? Infinity`) is removed; the spec states the safe-by-default principle.
  - §2.1 scope note updated: the budget covers two layers (cognition + execution), not just Pi turns.
  - §6 `LoopStatus` already carries `budget-paused` — confirm no change needed.
  - Informative cross-ref that the Pi judgment model is an operator-tunable tier (C6) and that retry budget complements the per-day + max-runs ceilings (C7).
- **Dependencies:** C5 (handler-pause/control-points) consumes the exhaustion event this spec emits — co-design the event name + payload.

### `specs/handler-pause.md` (C5)

- **Change summary:** Cross-check HP-012 (account-budget exhaustion trip) and confirm the unified-budget event trips the existing `handler-pause-policy-budget-exhausted-claude-code` consumer; resolve §13 deferred item #3 (the `budget_scope` dependency) by reference.
- **Requirements:**
  - HP-012 is confirmed (not rewritten) as the correct consumer: the unified-budget event MUST carry `budget_scope = handler-account` to be handler-fatal (immediate pause, no hysteresis); per-run budget exhaustion stays per-bead per HP-012's existing carve-out.
  - §13 deferred item #3 (`budget_scope` "does not exist in control-points.md §4.5; deferred to control-points amendment") is resolved by credfence's control-points amendment (C5) — add a clarifying note that the dependency is now satisfied.
  - The end-to-end exhaustion path is documented (meter trips → `budget_exhausted{budget_scope=handler-account}` → consumer pauses the `claude` handler type → cognition loop `budget-paused` → operator `supervise resume`).
- **Dependencies:** C4 (defines what trips the event); the control-points `budget_scope` amendment (same component C5).

### `specs/control-points.md` (C5)

- **Change summary:** Add the `budget_scope` field to the Budget ControlPoint (§4.5, CP-001..005) that HP-012 requires.
- **Requirements:**
  - The Budget ControlPoint's typed payload gains a `budget_scope` field (e.g. `{handler-account, per-run}`) so a `budget_exhausted` event can be classified as handler-fatal vs per-bead per HP-012.
  - The addition is ADDITIVE (new optional field; no change to CP-001's single-typed-primitive invariant or the per-Kind semantics table CP-005).
- **Dependencies:** none upstream; C5's handler-pause cross-check depends on this field existing.

### `specs/claude-launchspec.md` (C2)

- **Change summary:** Additive note recording that the CHB-006 env-assembly step (`ClaudeEnvVars`) includes the credential deny-list scrub.
- **Requirements:**
  - §4 env-assembly step (near the `baseEnv` note, line ~108 / step 5) records that env assembly removes the deny-list keys from the constructed child env — keeping the launch-spec spec consistent with the new `credential-isolation.md` contract.
- **Dependencies:** C1 (deny-list set), C2 (scrub obligation it references).

### `specs/operator-nfr.md` (C3, C6)

- **Change summary:** Record that `harmonik supervise` owns the credential-injection source (C3) and the model-tier / daemon-baseline env knobs (C6) as part of the operator surface.
- **Requirements:**
  - §4.3 records `supervise start` injects `ANTHROPIC_API_KEY` from a non-committed scoped source (C3).
  - §4.3 records the operator env overrides `FLYWHEEL_MODEL_TIER1/2/3` (Pi judgment tier; default tier-3 Sonnet, Opus opt-in) and the single daemon `claude`-baseline default (C6).
- **Dependencies:** C1 (Pi as sole holder), C3, C6.

## 3. New Specs

### `specs/credential-isolation.md` (C1, C2, C3) — NEW

- **Scope:** The cross-cutting credential-holder discipline — which process may hold a key in the deny-list, the scrub guarantee at every `claude` spawn boundary, and the scoped-injection rule for Pi. Owns the contract because it spans daemon env assembly, Pi process launch, and the tmux substrate handoff, none of which is the natural single owner.
- **Requirements:**
  - **(C1) Holder contract:** exactly ONE process — the Pi cognition process — MAY hold `ANTHROPIC_API_KEY`; the daemon process and every daemon-spawned `claude` child MUST NOT. Stated as an invariant: "no harmonik process other than Pi receives a key in the deny-list."
  - **(C1) Deny-list set:** names the credential deny-list normatively — `{ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN, CLAUDE_CODE_OAUTH*}` (glob-prefixed) — a mechanism-tagged fixed set, single source of truth referenced by C2 (scrub), C3 (Pi injection), and C7 (secret scan).
  - **(C2) Scrub guarantee:** the daemon's child-env assembly (`ClaudeEnvVars`, CHB-006) MUST remove every deny-list key from the constructed env — stated symmetric with the existing `HARMONIK_SECRET_*` strip (`internal/handler/claudehandler_chb006_024.go:200-212`) — at the SINGLE env-assembly boundary, not scattered `env -u` wrappers. The tmux substrate handoff (`tmuxsubstrate.go:179`) is an ASSERTION point, not a second scrub.
  - **(C2) Scrub invariant + named test:** "no daemon-spawned `claude` ever receives a deny-list key," locked by a named regression test (hk-4g32m).
  - **(C3) Scoped injection:** the credential reaches Pi via an EXPLICIT Pi-scoped env builder, NOT blanket `os.Environ()` passthrough (today: `shim.go:103`, `attach.go:58`, supervisor exec); the scoped env includes the deny-list key ONLY when launching Pi.
  - **(C3) supervise-start source:** `supervise start` injects the key from a NON-COMMITTED scoped source so a fresh Pi boot authenticates without a manual `export` (closing HANDOFF blocker #1).
  - Mechanism-tagged per architecture.md §4.4; cross-references `claude-launchspec.md` (env assembly) and `cognition-loop.md` CL-001 (Pi is the cognition process).
- **Dependencies:** none upstream (KEYSTONE). Downstream: `claude-launchspec.md` note (C2), `operator-nfr.md` note (C3).

## 4. Dependency Map

```
credential-isolation.md (C1: holder + deny-list)  ── KEYSTONE, no deps
  ├─ C2 scrub guarantee + claude-launchspec note   needs C1's deny-list
  └─ C3 Pi-scoped injection + operator-nfr note     needs C1's holder rule

cognition-loop.md CL-090 (C4: unified meter,       ── KEYSTONE (budget), no deps
                          finite default, max-runs)
  └─ C5 hard-halt wiring                            needs C4's exhaustion event
       ├─ handler-pause.md HP-012 cross-check       needs budget_scope field
       └─ control-points.md §4.5 budget_scope field  (additive; satisfies HP-012)

operator-nfr.md + cognition-loop note (C6 knobs)   ── independent, parallel
.gitignore + tooling + spec notes (C7)             ── independent, parallel
     └─ retry-budget note references C4's meter      (soft, not blocking)
```

**Ordering summary:**
- **Credential chain (sequential):** C1 → {C2, C3} (C2/C3 parallel after C1).
- **Budget chain (co-designed):** C4 → C5 (and within C5, control-points `budget_scope` field before the handler-pause cross-check that depends on it).
- **Independent (any time):** C6, C7.
- The two chains are mutually independent and proceed in parallel; the credential chain is the assessment-named *first* priority (the literal leak), the budget chain the *root-cause* fix (the wrong meter).

## 5. Goal → Area Traceability

| Goal (01-problem-space §2) | Spec area(s) | Component(s) |
|---|---|---|
| G1 single credential holder | `credential-isolation.md` | C1 |
| G2 scrub at every spawn boundary | `credential-isolation.md` + `claude-launchspec.md` | C2 |
| G3 Pi-scoped injection + supervise-start source | `credential-isolation.md` + `operator-nfr.md` | C3 |
| G4 unified meter (Pi + daemon claude) | `cognition-loop.md` CL-090 | C4 |
| G5 finite budget default | `cognition-loop.md` CL-090 | C4 |
| G6 budget-exhaustion hard-halt | `handler-pause.md` + `control-points.md` | C5 |
| G7 operator model-tier knobs | `operator-nfr.md` + `cognition-loop.md` | C6 |
| G8 uncommittable secret + dry-run | repo `.gitignore`/tooling + `cognition-loop.md` | C7 |
| G9 retry budget | `cognition-loop.md`/`execution-model.md` | C7 |

Every goal maps to ≥1 spec area; no spec area is listed that isn't justified by a goal.

## 6. What is NOT a component (out of scope, per §3 of problem-space)

- **Quiet-by-default daemon / no-auto-pull** (S2, hk-exd7m) — the incident TRIGGER; belongs to the `pilot` work. Not a credfence component.
- **Pi-driven dispatch + pause/resume control plane** (S5/S6/S9/S10) — `pilot`. credfence only WIRES budget-exhaustion into the existing handler-pause consumer; it does not design the `harmonik pause`/`resume` verb or the `operator_pause_status` producer.
- **Crash recovery / orphan sweep / reap-on-exit / single-flywheel lock** (lost issues #5/#8/#9) — the `reap` work.
- **A new secrets-management subsystem / vault** — the contract is "scoped non-committed source," not new infrastructure (problem-space §3).
- **CL-051 two-phase-done verification** (S8) — correctness/verification, not credential or spend; closer to `pilot`'s bridge work.
- **Token-accurate billing-grade cost ledger** — the meter is governance-grade approximation (problem-space §3).

## 7. Cross-component notes for the research/design passes

- **Single deny-list source of truth.** C1's deny-list set (`{ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN, CLAUDE_CODE_OAUTH*}`) is referenced by C2 (scrub), C3 (Pi injection), and C7 (secret scan). The design pass must place it where all three reference it (a named constant / a spec table), not duplicate it three times.
- **The budget event is the C4/C5 contract seam.** The exact event name + payload (`budget_exhausted` vs `flywheel_budget_exhausted`, and the `budget_scope` field) is the interface between the meter (C4, Pi-side) and the halt (C5, daemon-side handler-pause consumer). Design must pin it so both halves agree; `event-model.md` may need a coordination note.
- **`budget_scope` is a hard dependency (OQ-4).** control-points.md §4.5 lacks the field HP-012 needs (handler-pause.md §13 #3). credfence resolves it in C5 — confirm in the spec-draft pass that the Budget primitive (CP-001..005) gains it as an additive field.
- **Cost-attribution mechanism (OQ-1).** How daemon `claude` USD reaches Pi's meter (run_cost event vs Pi-side estimation from run events) is the open design decision for C4. Research pass to confirm the event surface; lean is a daemon-emitted approximate `run_cost`.
- **Open questions OQ-1..OQ-6** (in `01-problem-space.md` §7) are the research/design-pass agenda: cost attribution (C4), one-spec-or-two (C1), max-runs reset semantics (C4), budget_scope placement (C5), supervise-start source (C3), rate table (C4).
