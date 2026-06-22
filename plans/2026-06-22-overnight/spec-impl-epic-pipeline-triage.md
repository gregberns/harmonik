# Spec-impl epic pipeline — STEP-1 triage map (stilgar, 2026-06-22)

Triage of all 11 `kind:spec-parent` epics for the captain's autonomous triage→audit→close pipeline.
Method per epic: child done-ness sweep + code-confirm + spec-version drift (epic-scoped vs on-disk) + conflict-surface vs paul's hard-hold daemon files (dot_cascade/workloop/daemon/stalewatch/reviewloop).

**Universal finding: EVERY epic has spec-version drift** — all were scoped to an older spec version and the spec has since evolved. So a "conformance audit vs *current* spec" is the right move everywhere, and the version-delta is the highest-value audit section. Biggest drift: Execution Model v0.3.3→v0.9.0.

---

## A. CLEAN — audit-ready now (implemented, no/low daemon conflict)

| Epic | Spec | Children | Drift | Daemon conflict | State |
|------|------|----------|-------|-----------------|-------|
| hk-hqwn Event Model | event-model.md | all closed | v0.3.3→**v0.6.4** | none (internal/core) | **audit IN FLIGHT** = hk-pqgtm |
| hk-872 Beads Integration | beads-integration.md | 57/57 closed | v0.4.1→**v0.7.0** | touches workloop.go (read-only audit safe; only a *fix* would gate) | audit-ready |
| hk-i0tw Scenario Harness | scenario-harness.md | 54/54 closed | v0.2.0→v0.2.2 (observational) | **ZERO** (internal/scenario isolated) | cleanest |

→ These are the realistic autonomous audit→close pipeline: hqwn (running) → i0tw → 872.

## B. IMPLEMENTED but DAEMON-CENTRAL — defer audit until paul settles

A *verify-only* audit is read-only so technically safe, BUT it reads paul's **actively-edited** hold files (hk-1veco in flight, all 5 files edited <48h) = unstable conformance view, and any gap-fix would land squarely in paul's hold.

| Epic | Spec | Children | Drift | Daemon conflict |
|------|------|----------|-------|-----------------|
| hk-8mup Process Lifecycle | process-lifecycle.md | 63/63 closed | v0.4.1→v0.5.3 | **all 5 files** (this IS the daemon spec) |
| hk-8i31 Handler Contract | handler-contract.md | 82/82 closed | v0.3.3→v0.5.4 | **all 5 files** (deep: 37 refs workloop) |
| hk-b3f Execution Model | execution-model.md | 109/109 closed | v0.3.3→**v0.9.0** (biggest) | **all 5, CENTRAL** (daemon state machine built on it) |
| hk-zs0 Architecture | architecture.md | 62/62 closed | v0.3.1→v0.3.2 | dot_cascade/daemon (AR cites) — **FROZEN by design to 2027-01-01**, leave it |

## C. HAS REAL UNDONE WORK — escalate for sizing, NOT a clean audit-close

| Epic | Spec | Open | Drift | Nature of open work |
|------|------|------|-------|---------------------|
| hk-63oh Reconciliation | reconciliation/spec.md | ~18/82 | v0.4.0→v0.4.5 | verdict staleness→execution chain (.34/.35/.37/.38), S01 lib contract (.8), post-MVH sensors. **Some bead/code drift** — several open beads' impl files already exist → unclear if genuinely undone or just unclosed |
| hk-sx9r Operator NFR | operator-nfr.md | 7/84 | v0.4.1→v0.5.5 | **config knobs named in spec but UNWIRED in code**: config-inventory precedence-reader (ON-004), drain timeouts (ON-029), exhaustion protocol skeleton (ON-048), multi-daemon cmds (ON-041) |
| hk-8mwo Workspace Model | workspace-model.md | 4/72 | v0.4.2→v0.4.5 | post-MVH conflict-resolver/escalation — functions exist but **unwired in daemon loop** (deferred by design) |
| hk-a8bg Control Points | control-points.md | 18/69 | v0.3.2→v0.4.3 | **all post-MVH** (policy grammar/sensors/config). Daemon conflict **MINOR/nil-safe** — active evaluator is in dot_gate.go (NOT a hold file), less than feared |

---

## Recommendation

1. **Autonomous now:** finish hk-pqgtm → close hk-hqwn on green → dispatch hk-i0tw audit → then hk-872 audit → close each on green. (3 clean epics.)
2. **Defer (greenlight when paul settles):** hk-8mup, hk-8i31, hk-b3f audits — daemon-central, unstable to audit mid-flight. hk-zs0 stays frozen.
3. **Escalate for sizing (not audit-close):** hk-63oh + hk-sx9r have **near-term real gaps** (verdict-exec chain; operator config-wiring). hk-8mwo + hk-a8bg are post-MVH-deferred — fine to leave, just relabel.
4. **Cross-cutting:** every epic's bead description has a stale spec version — a cheap mechanical cleanup is to resync each epic's scoped-version metadata to the on-disk version.
