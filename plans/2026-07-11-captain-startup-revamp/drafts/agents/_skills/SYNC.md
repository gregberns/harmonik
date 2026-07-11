<!-- DRAFT — proposed NEW file (no live predecessor). Intended home: .harmonik/agents/_skills/SYNC.md.
     Part of plans/2026-07-11-captain-startup-revamp (Stage-0 step 3 + Stage-5 step 1).
     Do not deploy from here; this is a design note + script spec for review. -->

# \_skills/ SYNC — one skill tree, mechanically enforced

Design note + script spec for `scripts/agents-skills-sync.sh`. Kills the two-skill-tree
drift (SYNTHESIS §2.4) and gives the Stage-5 rot gate a checkable definition of done
(01-revamp-process.md Stage 5.1).

## Principles

Everything below is a consequence of four principles. When a case comes up that the spec
doesn't cover, reason from these — don't invent a new rule.

1. **One authoritative copy.** Every skill has exactly one place where it is authored.
   Anything else that carries its text is a *generated artifact* of that source. A second
   hand-maintained copy is not a convenience — it is a fork that WILL diverge, and because
   bare manifest refs resolve `_skills/` first (`ResolveRef`,
   `internal/agentmanifest/manifest.go`; resolution rule SPEC §6), a stale mirror silently
   *shadows* the real skill for every booting agent. That is why drift is a FAIL, never a
   warning.
2. **Enforce mechanically; don't add rules.** The anti-rot rules already exist
   (context/CLAUDE.md §"Forced READ + freshness" gives the admiral audit ownership of
   striking expired state; §"Retention" caps the direction-log; the state files declare
   their own contracts). What's missing is anything that *checks* them — the rot is an
   enforcement gap, not a rule gap (SYNTHESIS §3). This script is the mechanical arm of
   rules that already exist; every check below cites the rule it enforces.
3. **Derive expectations from reality, not from a snapshot.** An anti-drift tool with a
   hardcoded worldview drifts itself. The mirror set is computed from
   `.harmonik/agents/*/manifest.yaml` at runtime; hardcoded lists in this doc are
   illustrations of today's state, not the contract.
4. **Fail fast and loud; git is the archive.** A failed check stops the commit with
   file:line evidence — no auto-fix, no silent overwrite, no warning-and-continue. A
   hand-edit found in a mirror halts `--apply` rather than clobbering it. History lives in
   git, never in prose `SUPERSEDED` blocks (that write discipline — replace-in-place
   everywhere — is what §3 enforces).

## 1. Canonicity

- **`.claude/skills/` is CANONICAL** for every skill that exists in both trees.
- **`.harmonik/agents/_skills/` is a GENERATED MIRROR.** Never hand-edit it. Edit the
  `.claude/skills/` source, run the sync, commit both.
- **Exception — `_skills/boot/`** is native to the manifest tree (no `.claude/skills/boot`
  exists; it only makes sense for manifest agents). It is authored in place and exempt from
  mirroring, but the check verifies it exists and is non-stub (≥10 lines).
- Same pattern already proven for `cmd/harmonik/assets/skills/` (see
  `cmd/harmonik/init_skill_assets.go` L12–15 + `init_skills_sync_test.go`): canonical source
  in `.claude/skills/`, byte-identical copy, drift = test failure.

**Mirror set is derived at runtime** (principle 3): collect every bare ref (no `/`) from
`.harmonik/agents/*/manifest.yaml` context lists; each must be either a mirrored pair or a
declared native (`boot`). Today that derivation yields:

| \_skills entry | Source | Mode |
|---|---|---|
| `agent-comms/SKILL.md` | `.claude/skills/agent-comms/SKILL.md` | mirrored |
| `beads-cli/SKILL.md` | `.claude/skills/beads-cli/SKILL.md` | mirrored |
| `harmonik-dispatch/SKILL.md` | `.claude/skills/harmonik-dispatch/SKILL.md` | mirrored |
| `crew-launch/SKILL.md` | `.claude/skills/crew-launch/SKILL.md` | mirrored |
| `boot/SKILL.md` | authored in place | existence + non-stub check only |

This table is a snapshot for the reviewer, not the contract — the script never reads it.

## 2. Script spec — `scripts/agents-skills-sync.sh`

One script, two verbs. No args = check.

```
scripts/agents-skills-sync.sh            # check: completeness + diff each mirrored pair; exit 1 on ANY drift
scripts/agents-skills-sync.sh --apply    # copy .claude/skills → _skills for the mirror set
scripts/agents-skills-sync.sh --rot      # state-rot checks only (§3)
scripts/agents-skills-sync.sh --all      # skill-drift + rot checks (audit mode)
```

Behavior:

- **check** (default), three parts, all FAIL loud with paths:
  1. **Completeness** (principle 3): parse the bare refs out of
     `.harmonik/agents/*/manifest.yaml` at runtime. FAIL on (a) a manifest bare ref with no
     corresponding `_skills/<ref>/` entry, and (b) a `_skills/` subdir that no manifest
     references (dead weight or a typo'd name — either way a shadowing hazard). Without
     this, a future bare ref (e.g. `keeper`) drifts invisibly — a blind spot in an
     anti-drift tool.
  2. **Pair drift**: `diff -q` each mirrored pair.
  3. **Native check**: `_skills/boot/SKILL.md` exists, ≥10 lines.
  Print each failure + a `--apply` hint. Exit 0 clean / 1 drift / 2 usage.
- **--apply**: `cp` source → mirror for the mirror set (never the reverse; never touches
  `boot/`). **Hand-edit guard (principle 4), pure-git blob comparison — no mtime:** a
  mirror is safe to overwrite iff its current content matches some committed blob of its
  canonical source (it is provably just a stale copy). Concretely: compare
  `git hash-object <mirror-file>` against the source's blob hashes across
  `git log --format=%H -- <source-path>` (and the working-tree source). No match ⇒ the
  mirror carries text that never existed in the source ⇒ print "hand-edit detected in
  mirror; reconcile into .claude/skills first" and exit 1. (mtime is worthless here — a
  checkout or clone rewrites every timestamp.)
- Paths hardcoded relative to repo root (`git rev-parse --show-toplevel`); no env vars.

Callers (all three, same script):

1. **Standalone** — any agent or the operator, any time.
2. **lefthook** — `pre-commit` command, gated on staged paths matching
   `{.claude/skills/**,.harmonik/agents/_skills/**,.harmonik/agents/*/manifest.yaml}`
   (mirrors the existing check-fast/agent-review pattern in `lefthook.yml`; manifests are
   in the gate because they define the mirror set). Check mode only — the commit fails,
   the human/agent runs `--apply` and re-stages.
3. **agent-config-reviewer + admiral audit** — run `--all` as part of the skill-registry
   drift check; any exit-1 maps to `DRIFT_MAJOR` with flag `skill-tree-drift` (or
   `state-rot` for §3 failures). context/CLAUDE.md already assigns the admiral audit
   ownership of striking expired state; this script is that duty's mechanical arm
   (principle 2).

### Immediate reconciliations (first `--apply`, do these before wiring the check in)

1. **agent-comms**: the presence-refresh ≤90s CRITICAL block is ALREADY in the canonical
   `.claude/skills/agent-comms/SKILL.md` (L223–227 + L306 bullet, verified 2026-07-10 — the
   SYNTHESIS §4 "exists ONLY in manifest Bounds" backfill has since landed). The `_skills`
   mirror is 6 lines behind and is MISSING that block. First `--apply` closes it. Verify the
   block is present in the source before syncing — it is the one single-copy rule loss the
   synthesis flags (§7).
2. **crew-launch**: `_skills/crew-launch/SKILL.md` is a **1-line stub** while
   crew/manifest.yaml L12 injects it as the crew's authoritative skill (empty authority).
   Interim fix: `--apply` copies the current 609-line `.claude/skills/crew-launch/SKILL.md`
   verbatim — verbose but correct beats empty. When Stage 3 lands the ~150-line retrieved
   reference in `.claude/skills/`, the mirror picks it up on the next sync automatically.

## 3. State-rot check (`--rot`; admiral audit owns it)

Principle 2 applied to operational state: the rules below all pre-exist (context/CLAUDE.md
§"Forced READ + freshness" + §"Retention", the Stage-1 header contracts, the manifest-boot
model); each check is their enforcement, not a new rule. Each FAILs (exit 1) with file:line
evidence:

| # | Check | Mechanics |
|---|---|---|
| R1 | captain-lanes.md over cap | line count > declared cap (Stage-1 header contract: ≤60; read `max-lines:` from the file header, default 60) |
| R2 | direction-log.md over cap | >60 lines or >10 `## ` entries (context/CLAUDE.md §Retention) |
| R3 | past `expires:` anywhere in `.harmonik/context/*.md` + lanes.json | parse RFC3339; timestamp < now = FAIL. Date-only value = FAIL too (context/CLAUDE.md: date-only parses as expired) |
| R4 | >1 `CURRENT TRUTH` heading in captain-lanes.md | `grep -c 'CURRENT TRUTH'` > 1 (motivating rot: 19 stacked blocks at the 2026-07-11 audit; the live file has since been compacted to one — R4 holds that line) |
| R5 | `SUPERSEDED` in any `.harmonik/crew/missions/*.md` (templates `_TEMPLATE-*` excluded) | grep; missions are overwrite-only — git is the archive (today: kynes.md ×5) |
| R6 | captain-lanes vs admiral-initiatives flagship disagreement | both files carry an `updated:` stamp and a one-token `flagship:` status line; FAIL if the tokens differ or either stamp/line is missing. Dependency, verified 2026-07-11: the `updated:` stamp is in the Stage-1 captain-lanes draft, but the `flagship:` line exists in **NO draft yet** — it is a required companion edit at cutover Step 2.3 (add to both files' header contracts); R6 stays red until that lands. (The 01:54Z "REDEPLOY HELD" vs 04:26Z "flagship DONE, 59089968" contradiction is the motivating case) |
| R7 | routers re-teaching the old boot | grep AGENTS.md + AGENT_INDEX.md, case-insensitive, for **ANY `STARTUP\.md` mention** — not just "STARTUP.md FIRST"; the live router carries both a backtick-quoted ``read `.claude/skills/captain/STARTUP.md` FIRST`` and a bare parenthetical `(see .claude/skills/captain/STARTUP.md)`, and a narrower regex misses its own motivating case — plus the reading-order ritual (`AGENT_INDEX(\.md)?\s*(→\|->)\s*STATUS`, `reading order`); any hit = FAIL. **One exemption** (principle 2 — the check enforces "don't re-teach the old boot", and a negation IS the new teaching): skip a line matching `(?i)do not read[^.]*STARTUP\.md` — the amended AGENTS.md draft carries exactly one such sanctioned line ("Do not read STARTUP.md or any doc chain"), and without the exemption R7 can never go green post-cutover. The tombstone file itself is not scanned |

Sequencing notes:

- Several checks FAIL against the live tree **by design** — they gate a cleanup stage and
  then hold the line. As of this draft: **R2, R5** fail on live state (direction-log at
  138 lines / 8 entries; kynes.md `SUPERSEDED` ×5) and clear at GATE 1; **R6** clears only
  when the `flagship:` header line lands in both files (cutover Step 2.3 companion edit —
  see the R6 row); **R1 and R4 already pass** (the live captain-lanes was compacted to one
  block / 52 lines on 2026-07-11); **R7** fails on the live routers and clears only at
  Stage 4 (the router + tombstone landing, with the negation exemption), NOT GATE 1. Do not
  wire `--rot` into lefthook until every check is green; audit-only until then.
- Write discipline this enforces (Stage 5.2): replace-in-place everywhere; prose
  `SUPERSEDED` markers banned; git is the archive.

## 4. Beads to file (code, NOT doc drafts — dispatch via normal queue)

Renderer work is daemon-binary work: pre-deploy e2e gate + `docs/daemon-redeploy.md` apply.
Docs must not depend on these landing (until then, operating.md carries explicit paths for
its retrieved docs).

1. **Brief-renderer bead A — ref rendering:** (a) parse frontmatter `description:` for
   injected-skill short-descs (today the first line of a frontmatter'd skill renders as
   `**agent-comms:** ---`); (b) render `as: doc, presence: retrieved` refs WITH their paths
   (today captain/manifest.yaml L15–16's orchestrator-rules + orchestration-protocol-v2.md
   are silently dropped — booting agents cannot find their declared standing-rules doc).
2. **Brief-renderer bead B — handoff framing:** stamp the brief's Handoff section header
   "CLAIM, not ground truth — `harmonik digest` overrides" (the embedded captain handoff
   still claims the reaper P0 unfixed though 95701ee9 shipped it).
3. **Shared reviewer-renderer bead:** extract ONE `renderReviewerConstraint()` used by all
   three injection points — `internal/workspace/agenttask_chb028.go` L356–363 (agent-task.md
   builder) and L570–576 (review-target.md builder) still teach the BANNED
   hand-write-review.json-via-`mv`; `internal/daemon/pasteinject.go` L1846–1853 (seed,
   hk-9w79a) already mandates `harmonik write-review-verdict` and is newest → wins. All
   three must mandate `write-review-verdict`. Acceptance = GATE 4: canary bead through
   implement→review→merge, verdict lands via write-review-verdict, no bead-issued terminal
   `br` transitions in events.
