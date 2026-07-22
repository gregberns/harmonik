# Codex + Remote Strategy Realignment — 2026-07-20

## Why this plan exists

The operator paused a multi-day effort that had the captain, admiral, and one crew
all churning on a single thread of work (unblocking Codex-as-a-crew for a prod
deploy). The operator's read: there is a **serious miscommunication** buried in
here, and possibly a **fundamentally wrong strategy** on both the sandbox question
and the remote-execution question. This plan re-establishes ground truth before any
more building.

## Operator's stated concerns (verbatim intent, paraphrased)

1. **The sandbox decision.** We have run *Claude* unsandboxed, right on the host,
   for months. The operator only ever wanted a sandbox for **Pi** (used to run
   *low-quality* models that might wipe the machine). At some point an agent decided
   **Codex** needed a sandbox — and there's a confusing local-vs-remote split in
   that requirement — and it was **never clearly communicated** to the operator.
   Codex reportedly already runs in a partial sandbox of its own.
   → Question: what is the ACTUAL requirement vs a SELF-IMPOSED constraint?

2. **The remote strategy.** It looks like we ship the whole project onto the remote
   machine and try to connect back when DOT nodes complete, and that has problems.
   The operator believes the Codex design should let us **start a Codex process on a
   remote machine, point at it, and stream a ton of work to it** — and that this
   should be easy. Why can't we?

3. **Local Codex.** After a long time, running Codex *locally* still isn't actually
   hooked up. Why? It should be easy.

## What the operator asked for

- Get COMPLETELY familiar first (fan out subagents; write research to disk; a
  consolidation pass checks it) — so the analysis lives in-context, not re-fetched.
- Then a **concise, technical** briefing (no-jargon mode; experienced-engineer
  audience). Break it into **logical units** and go **one at a time** — a
  conversation, not a book. Diagrams welcome.
- For each issue, separate **ASSUMPTIONS**, **ACTUAL limitations**, and
  **SELF-IMPOSED constraints**.
- Document the discussion + decisions here as we go.

## Layout

- `research/` — subagent findings, one file per investigation angle.
- `SYNTHESIS.md` — consolidation pass; the checked, deduped picture.
- `NOTES.md` — running discussion log with the operator.
- `DECISIONS.md` — decisions as we make them.

## Operating constraints for this session

- Timers/crons/keepalive monitors DISABLED (operator request, 2026-07-20).
- Do NOT hand off even if the keeper asks — the operator wants an uninterrupted run.
- Daemon + comms are currently DOWN; research relies on code, git, beads, kerf,
  plans/ — not live comms.
