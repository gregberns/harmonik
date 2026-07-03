# remote-substrate — Requirements (DRAFT v0)

> Status: DRAFT skeleton, to be hardened AFTER the problem statement is confirmed and
> the research lands. Companion: `01-problem-space.md`, `BRAINSTORM.md`.
> Convention: [MUST] / [SHOULD] / [MAY] / [TBD].

## A. Functional requirements

- FR1 [MUST] The daemon can dispatch a bead's implementer Claude Code session to a
  **remote worker** instead of localhost, and reconcile its commit back to box A's
  one-at-a-time merge flow.
- FR2 [MUST] "Where a session runs" is selected via a **substrate provider**
  abstraction; `local-tmux` is one provider, remote providers slot in beside it.
- FR3 [SHOULD] A worker can be registered declaratively (config file lists worker
  targets + their substrate type + capacity), no code change to add one.
- FR4 [OUT for v1 — D3] Crews stay on box A (simpler AND preferred); only per-bead task
  work goes remote. Remote crews explicitly deferred (would need network bus transport +
  an interactive-login box for `--remote-control`).
- FR5 [MUST] Repo/worktree state reaches the worker and resulting commits return,
  without corrupting the merge/skip-on-conflict flow.
- FR6 [SHOULD] Operator can still inspect a remote session (tmux attach over SSH, or
  equivalent) — preserve harmonik's inspectability principle.
- FR7 [MUST] A remote worker going away — at dispatch OR mid-bead — and any SSH/connection
  problem is **detected and raised** (not silently swallowed); the bead is recovered (re-queue
  or clean-fail), not silently wedged.
- FR8 [MAY] harmonik can *provision* a sandbox on demand (the "spin-up" provider),
  not just *connect* to a pre-existing one. (Phase 2 — see Q1.)
- FR9 [MUST — v1] Exactly **one** remote worker is configured (`.harmonik/workers.yaml`).
  Multi-host + round-robin/scheduling is parked for later.
- FR10 [MUST] The worker registry entry carries: `name`, `transport`, `host`, **`os`**
  (darwin|linux), `repo_path`, `max_slots`, **`enabled`** flag.
- FR11 [MUST] On startup the daemon **health-checks** each enabled worker (SSH reachable +
  tmux + claude present + repo present). A configured-but-failing worker is marked **unhealthy
  and skipped** — its config entry is NOT removed (it may be temporarily offline). The failure
  is raised clearly.
- FR12 [MUST] A worker can be **live-disabled/enabled** at runtime (`enabled` flag) without
  editing or deleting its config.
- FR13 [MUST] Run metadata records **which worker** a bead ran on **and its OS**, so a crew can
  SSH into the right box to remediate (e.g. install a missing dependency).

## B. Non-functional requirements

- NFR1 (throughput) [MUST] Adding one worker lifts sustained concurrent-bead
  throughput above the single-box `--max-concurrent` knee, without box A hitting
  disk/RAM ceilings.
- NFR2 (latency) [SHOULD] Per-bead remote dispatch overhead (place + sync + start) is
  small relative to bead runtime (target: [TBD], minutes-scale beads → seconds-scale
  overhead acceptable).
- NFR3 (isolation) [SHOULD] At least one provider gives real blast-radius containment
  (per-bead or per-crew sandbox), stronger than today's worktree-only separation.
- NFR4 (billing) [MUST — D2] Remote sessions MUST bill the **subscription**, never the API
  credit pool (API billing is too expensive). Phase 1 (remote Mac) → one-time interactive
  login; Phase 2 (container) → `CLAUDE_CODE_OAUTH_TOKEN`. The daemon MUST fail-closed if
  `ANTHROPIC_API_KEY` is present in a remote session's env. (See C1/C2.)
- NFR5 (reliability) [MUST] Network partition / worker death is a recoverable event,
  not a daemon wedge. Heartbeat + timeout + cleanup.
- NFR6 (security) [SHOULD] Credentials reach the worker without being baked into
  images or leaking into the repo/logs. [from R1 prior-art] Prefer **brokering secrets
  OUTSIDE the agent sandbox** (proxy git auth / Claude auth so tokens never enter the
  worktree), mirroring Claude-web / Devin / Codex.
- NFR8 (egress) [SHOULD, from R1] Support **two-phase network egress** on managed
  sandboxes — internet permitted during setup/dependency-install, restricted (default-deny
  + allowlist) during the agent phase, when prompt-injection exfil risk peaks. This is the
  industry-standard posture and directly hardens against the credit-burn / key-leak class.
  **Per-bead override:** some beads need network during the agent phase (e.g. integration
  tests) → egress policy is per-bead-configurable, default-deny with opt-in. Real egress
  isolation is a **Phase 2 (container)** capability; Phase 1 (bare remote Mac) = open egress.
- NFR7 (no regression) [MUST] Single-box local-tmux operation is unchanged when no
  remote workers are configured.

## C. Hard constraints (carried from research)

- C1 [auth/billing — from R5] Supported subscription-billed paths:
  (i) persistent remote **Mac** with a one-time interactive `claude` login (creds in
  Keychain / `~/.claude`); (ii) **`CLAUDE_CODE_OAUTH_TOKEN`** — a 1-year token from
  `claude setup-token`, injectable into Linux/ephemeral containers, subscription-billed.
  `ANTHROPIC_API_KEY` = API credit pool (avoid unless intended). Copying `~/.claude`
  across machines is NOT officially supported (device-bound, fraud-detection risk).
- C2 [ToS — from R5] Do not reverse-engineer Claude Code OAuth into third-party tools;
  do not set `ANTHROPIC_API_KEY` alongside subscription auth (key wins → silent API
  billing + auth failures).
- C3 [arch — from R3] Apple-Silicon workers are ARM64: Linux containers must be
  `linux/arm64` or they run under slow QEMU. An x86 VPS sidesteps this.
- C4 [remote-control — VERIFIED, MOOT for v1 per D3] `claude --remote-control` requires a
  full-scope **interactive subscription login**; it is blocked for BOTH
  `CLAUDE_CODE_OAUTH_TOKEN` AND `ANTHROPIC_API_KEY` (docs: code.claude.com/docs/en/remote-control).
  Since crews stay on box A (interactive login) in v1, no remote `--remote-control` is needed.
  Re-examine only if remote crews are ever revived (they'd require a logged-in box, never a
  token-container).
- C5 [tmux coupling — CONFIRMED R6] Spawn path = `handler.Substrate.SpawnWindow` →
  `tmux.OSAdapter` (`exec.Command("tmux",…)`). The remote impl wraps these in `ssh host --`.
  Seams: A (Substrate), B (paste/liveness), C (worktree). v1 needs A+B+C, not D.
- C6 [comms reach — DEFERRED per D3] Comms bus is a local unix socket (R6 seam D). Crews
  stay on box A → **no network bus transport needed for v1**; the bus is untouched.

## D. Out of scope (v1)

General cluster scheduler; elastic auto-scaling; multi-tenant hardening; moving
**crews/daemon/captain** off box A (D3); replacing tmux as the local inspectable layer.

## E. Open requirement questions (mostly resolved 2026-06-14)

- ~~FR4 remote crews in v1?~~ → RESOLVED: OUT (D3).
- ~~NFR4 subscription billing MUST or optional?~~ → RESOLVED: hard MUST (D2).
- NFR2/NFR3 numeric targets — still [TBD] (set in analyze/spec).
- FR8 phase placement → connect-in is v1 (Phase 1); provision/spin-up is Phase 2+.
