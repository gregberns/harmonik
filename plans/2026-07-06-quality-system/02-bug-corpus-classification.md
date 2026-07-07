# Daemon bug corpus — classification for a regression/test corpus (last ~7 days)

*Planning subagent → quality-system. Window: commits since 2026-06-29 + open/closed bug beads + memory
incident bank + HANDOFF-gb-pr-19. Lens: TASK PROCESSING pipeline (queue-submit → worker-select →
harness-launch+agent_ready → model-selection → sandbox+provider-comms → edit+commit → commit_gate →
DOT-review → merge). Goal: generalize concrete bugs into CLASSES, each a candidate "prove this whole
class works" test target; cross-ref the failure-corpus table in
`../2026-07-05-agent-world-models/daemon-testbed-design.md`.*

## 1. Concrete bugs found (this window)

| id / commit | one-line | cluster | pipeline-stage |
|---|---|---|---|
| hk-pkugu / 3b56822f | Pi harness model leak: claude tier-3 default shadows configured pi model | C4 model-selection | model-selection |
| hk-lfrub / 46556d61 | DOT per-node `model=` pin leaked across harness families (claude pin hit pi) | C4 model-selection | model-selection |
| hk-6atjk / 9dc09433 | Pi exec child env missing PATH → launch fails | C5 env/sandbox | harness-launch |
| hk-4ir08 (open) | ornith DGX is a reasoning model, incompatible w/ pi harness (content:null, no tool_calls) | C6 wire-format | sandbox+provider-comms |
| hk-u69my / bc25/a5e4 | srt sandbox blocked sandboxed Pi reaching LAN/loopback model; false-green egress test | C5 env/sandbox | sandbox+provider-comms |
| hk-ybuts / 37eca951 | srt sandbox-wrap wrongly applied to remote (tcp://) runs | C2 remote-vs-local | sandbox+provider-comms |
| hk-ybuts / 4f9b8589 | SessionIDCaptured exec-path not wired into DOT cascade → Pi no-commit | C1 state/lifecycle | edit+commit |
| hk-z4nif / 93072cc9 | pasteTarget not nil'd for SessionIDCaptured harness in DOT cascade | C1 state/lifecycle | harness-launch |
| hk-ytzj2 / 632406ce | DefaultHarness not wired into tier-4 dispatch; review not pinned to claude-code | C4 model-selection | worker-select |
| hk-u6zp (open P0) | daemon DROPS per-item workflow_ref/workflow_mode at queue-submit (rpc rebuild copies 4 fields) | C7 boundary/wire | queue-submit |
| hk-y3o51 (closed P0) | queue-submit hardcodes review-loop, ignores config default workflow_mode | C7 boundary/wire | queue-submit |
| hk-4tjt6 / 710139ce | level-2 per-queue gate counted local+remote → capped all-remote queues | C3 concurrency/gate | worker-select |
| hk-hs7ex / 60708048 | split concurrency gate: local hard-cap vs remote capacity (local-only leak) | C3 concurrency/gate | worker-select |
| hk-5qp7z / d92b16de | concurrent worktree-create race (add+HEAD-resolve not serialized) → empty HEAD | C3 concurrency/race | harness-launch |
| hk-lt091 / 2b92169d | remote git race: fetchBaseOnWorker not serialized under mergeMu | C3 concurrency/race | merge |
| hk-5z1f0 / fd92adfc | remote reviewer agent_ready_timeout under 6 concurrent slots; raise 90→150s + spawn semaphore | C3 concurrency + C1 | harness-launch+agent_ready |
| hk-xkou8 / e0b02f77 | reviewloop unbounded sess.Wait in ErrAgentReadyTimeout → idle hang | C1 state/lifecycle | DOT-review |
| hk-4hso5 / 6ac86a94 | workloop unbounded sess.Wait in ErrAgentReadyTimeout path (same class) | C1 state/lifecycle | harness-launch |
| hk-up1pk / 29c74e89 | bounded sess.Wait reap regressed; re-applied w/ regression tests | C1 state/lifecycle | DOT-review |
| hk-vv10r (open) | DOT reviewer verdict read ErrMalformed (ssh-cat mid-write) on gb-mbp | C6 wire-format | DOT-review |
| hk-a74e5bde | DOT remote reviewer brief + stale-verdict cleanup not routed through worker runner | C2 remote-vs-local | DOT-review |
| hk-thbbv/hfmg6 (open) | spurious flagless REQUEST_CHANGES wedges run forever; later APPROVE ignored | C1 state/lifecycle | DOT-review |
| hk-vbv3b / 2aac5a58 | genuine APPROVE + rebase_dropped_commits wrongly reopened bead | C1 state/lifecycle | merge |
| hk-whru3 / a749486c | reconcile-close advisory-RC bead when rebase drops commits | C1 state/lifecycle | merge |
| hk-44ab2 / 9b8288ad | merge-gate hard-failed on cold-cache build failure; add retry | C5 env/disk | commit_gate |
| hk-5uezz (open) | `go clean -cache` mid-build wipes SHARED cache → kills in-flight builds | C5 env/disk | commit_gate |
| hk-nlhys / e723d9a9 | stale-cache gate failure needed re-commit (CI cache-wipe pattern) | C5 env/disk | commit_gate |
| hk-qe736 / f4dd8cbc | stale agent worktrees not force-unlocked/age-pruned | C1 state/lifecycle | harness-launch |
| hk-gf59k / 00543657 | QM-025 false-defer against a CLOSED blocker + deferred-only idle stall | C1 state/lifecycle | queue-submit |
| hk-aiw63 (closed) | claude dirties tracked .claude/settings.json in worktree → rebase aborts | C1 state/lifecycle | merge |
| PR-19 (open) | Claude 2.1.201 trust/permissions modal wedges EVERY dispatch pre-agent_ready | C8 version-drift | harness-launch+agent_ready |
| hk-9s5fx (open) | daemon-launched pi auto-loads dead flywheel ext → kerf-next fork bomb | C5 env/sandbox | harness-launch |

## 2. Cluster summary (generalized test targets)

| Cluster | Family | #bugs (window) | A single scenario/harness test for the CLASS asserts… |
|---|---|---|---|
| **C1 state/lifecycle drift** | most common | ~12 | Every abnormal harness exit (no terminal event, agent_ready timeout, pane death, stranded in_progress, flagless RC, rebase-dropped-commits) is BOUNDED and resolves to exactly one of {re-dispatch, safe-fail, merge-on-approve} — never an unbounded hang and never a false close/reopen. |
| **C2 remote-vs-local divergence** | boundary | ~3 | The DOT/review/sandbox/verdict path behaves identically local vs remote(tcp://): the remote runner is exercised through the SAME code seam, no local-only wiring skipped, no sandbox-wrap misapplied to tcp runs. |
| **C3 concurrency / race / gate** | race | ~5 | Under N concurrent slots (local+remote mix): worktree-create, base-fetch, and same-file merge are serialized/fail-safe; the level-2 per-queue gate counts the RIGHT pool so all-remote queues aren't starved and local stays hard-capped. |
| **C4 model-selection** | state | ~3 | The resolved model reaching the harness == configured model for THAT harness family (pi≠claude), per-node `model=` pins don't leak across families, review pins to claude-code, DefaultHarness flows to tier-4 dispatch. |
| **C5 env / disk / sandbox** | env | ~6 | Launch env is complete (PATH, piHome, no dead extensions); disk-pressure cache-wipe is reactive+scoped (never mid-build on shared cache); cold-cache build failures RETRY not hard-fail; sandbox egress lets the harness reach its provider (LAN/loopback/remote) while still blocking what it should. |
| **C6 boundary / wire-format** | boundary | ~2 | Verdict/review.json reads survive partial writes (ErrMalformed → salvage, not crash); provider responses with content:null / no tool_calls are detected and rejected with a clear failure, not a silent no-commit. |
| **C7 queue-submit field-fidelity** | boundary | ~2 | queue-submit round-trips EVERY per-item field (workflow_ref, workflow_mode, model, harness) — no rpc rebuild dropping fields, no hardcoded review-loop default. |
| **C8 version-drift (harness UI)** | version | ~1 | A real git-worktree e2e launch clears every Claude startup gate (folder-trust, permissions-consent, onboarding) to agent_ready — the gate that unit/spec tests miss because it's a version×worktree interaction. |

### Cross-ref vs the existing failure-corpus table (daemon-testbed-design.md §"How it maps")
That table already seeds: concurrent same-file merge race (C3), disk cache wipe (C5), reviewer-pane death
(C1), stranded in_progress (C1), concurrent-slot cold-start (C3+C1, hk-5z1f0), malformed review.json (C6),
mid-flight cancel (C1). **Gaps it does NOT yet cover:**
- **C4 model-selection entirely** — no scenario proves the pi/claude/codex configured model actually reaches the harness (whole pi-model-leak week).
- **C2 remote-vs-local divergence** — table's twins are local; no assertion the remote (tcp://) path exercises the same seam (srt-on-remote, DOT-brief-via-worker-runner, ssh-cat malformed).
- **C6 provider wire-format** — malformed review.json is listed, but NOT provider-side content:null/no-tool_calls (reasoning-model incompat, hk-4ir08).
- **C7 queue-submit field-fidelity** — no scenario feeds a per-item custom workflow/model and asserts it survives to dispatch.
- **C8 harness-version startup-modal** — explicitly called out in PR-19 as an untested class (needs real-worktree launch, not temp-dir config test).
- **C1 flagless-REQUEST_CHANGES wedge + rebase-dropped-commits reopen** — richer than the pane-death row; add as adversarial reviewer verdicts.

## 3. Ranked top task-processing coverage gaps (support it, no e2e proof it works)

1. **Model actually reaches the harness (per family).** We support pi/claude/codex + per-node model pins,
   but an entire week of leaks (hk-pkugu/lfrub/ytzj2) shipped because nothing asserts resolved-model ==
   configured-model at the launch seam. Highest-frequency recent class, zero corpus coverage.
2. **Remote (tcp://) path == local path.** gb-mbp is the throughput critical path yet the remote DOT/
   review/sandbox seam repeatedly diverged (srt-on-remote, DOT-brief routing, ssh-cat malformed,
   agent_ready under concurrent slots). No scenario runs a bead through the remote runner end-to-end.
3. **Provider comms boundary through the sandbox.** We "support" ornith/pi providers but hk-4ir08 (reasoning
   model, content:null) and hk-u69my (sandbox blocked loopback model) prove no test drives
   harness→sandbox→provider→tool_call→commit; failures surface as silent no-commit.
4. **queue-submit → dispatch field fidelity.** Custom DOT workflows / non-default modes / model overrides
   are silently dropped at the rpc rebuild (hk-u6zp, hk-y3o51). No test submits a fully-specified item and
   asserts the worker launched with those exact fields.
5. **Real Claude-worktree startup to agent_ready.** PR-19 outage: every dispatch wedged on the 2.1.201
   permissions modal while unit/spec tests were green. No git-worktree e2e clears the startup-modal gates —
   the single most impactful class (fleet-wide dispatch down) and completely unproven.
</content>
</invoke>
