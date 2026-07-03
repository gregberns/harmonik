# 07 — Tasks: implementation beads for codex-harness

> Plan-jig Pass 7. The filed, ordered, dependency-wired bead corpus. All beads labelled
> `codename:codex-harness`, status **open**, **NOT dispatched** (the captain handles dispatch).
> Dependencies wired via `br dep add` (`blocks` type); verified acyclic (`br dep cycles`: none).
> 20 beads total = 18 implementation + 2 jig-required test beads.

## The bead corpus (by component / landing step)

| T# | Bead | Title (abbrev) | Depends on |
|----|------|---|---|
| **C1 — Harness seam** ||||
| T1 | `hk-e8omz` | define `Harness` interface + CompletionMode/SessionIDPolicy enums + `core.AgentTypeCodex` | — (root) |
| T2 | `hk-3kyh3` | implement `ClaudeHarness` (no behavior change) + golden + side-effect-parity tests | T1 |
| T3 | `hk-hj9ld` | route registry + `launchSpecBuilder` lookup off the resolved harness (claude-only) | T2 |
| **C4 — Selection & config** ||||
| T4 | `hk-y01k6` | `ResolveHarness` precedence resolver + `Config.DefaultHarness/CodexBinary` + `--default-harness` + tests | T1 |
| T5 | `hk-u67of` | parse DOT `harness`/`agent_runtime` + `reviewer_harness` node attributes | T4 |
| T6 | `hk-4x3rg` | per-queue `harness` default field in `queue/types.go` (GATED on named-queues) | T4 |
| **C2 — Codex adapter** ||||
| T7 | `hk-rgxwd` | `buildCodexLaunchSpec` — `codex exec --json --sandbox workspace-write -a never -C` | T1 |
| T8 | `hk-m57va` | `CodexHarness` + JSONL parser + captured `thread_id`; Completion=ProcessExit, SessionID=Captured | T7, T3 |
| T9 | `hk-bpxci` | guarantee `Refs:<bead>` trailer + deterministic commit-after-exit fallback | T8 |
| **C3 — Auth/billing guard** ||||
| T10 | `hk-jxgnp` | strip `OPENAI_API_KEY`+`CODEX_API_KEY` from codex child env (empty overrides) | T8 |
| T11 | `hk-tu48u` | billing guard: `forced_login_method=chatgpt` + pre-flight `assertChatGPTPlan` + events; fail closed | T10 |
| **C5 — Workflow / review-loop** ||||
| T12 | `hk-xhawy` | cascade routes implementer AND reviewer through the resolved harness | T3, T4, T8 |
| T13 | `hk-o90sl` | `Completion()` gate at `dot_cascade.go:643` — skip `pasteInjectQuitOnCommit` for ProcessExit | T12 |
| T14 | `hk-iv748` | `reviewer_harness` resolution (default = implementer harness) + optional override | T12, T5 |
| **C6 — Migration / test / docs** ||||
| T15 | `hk-of3h4` | codex twin binary — scripted JSONL + `Refs:` commit, 4 variants | T1 |
| T16 | `hk-hwwlk` | regression golden: no-selection = byte-identical claude (the N-1 proof) | T13 |
| T17 | `hk-u1id4` | operator guide + MUST-TEST checklist (env precedence, #2000 org-key audit, reviewer-verdict) — **AC MUST enumerate the 2 empirical checks below as distinct, signed-off items (R3.5, R6.5)** | T11, T14 |
| T18 | `hk-vfkyl` | land `specs/harness-contract.md` from `SPEC.md` (spec-first normative contract) | T16 |
| **Jig-required test beads** ||||
| — | `hk-vfmn9` | **scenario:** codex-adapter full lifecycle on twin substrate (run_started→reviewer_verdict→run_completed, `Refs:` commit) | T13, T15 |
| — | `hk-qxfj0` | **explore:** codex selection — `harmonik queue dry-run` shows resolved harness per item; bead-label override | T4, T5 |

## Execution order (topological)

```
T1 ──┬─ T2 ── T3 ──────────────┐
     ├─ T4 ──┬─ T5 ───────────┐ │
     │       └─ T6 (gated)    │ │
     ├─ T7 ── T8 ─┬─ T9       │ │
     │            ├─ T10 ─ T11 │ │
     │            └────────────┴─┴─ T12 ── T13 ──┬─ T16 ── T18
     │                                  │  T14 ──┘   (+ hk-vfmn9 scenario ← T13,T15)
     └─ T15 ───────────────────────────┘          (+ hk-qxfj0 explore ← T4,T5)
                                       T17 ← T11, T14
```

**Ready at start (no blockers):** T1 (`hk-e8omz`). Once T1 lands, T2/T4/T7/T15 unblock in parallel.
The captain dispatches `br ready` / `kerf next` beads only — blocked beads must not be dispatched
(a blocked bead insta-fails at dispatch).

## Landing-step grouping (from 06-integration.md)

1. **C1 (T1→T2→T3)** — keystone; claude byte-identical, mergeable first.
2. **C4 (T4,T5,T6)** — selection plumbing; still claude by default.
3. **C2 (T7,T8,T9)** — codex adapter; reachable only when C4 routes to codex (off by default).
4. **C3 (T10,T11)** — billing guard; must land before any real codex run is enabled.
5. **C5 (T12,T13,T14)** — workflow/review-loop routing + the `Completion()` bypass.
6. **C6 (T15,T16,T17,T18 + hk-vfmn9, hk-qxfj0)** — twin, regression, docs, spec; the enable + N-1 proof.

## Notes for the implementing session

- **Do NOT enable codex in production** (step 6 flip) until the C3/C6 MUST-TEST checklist passes on
  the pinned codex version (env precedence, `forced_login_method` honored by `exec`, #2000 org-key
  audit, reviewer-verdict reliability).
- **T13 anchor is load-bearing:** the `Completion()==ProcessExit` bypass lands at the
  `go pasteInjectQuitOnCommit(...)` launch in `dot_cascade.go:643`, NOT in `workloop.go`.
- **Scenario test (`hk-vfmn9`) authoring:** scenario tests boot real daemons and exceed the 30-min
  commit budget — author via a worktree sub-agent + targeted fast gate, not the live daemon
  (per `reference_scenario_test_authoring`). The daemon gate SKIPS `//go:build scenario` tests.
- **T6 is gated** on the named-queues work; if that hasn't landed, ship the bead/node/global tiers
  and leave T6 for a follow-up — not a blocker.
- Each bead carries a `[Cn/Tn]` tag in its title mapping back to its `05-specs/Cn-*.md` change spec.
