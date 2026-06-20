# Boot-digest scripts (captain / crew context economy)

These two scripts collapse the captain's and crew's deterministic boot discovery
into a single shell call that emits one Markdown STATE DIGEST. The LLM reads ONE
digest instead of running ~6–12 individual discovery turns, so it accrues far less
context before real orchestration begins (bead **hk-039z**, captain-economy CE1).

| script | replaces | who runs it |
|---|---|---|
| `captain-boot-digest.sh` | STARTUP.md Steps 2a–2g + Step 4 (daemon status, comms who, crew list, tmux fleet, paused queues, recent comms, ready beads, open epics, kerf next, kerf map) | a captain session, at boot AND on every keeper-restart resume |
| `crew-boot-digest.sh` | crew-launch SKILL.md Steps 1–2 (mission file, identity check, daemon status, comms who, my-queue status, epic state, ready beads, recent comms) | a crew session, at boot |

## Why they exist in git

Prior versions lived ONLY as out-of-git copies at `~/.claude/captain-tools/` on a
single machine (the false-closed TA2 boot-digest bead hk-n3w1). The skills referenced
the non-versioned `~/.claude/captain-tools/...` path, so on any other box the digest
silently did not exist and the captain fell back to the heavy raw-command boot. These
in-repo copies under `scripts/` are the authoritative, portable versions; the skills
reference `scripts/captain-boot-digest.sh` / `scripts/crew-boot-digest.sh`.

## What they do NOT do

They cover DISCOVERY only — pure deterministic reads. Every JUDGMENT step stays
LLM-driven and is explicitly excluded:

- captain: zombie classification, lane planning, fleet establishment, bead selection.
- crew: comms join, `br update --assignee` mirror, `recv --follow` arming, boot-status post.

## Usage

```bash
# Captain (run from anywhere; defaults HK_PROJECT to the harmonik repo):
scripts/captain-boot-digest.sh [--project DIR]

# Crew (auto-detects the crew name from $HARMONIK_AGENT):
scripts/crew-boot-digest.sh [--crew NAME] [--project DIR]
```

Both are read-only against live state (queue/comms/beads RPCs + tmux + kerf) and
exit 0 even when the daemon is down — the local reads (`crew list`, `comms who`,
`comms log`) still work, and the digest flags the daemon-down condition inline.

## Mandatory on the captain resume path

On a keeper-restart resume the captain runs `captain-boot-digest.sh` as the SINGLE
verification pass and TRUSTS the tier-2 / tier-3 cached context (mid epics / long
goals are stable) rather than re-deriving everything via the full heavy boot. Full
Step-2 re-derivation is only for a cold boot or a digest-flagged discrepancy. See
`.claude/skills/captain/STARTUP.md` Step 2 and the "On resume after a restart-now
cycle" block.
