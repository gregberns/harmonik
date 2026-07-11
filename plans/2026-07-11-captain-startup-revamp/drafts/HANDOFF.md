<!-- DRAFT — proposed replacement for /HANDOFF.md (repo root)
     (startup-doc revamp companion, per 02-cutover-and-open-questions.md §2.2[B] + Step 0.3 +
     00-SYNTHESIS.md §6 "HANDOFF.md body | CUT → 5-line tombstone pointing at HANDOFF-<agent>.md
     + the brief; add retention/home rule for the 27 root HANDOFF-*.md files"). Lands WITH the
     amended AGENTS.md at Step 4.1.

     Why: the generic root HANDOFF.md body was a dead 2026-06-21 snapshot no manifest agent
     reads at boot (`harmonik agent brief` embeds the PER-AGENT handoff — e.g. HANDOFF-captain.md
     — not this file); it was also already gitignored as a session artifact
     (15382e4f1 "chore: gitignore HANDOFF.md — session artifact was tracked, causing reverts to
     stale handoffs"). This draft is the still-tracked TOMBSTONE that replaces the body: it
     explains where the real handoffs live and stays in git even though the generic HANDOFF.md
     it once was is not. This DRAFT comment is removed on deploy; the tombstone body below is
     the whole live file (5 lines, per the synthesis target). -->

# HANDOFF.md — retired as a generic snapshot

Not a boot doc. Manifest agents get their handoff via `harmonik agent brief` (which embeds the
per-agent file below); the solo `/session-resume` path reads its own target directly.

**Home rule:** every agent's handoff lives at root `HANDOFF-<agent>.md` (e.g. `HANDOFF-captain.md`,
`HANDOFF-kynes.md`) — one file per agent identity, overwritten at each handoff, never this one.
Git tracking for a given `HANDOFF-<agent>.md` is that role's own call (some are `.gitignore`d
scratch, some are a durable storage slot a boot pipeline reads directly — check the file's own
header banner, e.g. `HANDOFF-captain.md`, before assuming either way).
