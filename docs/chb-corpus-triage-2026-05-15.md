# CHB Corpus Dispatch Triage — 2026-05-15

Epic: `hk-qo08q` — *Land claude-hook-bridge spec corpus changes* (P2, OPEN).
Subgraph size: 43 beads (31 open, 12 closed). Source: `bv --robot-triage --graph-root hk-qo08q`.

---

## 1. Plain-English summary

The **claude-hook-bridge (CHB)** corpus wires the real `claude` binary into harmonik's
review-loop by way of Claude Code's hook system: harmonik materializes a workspace
`.claude/settings.json` whose five hook entries shell out to a small
`harmonik hook-relay` subcommand. The relay reads a hook payload on stdin, opens a
one-shot NDJSON connection to the daemon socket, and forwards a single progress event;
the handler process runs alongside Claude and emits the long-lived stream
(`handler_capabilities`, heartbeats at T/2, terminal `agent_completed` / `agent_failed`).
This gives the daemon a single observation surface that is **twin-blind** — the
`harmonik-twin-claude` binary emits the same wire format so daemon code never branches
on real-vs-twin. Closure criterion: spec corpus published, six Phase-1 impl beads green,
and the G1 acceptance test (`hk-7uasg` real-Claude review-loop) passing.

---

## 2. Ranked dispatch list (open children only)

Note: corpus priorities are P1 (1 bead) and P2 (rest). "Rank tier" below is the
recommended dispatch order based on graph centrality, blocker fan-out, and acceptance-gate
proximity — not the recorded P-level.

### Tier A — dispatch first (critical-path bottlenecks / unblock most work)

| # | ID | Title | Why this rank |
|---|----|-------|---------------|
| 1 | `hk-q7atz` | Daemon socket acceptor + CHB-023 durable-checkpoint write | bv top pick: 100% betweenness, 90% PageRank, unblocks both `hk-pcgms` and indirectly `hk-7uasg` G1 test; req-children CHB-015 and CHB-023 already CLOSED so the impl can land directly |
| 2 | `hk-02sp0` | Workspace `.claude/settings.json` materialization (WM-040a) | All five req-children (CHB-001..005) already CLOSED in `fb1bb8c`; bead is a tracking placeholder — likely sweep-close, verify and either close or land remaining glue |
| 3 | `hk-crf9a` | Handler launch-prep: env, forbidden-flags, claude_session_id mint/resume | Unblocks G1; req-children CHB-006/007/008 still OPEN — has spec-write component (see §4); cluster size moderate |

### Tier B — independent impl tracks, dispatch in parallel with Tier A

| # | ID | Title | Why this rank |
|---|----|-------|---------------|
| 4 | `hk-lj848` | Implement `harmonik hook-relay` subcommand in `cmd/harmonik/` | Largest fan-in (8 CHB req-children: CHB-010..017); only CHB-015 closed, others still OPEN. Self-contained subsystem (`cmd/harmonik/`); no cross-coupling with daemon work above |
| 5 | `hk-pcvw8` | Handler Wait-window: pre-exec emissions, heartbeat, terminal events | Covers CHB-018/019/020; isolated to `internal/handler/`; independent of relay and daemon-socket work |
| 6 | `hk-s2vpx` | Twin emits identical wire-format sequence (CHB-021) | Touches only `twins/harmonik-twin-claude`; needs CHB-021 + CHB-022 spec text first; independent of all impl paths above |

### Tier C — acceptance gates (run after impl beads land)

| # | ID | Title | Why this rank |
|---|----|-------|---------------|
| 7 | `hk-gerqr` | CHB-INV-001 sensor: two-contributor session | Blocks G1 (`hk-7uasg`); pure test, runs against landed impl |
| 8 | `hk-qo96c` | CHB-INV-002 sensor: single terminal event | Blocks G1; sibling sensor |
| 9 | `hk-xlach` | CHB-INV-003 sensor: mechanism-no-cognition | Blocks G1; grep-only sensor, trivial |
| 10 | `hk-cw56j` | Implementer `--resume` correctness across daemon restart | Verifies CHB-023 already-closed durable-checkpoint; needs `hk-q7atz` live |
| 11 | `hk-pcgms` | Relay-failure scenario: daemon socket missing → `bridge_dial_failed` | Needs `hk-q7atz` live; depends on relay (`hk-lj848`) for `bridge_dial_failed` emission |
| 12 | `hk-7uasg` | **Real-Claude end-to-end review-loop integration test (P1)** | G1 acceptance gate for the whole epic — blocked on Tier C sensors |

### Tier D — spec-text req-beads still OPEN (need to land before their impl bead)

These are individual requirement beads in the spec; their impl umbrella beads above
already reference them. They represent the spec-text-not-yet-codified-in-code work that
must precede or accompany the impl bead.

| ID | Req | Umbrella impl bead |
|----|-----|--------------------|
| `hk-qo08q.6` | CHB-006 env-var schema | `hk-crf9a` |
| `hk-qo08q.7` | CHB-007 forbidden flags | `hk-crf9a` |
| `hk-qo08q.8` | CHB-008 session_id mint | `hk-crf9a` |
| `hk-qo08q.9` | CHB-009 reviewer fresh-mint | (orphan — no umbrella) |
| `hk-qo08q.10..14` | CHB-010..014 relay surface | `hk-lj848` |
| `hk-qo08q.16, .17` | CHB-016, CHB-017 relay retry / exit | `hk-lj848` |
| `hk-qo08q.18..20` | CHB-018..020 handler Wait-window | `hk-pcvw8` |
| `hk-qo08q.21, .22` | CHB-021, CHB-022 twin / daemon-blind | `hk-s2vpx` |
| `hk-qo08q.24` | CHB-024 settings-precedence verification | (orphan — no umbrella) |

Two orphan req-beads (`hk-qo08q.9`, `hk-qo08q.24`) have no umbrella impl bead — see §5.

---

## 3. Parallel tracks (non-conflicting concurrent dispatch)

Three tracks can run truly in parallel; each touches a disjoint code area.

- **Track 1 — Daemon socket** (`internal/daemon/`): `hk-q7atz`
- **Track 2 — Relay subcommand** (`cmd/harmonik/`): `hk-lj848` + req-beads `.10..14, .16, .17`
- **Track 3 — Handler surface** (`internal/handler/`): `hk-crf9a` + `hk-pcvw8` (sequential within track — both edit handler package; can split by file boundary if needed)
- **Track 4 — Twin** (`twins/harmonik-twin-claude`): `hk-s2vpx` + req-beads `.21, .22`
- **Track 5 — Workspace sweep**: `hk-02sp0` — likely a single-pass close-out

Sensors (Tier C) join after Tracks 1-3 land. The docs-amendment bead `hk-ocisx` can run on
any track at any time (read-only edits to `docs/subsystems/`).

---

## 4. Spec-write vs. implementation split

**Per the spec-text-check-in constraint** (user-locked): any new normative spec language
requires user check-in before commit. Pure code/test work does not.

### Needs spec-text check-in (write spec section first, confirm, then implement)

None of the open beads in this corpus require **new** normative spec text. The spec
corpus (`specs/claude-hook-bridge.md`) and the four cross-spec amendments (HC/WM/PL/EM)
have already been finalized — closed amendment beads `hk-4woeq`, `hk-rirxa`, `hk-2ubs8`,
`hk-63k6b`, `hk-u5c5i` all confirm "spec text already landed in 6bc2e57". The open
`hk-qo08q.N` req-beads describe requirements that **already exist** in
`specs/claude-hook-bridge.md`; they are tracking placeholders for impl verification, not
new spec authorship.

### Pure code/test (no check-in required, dispatch freely)

All Tier A/B/C beads above. The "umbrella" impl beads (`hk-q7atz`, `hk-02sp0`,
`hk-crf9a`, `hk-pcvw8`, `hk-lj848`, `hk-s2vpx`) and all sensors/scenario tests are
read-the-spec → write-Go work.

### Caveat — informational docs

`hk-ocisx` (docs amendment for `agent-runner.md` / `hook-system.md` / `AGENT_INDEX.md`)
is informational, not normative. Per project convention, `docs/` is not normative; no
check-in required, but a reviewer sub-agent should confirm the docs match the spec.

---

## 5. Blockers (missing infra, unmade decisions, orphans)

### Hard blockers — none

No bead in the corpus is gated on missing infra. All four cross-spec amendments are
CLOSED, the spec is finalized, and three CHB req-beads with sub-bead closures
(CHB-001..005, CHB-015, CHB-023) have already landed code.

### Soft items worth surfacing before dispatch

1. **Orphan req-bead `hk-qo08q.9` (CHB-009 reviewer fresh-mint)** has no umbrella impl
   bead. Belongs naturally in `hk-crf9a`'s scope (handler launch-prep) but is not listed
   as a child req. Decision needed: extend `hk-crf9a` scope, or spawn a tiny impl bead.
2. **Orphan req-bead `hk-qo08q.24` (CHB-024 settings-precedence verification)** has no
   umbrella impl bead. Belongs naturally in `hk-02sp0` (workspace materialization) or as
   a daemon-startup check. Decision needed: pick the host bead.
3. **Tracking-bead sweep**: `hk-02sp0`, `hk-ocisx`, and possibly `hk-q7atz` may be
   subsume-closeable already given closed sub-children — recommend a dedicated sweep
   pass running `br show` on each before opening fresh work. (Read-only here; not
   acted on.)
4. **G1 (`hk-7uasg`) is P1** — the only P1 in the corpus, and the closure criterion for
   the whole epic. Treat it as the goal; everything else exists to make it green.

---

## Translations glossary

- **CHB** = claude-hook-bridge (the spec / corpus)
- **G1** = `hk-7uasg`, the real-Claude end-to-end review-loop integration test —
  acceptance gate for the epic
- **`hk-qo08q`** = parent epic (P2)
- **`hk-qo08q.N`** = req-bead N for spec section N (sub-children)
- **HC/WM/PL/EM** = handler-contract / workspace-model / process-lifecycle /
  execution-model specs (the four amended)
- **Twin-blind** = daemon code contains no `if isTwin` / `if relay` branches;
  twin and real-Claude emit identical NDJSON wire format
- **One-shot NDJSON** = relay dials daemon socket, writes one line, reads one ack,
  closes (CHB-015)
- **T/2 heartbeat** = handler emits `agent_heartbeat` at 300s cadence (CHB-019)
