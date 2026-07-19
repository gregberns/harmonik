# code-revamp — COORD (git-verified reset, 2026-07-18)

> **Source-of-truth reset (admiral, operator-approved, 2026-07-18).** This file was rebuilt
> from `git log HEAD` (not prior COORD prose). Branch tip `2d308836`. `go build ./...` +
> `go vet ./internal/daemon/` GREEN. Every "DONE" line below carries the landing SHA it was
> verified against, on this branch's ancestry.
>
> **History (c054–c087, the full narrative) is archived:** [`COORD-archive-2026-07-18.md`](COORD-archive-2026-07-18.md) (1308 lines).
> Do NOT re-narrate it here — this file holds only current git-verified state going forward.

---

## End-game reconciliation — DONE (with SHA) vs GENUINELY-REMAINING

Every code track is landed on tip. **No un-gated code work remains.** All remaining items are
operator/admiral/real-box-gated (a GitHub action, a design ruling, or a human-box run).

### ✅ DONE — verified in `git log HEAD`

| Track / workstream | Landing SHA(s) | Note |
|---|---|---|
| **M6 WS2 — docker E2E** | WS2.1+2.2 `5853f458` · WS2.3 compose E2E `c993b606` · WS2.4 subprocess smoke `1dbfddfe` (+docker-form `f84ae433`) · WS2.5 harness doc `42fe8c7e` | WS2.3 was M6's hardest gate — green on-box (`make test-docker-e2e` EXIT 0). |
| **M6 WS4 — core-loop-proof** | WS4-0 env/cred `533cd092` · WS4-1 reconcile `3e4bb3cb` · WS4-2 credfence `164ccf71` · WS4-3 provision+deterministic-dispatch `134a9781`,`4ae8a549` · WS4-4 empty-model+pi-dot `5e8e6c50`,`4ec0045f`,`58748daa` · WS4-5 LT-leg gate `94e72916` | Wall-1 landed; Wall-2 codex-half fixed (c072); DOT resume-feedback product defect FIXED + proven green e2e `8dbe5a17` (c075). |
| **M6 WS5 — assessor** | WS5-1 `331d92b7` · WS5-2 `7c98b890` · WS5-3 `36b159d5` · WS5-4 `511d2d92` · WS5-5 `69f3f1cd` · WS5-6 `2fd53b62` · WS5-7 `1b0bf440` | WS5-1..5-7 landed. WS5-8 capstone dry-run = a GATE, not a commit — see remaining. |
| **M6 WS1 — gate scaffolding** | WS1.2/1.3 remote-E2E-via-CI `4caa9822` · WS1.5 gate-map+risk-tiers `a9006fb4` | WS1.1 CI-required flip itself = a GitHub action — see remaining. |
| **Lint remediation (Track 2)** | L1 `7844cb6a` · L3 `9bfcc783` · L4 `4a9aa2b6` · L5 `3de17ed9` · L6 `1e5bbcc5` · L7 `7399286d` · L2 daemon `4177c8d6` · depguard-core `6b06a70d` · depguard-gate `33296fb2` | Real CI gate `golangci-lint --new-from-rev=origin/main` = 0 issues (c066, independently re-verified). WS1.1 green-tree prereq MET. |
| **Giant-retirement (Track 3)** | boot-config B1–B6 `6cd0bc98`,`88cd2c35`,`389d5a81`,`fcd9d8bd`,`94a7ea75`,`ca5801c4` · socket-router SR-1→SR-3 `a552dc06`,`9d7d56a3`,`6984b8fa`,`080adf3e` | `startWithHooks` 1617→153 lines linear phase calls; `handleSocketConn`→`router.Dispatch`. `bootconfig`+`router` unit tests green. **Independent review 2026-07-18: BOTH APPROVE — boot-config (all 10 §9 invariants confirmed); socket-router (clean 1:1 carve, byte-identical wire, concurrency-safe, no import cycle).** |
| **Mega code-review (Track 4)** | Waves 2a/2b/2c + C1 (c079–c082) · Wave 3 dead-code A1 `31027f2b` (+A2–A4,A9, c083) · Wave 4 batches 1–4 `d4c8f480`…`6776fa9d`, log `2d308836` (c084–c087) | §c medium backlog CLOSED + LOW/nit swept. |

### ▶ GENUINELY REMAINING — all gated (nothing an executor can start un-gated)

| Item | Gate | Prereq status |
|---|---|---|
| **WS1.1 CI-required-status-check flip** | admiral/operator GitHub branch-protection action; sequenced LAST | green-tree prereq **MET** (lint done) — ready to flip |
| **WS5-8 capstone dry-run + gap6 DOT round-trip determinism** | operator ② design ruling (per-node prompts, path A) then a live run | product defect ① DONE/proven (`8dbe5a17`); DOT-contract direction already chosen by operator (c075); the per-node-prompt wiring + capstone run remain |
| **codex-A real-box live re-capture** | operator + real-box (auth/tmux) window | parked; does not block the flip |
| **WS4 pi DGX real-box tail** | operator/infra | vLLM wedge was infra (c073 cleared); no code owed |
| **PR #20 push + M4 real-`gb-mbp` proof** | operator + human-box | ready to push; off critical path |

### 🐞 Known defects — filed as beads 2026-07-18
`hk-n1kln` (P2, hooksystem/eventbus `bus.Drain` flaky test race) · `hk-uhxwd` (P3, `TestSHINV005CorpusLint` schema drift) · `hk-1dgy0` (P2, CP-055/056/057 runtime-policy `LoadDotWorkflowWithPolicy` unwired).

---

## Log (fresh — c088 onward)

### c088 · 2026-07-18 · captain · Source-of-truth reset — git-verified end-game reconciliation
Rebuilt COORD from `git log HEAD`; archived c054–c087 to `COORD-archive-2026-07-18.md`. Root cause of
three consecutive stale directives (lint / giant-retirement "start now"): giant-retirement landed in git
2026-07-17 (`6cd0bc98`..`080adf3e`) but was never logged in COORD, so registry rebuilds from COORD prose
missed it. This reset closes that gap: **every code track is DONE on tip; all remaining work is gated.**
Independent review of the previously-unreviewed giant-retirement diff commissioned 2026-07-18 — **both
APPROVE** (boot-config 10/10 invariants; socket-router clean 1:1 carve, byte-identical wire). **Next COORD entry = c089.**
