---
name: beads-cli
description: >
  Agent-facing wrapper for `br` (Beads CLI), the task ledger for harmonik.
  Declares the read surface agents may use (br show, br list, br ready, br dep
  cycles) and the write discipline they must follow (agents MUST NOT issue
  terminal-transition writes; the daemon owns those per beads-integration.md §4.4).
  Required in every agent's launch context per BI-028 and CP-031.

  Load-bearing: must not rot. Kept current with br v0.1.x and beads-integration.md.

sources:
  - specs/beads-integration.md §4.9 (BI-027, BI-028)
  - specs/handler-contract.md §4.11 (HC-046–HC-049)
  - specs/control-points.md §4.6 (CP-031, CP-052)
---

# Beads-CLI Skill

You are operating inside a harmonik run. Beads is harmonik's task ledger (SQLite +
JSONL, accessed via the `br` CLI). This skill defines the `br` surface available to
you and the write discipline you must follow.

---

## Write discipline (READ THIS FIRST)

**Agents MUST NOT issue terminal-transition `br` writes.**

Terminal transitions — `claim` (open → in_progress), `close` (in_progress → closed),
and `reopen` (closed → open) — are owned exclusively by the harmonik daemon per
[beads-integration.md §4.4 BI-010]. Bypassing the daemon's adapter violates the
idempotency and intent-log contracts of §4.10.

Agent-permissible writes are limited to:

| Operation | Command | Notes |
|---|---|---|
| Add a comment | `br comments add <bead_id> --body "..."` | Progress notes, observations |
| Add a label | `br update <bead_id> --add-label <label>` | Non-status metadata only |
| Remove a label | `br update <bead_id> --remove-label <label>` | Non-status metadata only |
| Update description/notes | `br update <bead_id> --notes "..."` | Clarifications, findings |

Agents MUST NOT call `br update --claim`, `br close`, `br reopen`, or any command
that transitions a bead's `status` field. Those paths are the daemon's exclusive domain.

---

## Read surface

### Check available work

```bash
# List beads ready to claim (open, unblocked, not deferred)
br ready --format json -l scope:bootstrap

# Filter by label (AND logic)
br ready --format json -l scope:bootstrap -l kind:scaffold

# Limit results
br ready --format json --limit 10
```

`br ready` returns beads whose dependencies are all satisfied and whose status is
`open`. It natively excludes `draft`-status beads (harmonik's readiness gate for
loaded-but-not-yet-dispatchable work).

### Inspect a bead

```bash
# Full detail (use this; text-output parsing is forbidden per BI-025b)
br show <bead_id> --format json

# Multiple beads at once
br show <bead_id1> <bead_id2> --format json
```

Always use `--format json`. Parsing text output from `br` is forbidden (BI-025b).

### List and search beads

```bash
# All open bootstrap beads
br list --format json -l scope:bootstrap -s open

# All in-progress beads (what is currently running)
br list --format json -s in_progress

# Children of an epic
br list --format json --parent <epic_id>

# Search by text
br search --format json "keyword"

# Count by status. Note: br count returns scalar text and does not support
# --format json — this is one of the rare BI-025b exceptions; do not pipe to jq.
br count --by status
```

### Dependency queries

```bash
# Cycle check (should always be clean)
br dep cycles

# What does a bead depend on?
br dep list <bead_id>

# Dependency tree (recursive)
br dep tree <bead_id>
```

---

## Idiomatic jq pipelines

Extract the description of a bead:

```bash
br show <bead_id> --format json | jq -r '.[0].description'
```

List IDs of ready bootstrap beads:

```bash
br ready --format json -l scope:bootstrap | jq -r '.[].id'
```

Check a bead's status:

```bash
br show <bead_id> --format json | jq -r '.[0].status'
```

Get all labels on a bead:

```bash
br show <bead_id> --format json | jq -r '.[0].labels[]'
```

List IDs of blocked beads:

```bash
br blocked --format json | jq -r '.[].id'
```

---

## Output formats

Always pass `--format json` to every `br` invocation that produces structured output.
Text output parsing is explicitly forbidden by BI-025b. The TOON format is an
alternative token-optimized notation — use json for pipelines, toon is optional for
human-readable inspection only.

`br schema` emits JSON Schema definitions for all output types if you need to
understand the shape of a response:

```bash
br schema issue         # Core Issue object
br schema issue-details # Show view: Issue + relations/comments/events
br schema ready-issue   # Ready list row
```

---

## Status vocabulary

Beads exposes a read surface with 8+ status values. The ones you will encounter:

| Status | Meaning |
|---|---|
| `open` | Ready to work; dispatchable via `br ready` |
| `draft` | Loaded but not yet dispatchable (harmonik's readiness gate) |
| `in_progress` | Claimed and actively running |
| `blocked` | Has unsatisfied dependencies |
| `deferred` | Scheduled for later; excluded from `br ready` by default |
| `closed` | Completed |
| `tombstone` | Deleted |
| `pinned` | Pinned by Beads; pass through, do not interpret |

Agents read all status values; agents write none of them (see Write discipline above).

---

## Subprocess timeout discipline

Per BI-025c, the daemon's adapter enforces:

- **5 s** for read commands (default; operator-tunable)
- **10 s** for write commands (default; operator-tunable)

When you invoke `br` directly from a shell tool, respect the same guidance: do not
allow `br` invocations to hang indefinitely. If a `br` invocation times out, report
it rather than retrying in a loop.

---

## Version

The pinned `br` version for this harmonik release is declared in the harmonik release
manifest. Run `br version` to confirm compatibility:

```bash
br version
# Expected: br version 0.1.x (release)
```

A version mismatch causes daemon startup to fail with exit code 8
(`beads-unavailable`) per BI-024a.

---

## What agents should NOT do

- Do NOT call `br update --claim`, `br close`, or `br reopen` — daemon-owned.
- Do NOT parse text output from `br` — always use `--format json`.
- Do NOT write additional Beads status values beyond the five-value write subset
  `{open, in_progress, closed, deferred, tombstone}` — harmonik MUST NOT extend
  Beads's status enum via writes (BI-007).
- Do NOT mint, parse, or rewrite bead IDs — they are project-scoped opaque strings
  owned by Beads (BI-008a).
- Do NOT use `br` to track intra-run node transitions — those live in the git
  checkpoint trail and JSONL event log, not Beads (BI-007, BI-011).

---

## Sources

- `specs/beads-integration.md §4.9` — BI-027 (skill as only agent access path),
  BI-028 (skill in every agent's launch context by default)
- `specs/beads-integration.md §4.4` — terminal-transition write ownership (daemon only)
- `specs/beads-integration.md §4.8a` — BI-025b (--format json mandatory),
  BI-025c (timeout discipline)
- `specs/handler-contract.md §4.11` — HC-046–HC-049 (skill provisioning obligations)
- `specs/control-points.md §4.6` — CP-031/CP-052 (Beads-CLI in every MVH-required
  role's default_skills)
