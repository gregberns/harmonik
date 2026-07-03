# Pi-in-a-sandbox — Problem Space

> Seeded from the authoritative implementation brief at
> `plans/2026-07-02-pi-sandbox/HANDOFF.md` (companion research: `README.md` same folder).
> The brief is the design; this artifact formalizes it into the kerf record. Do not re-derive —
> the brief is authoritative. Locked operator decisions are marked **[LOCKED]**.

## Summary

Give the harmonik daemon the ability to run the **Pi agent harness inside an OS-level sandbox**
for per-bead jobs, so Pi (a non-Claude harness we trust less to respect worktree discipline)
**cannot write to the host's main repo or main branch**. Isolation = own filesystem view
(writable only inside its worktree + caches + tmp), own git branch. The daemon dispatches a bead →
launches Pi wrapped in `srt` in an isolated git worktree → Pi works the bead → the result branch is
merged as today → the sandbox exits with the process.

## Goals

- Daemon can launch Pi confined by an OS sandbox so it cannot write outside its worktree / git-object
  paths / caches / tmp.
- Pluggable, config-driven backend (`sandbox: {backend: srt|none}`) — no hard-coded framework.
- Warm build-cache reuse (Go especially) so spin-up isn't dominated by recompiling.
- The sandboxed Pi can still call `br`, `harmonik comms`, `queue submit`, `gh`, and reach its model
  API (OpenRouter etc.).
- macOS support first, Linux second (same `srt` interface).

## Non-goals (out of scope for v1)

- Full **crew** (queue-owning / subagent-delegating) in a sandbox — v1 is per-bead job first.
  (Broader case: `plans/2026-06-30-distributed-fleet/00-the-case-for-isolated-crew.md`.)
- Non-Pi harnesses in the sandbox (the seam should allow it; proving it is later).
- OrbStack / container backend — the *escalation* backend behind the same config seam, for when a
  true VM boundary is needed. Not v1.
- No Pi harness work: Pi is already fully implemented and harness-blind (PI-010…050 landed). This is
  purely an isolation/substrate task.

## Locked decisions (operator design steer — do NOT re-open)

1. **[LOCKED]** Adopt `@anthropic-ai/sandbox-runtime` (`srt`) — do NOT hand-roll SBPL/bubblewrap.
2. **[LOCKED]** Support BOTH macOS and Linux; ship **macOS FIRST**.
3. **[LOCKED]** Pluggable config-driven backend `sandbox: {backend: srt|none}` — no hard-coded
   framework, fail-loud on missing keys (locked no-hardcoded-defaults principle).
4. **[LOCKED]** v1 = per-bead Pi job sandboxed (not a full crew), argv-wrap with `srt` inside the
   existing substrate, no container.
5. **[LOCKED — RESOLVED OPEN ITEM]** v1 network mode = **OPEN**. Rely on the filesystem boundary
   ("Pi cannot write main") as the core v1 isolation guarantee; tighten to an egress allowlist in a
   later pass. (Admiral's call; operator leaned this way.)

## Constraints (each can veto a design)

1. **Both platforms, macOS-primary.** `srt` covers both (Seatbelt on mac, bubblewrap+seccomp on
   Linux); verify Linux in CI/scenario tests.
2. **Lightest real isolation.** `srt`/Seatbelt is a policy on a host process, not a VM — accept that;
   it is the industry choice and uniquely good at warm-cache reuse.
3. **Warm cache is first-class.** Native host FS on mac → cache reuse is free (no VirtioFS tax).
   Design caches as read-only warm bases + per-run private writable area — never a shared concurrent
   writer (avoids the cache-reaper TOCTOU class of bug harmonik has already been bitten by).
4. **Daemon-drivable.** `srt` execs a subprocess tree — spawn, stream, wait-on-exit like any process.
5. **Attachable.** Native host process → `tmux attach` should work normally (validate, low risk).
6. **No hard-coded backend/credentials.** Backend + Pi provider/model/keys are operator config; fail
   loud if unset.

## Success criteria (concrete, verifiable)

- A bead dispatched to a sandboxed Pi run **commits to its own branch** successfully inside the
  sandbox.
- A write to a main-repo file (outside the run's worktree) from inside the sandbox is **denied**.
- The result branch **merges** back as today.
- `sandbox: {backend: none}` is a no-op (today's behavior); the sandbox path is gated entirely by
  config, fail-loud on missing required keys.
- The sandboxed Pi successfully reaches the local daemon (over the unix socket) for `br` /
  `harmonik comms` / `queue submit`, and makes at least one live model-API call (OpenRouter).
- `tmux attach` into a running sandboxed Pi session behaves normally.
- The same profile generator emits literal-path (Linux-bwrap-compatible) settings and the acceptance
  scenario passes on a Linux node.
