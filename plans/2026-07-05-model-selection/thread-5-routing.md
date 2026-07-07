# Thread 5 — The USE Mechanism: Routing Into Dispatch (THE PAYOFF)

**Status:** proposal drafted · **Reports to:** admiral
This is where evals + cost + Pareto get *used*: per task, decide which model runs it, hooked into
harmonik's dispatch, config-driven and overridable. shannon proposes; admiral decides/dispatches.

## 5.1 The one insight that shapes everything: harmonik has an OBJECTIVE verifier

Generic LLM routers/cascades are limited by **confidence calibration** — you can't trust a model's
self-rated certainty, so cascades over open-ended NL text need carefully tuned empirical thresholds
(Cascade Routing, arXiv 2410.10347: "the quality estimator is the single critical success factor").

**Harmonik does not have this problem for coding work.** It already runs an *objective, deterministic*
quality gate on every run: **tests pass · code compiles · linter clean · diff applies (non-ff merge) ·
agent-reviewer APPROVE verdict.** That gate is a free, reliable verifier — exactly what FrugalGPT's cascade
needs and what generic routers lack. So the **verifier-cascade** (cheap model first, escalate on gate
failure) is an unusually strong fit here and gets most of the published savings **with zero router training.**

## 5.2 Where the decision hooks in today (the real resolver)

Harness selection already runs through a **4-tier resolver**: `internal/daemon/harnessresolve.go:53`
(`resolveHarness`) — per-bead `harness:` label > (per-queue default, stub) > ... > global
`Config.DefaultHarness` (`--default-harness`, default `claude`). Workflow-mode resolves in
`moderesolve.go`. **Claude model** is chosen **per-DOT-node** via the `model=` attribute
(`dot_cascade.go` → `claudelaunchspec.go`) — so per-task Claude model routing is *already expressible* in the
DOT, and the eval program uses it to run Opus vs Sonnet concurrently.

**The routing decision is a natural extension of `resolveHarness`** — add a tier between the per-bead label
and the global default that consults the routing table (§5.3). No new subsystem; a new resolver tier.

**One real limitation:** Pi's *model* (MiniMax vs qwen3-coder vs ornith) is **daemon-global** config
(`harnesses.pi.*` in `.harmonik/config.yaml`, `cmd/harmonik/resolve_pi_config.go`,
`internal/daemon/pilaunchspec.go`) — one Pi model live at a time; switching needs a config edit + daemon
restart. So per-task routing among *open-weight* models isn't possible today without lifting Pi model
selection out of global config. Claude-tier routing (Opus/Sonnet/Haiku) has no such limit — the `model=`
seam already exists.

## 5.3 Proposed routing design (phased, lowest-risk first)

### Phase 0 — make cost trustworthy (prerequisite, small)
Fix the stale `pricingTable` (thread 2.3) + land Pi/Codex token extraction. Without this, no routing
decision has real cost data. **Dispatch these first.**

### Phase 1 — static category→tier routing table (no training, config-driven)
A checked-in policy table mapping **task category → model tier**, read at dispatch time. Category comes
from a bead label (e.g. `cat:mechanical`, `cat:review`, `cat:implement`, `cat:plan`) or is inferred from
existing labels. Table lives in `.harmonik/config.yaml`, e.g.:

```yaml
routing:
  default_tier: opus            # fallback
  categories:
    mechanical-edit: {tier: haiku}        # gofmt-ish, rename, single-file
    review:          {tier: sonnet}
    triage:          {tier: haiku}
    implement:       {tier: opus}
    plan:            {tier: opus}
    research:        {tier: sonnet}
  overrides:                     # per-bead label always wins
    respect_harness_label: true
```

Precedence: **per-bead `harness:`/`tier:` label > routing table > default_tier.** Keeps the existing manual
override (memory: never break the label escape hatch). Fully overridable, no ML.

### Phase 2 — verifier-cascade escalation (the big cost lever)
On a **transient/quality** run failure (gate fail, NOT infra fail like disk/merge-race), **re-dispatch the
same bead one tier UP** instead of just retrying at the same tier. This is FrugalGPT's cascade with
harmonik's objective gate as the verifier:
- cheap tier attempts → gate fails → escalate to next tier → gate passes → merge.
- Bounded (max 1–2 escalations), and only for *quality* failures (must distinguish from the infra-failure
  taxonomy already in the daemon: `merge_build_failed`, `non_ff_merge`, disk-pressure, API-500 — those
  retry same-tier, they're not a model-capability signal).
- This directly reuses harmonik's existing re-dispatch machinery; the change is *which tier* the retry
  targets.

### Phase 3 — historical per-category win-rate routing (online, telemetry-driven)
Log `(category → model → gate-pass?)` from `session-data.jsonl` and route each category to the **cheapest
model with an acceptable historical pass-rate**. This is a lightweight online bandit — no offline training,
learns from harmonik's own eval + production telemetry (thread 1). Escalation threshold stays a single
tunable dial (RouteLLM/Hybrid-LLM both expose one).

### Phase 4 (optional, only if telemetry shows headroom) — trained router
RouteLLM matrix-factorization or Hybrid-LLM difficulty router, simulated offline first on our
`session-data.jsonl` corpus (the way RouterBench lets you simulate on 405k precomputed inferences). Only
worth it if Phase 1–3 leaves measurable money on the table.

## 5.4 Why this ordering

Phases 1–3 are **[NO-TRAIN]**, config-driven, and reuse existing dispatch/gate/re-dispatch machinery, so
each is a small bead, not a subsystem. The objective coding verifier (5.1) means the cascade (Phase 2)
works without the calibration tuning that makes generic cascades fragile. Phase 4 is deferred until data
justifies it.

## 5.5 Blockers / decisions for admiral
- **Per-task Pi model** — routing to different open-weight models per task needs Pi model selection to move
  from daemon-global config to per-dispatch (currently a restart-gated global). Flag: is this in scope?
- **Cost policy** (thread 2.4) — the tier thresholds in the Phase-1 table encode a willingness-to-pay the
  operator must set.
- **Category taxonomy** — needs to agree with the eval program's task categories (thread 1) so quality
  data and routing table use the same buckets.

## Open items
- [ ] Confirm task-category taxonomy against thread 1 (eval program).
- [ ] Get cost-policy answers (thread 2.4) to fill real tier thresholds.
- [ ] Propose Phase-0 + Phase-1 as concrete beads to admiral once taxonomy + prices settled.
