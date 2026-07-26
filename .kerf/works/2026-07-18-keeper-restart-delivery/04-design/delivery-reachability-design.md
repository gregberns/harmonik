# Cluster design — delivery-reachability (R-A)

Codename: `2026-07-18-keeper-restart-delivery` · pass 4 (change-design)

This is the cluster-level navigation + summary doc for research cluster **R-A**. It
faithfully summarizes the design decisions; the normative text and full rationale live in
the spec-named files pointed to at the bottom.

## Scope & components covered

R-A is the spine of the work. It covers:

- **K1 — delivery channel & reachability:** stop writing leader nudges into the operator's
  pane; deliver them over comms instead, gated by a presence-based reachability read.
- **K5 — situational read (best-effort):** sharpen the operator-present read where cheaply
  possible, and feed reachability/liveness into the K1 decision; explicitly declare the
  remote/mobile-blindness fix out of scope.
- **agent-input reachability substrate:** record the keeper as a new comms producer and a new
  presence consumer, without redesigning the bus.

## Key design decisions

**The collision is structural, not a tuning bug.** The warn nudge always fires the same
`tmux paste + settle + 3× Enter` once the pane quiesces, and the operator-attached guard only
swaps the *text*, never suppresses the paste. The operator-present read cannot be made
accurate from the keeper side — tmux exposes no keystroke-recency finer than the 5-min
`client_activity` window, and remote/mobile input bypasses tmux entirely. So the fix is not
"detect the operator better" — it is to stop writing into the operator's pane at all for
leader sessions.

**Delivery = a comms `agent_message`, sent by shelling `harmonik comms send`.** The keeper
holds no daemon handle and is depguard-barred from a library bus call, so it sends the same
way it drives tmux — a subprocess: `harmonik comms send --from keeper --to <agent> --topic
keeper -- <body>`. It MUST NOT use `--wake`, because `--wake` re-enters the exact pane-paste
primitive K1 exists to avoid; the non-collision property comes from the subscribe path (no
pane write), not from a wake.

**Reachability = presence-Online, read in-process.** The keeper is depguard-allowed to import
`internal/presence`. A live `comms recv --follow` reader emits a presence refresh beat every
60s, so presence-Online (age < TTL 120s) is the existing signal that tracks a live follower.
The keeper reads it directly on the warn tick.

**Delivery is a total, deterministic function — no silent no-op.** A leader that is Online
gets the comms path (no pane write); a leader that is Stale/Offline routes to the terminal
fallback = the *existing* warn path, preserving the operator-attached-gated text and the
750ms-settle + retry-Enter loop in full (the retry loop is a load-bearing reliability fix and
must not be deleted). Crew delivery is unchanged this work.

**Stated limitation (not hidden).** Presence-Online is necessary but not sufficient: a bare
`comms join` also shows Online with no reader attached, and a follower's 60s beat can hold
presence Online for a moment after a `/clear`. So the comms path can occasionally deliver to
an unread inbox. This is non-catastrophic — the unchanged FORCE-ACT / hard-ceiling ladder
remains the backstop. Presence-Online is the v1 reachability definition; a sharper
"recv-follow-armed" signal is named as a future enhancement, not a v1 requirement.

**K5 is best-effort and honest about scope.** It adds an in-cycle operator-attached TOCTOU
re-check (today operator-attached is sampled once at cycle entry and never re-checked across
the up-to-300s wait), and it feeds the reachability pre-check into the K1 decision so a dead
inbox routes to terminal fallback. It does NOT close remote/mobile blindness — a hook-bridge
keystroke signal is named as an external dependency, out of scope here. K1's comms path makes
its absence non-fatal.

**agent-input substrate is thin.** The keeper is a new producer (`--from keeper`, `--topic
keeper`) and a new consumer of presence; no bus redesign, no new port/event class, no change
to at-least-once delivery or `event_id` dedupe.

## Requirement IDs this cluster produces

- session-keeper.md: **SK-022, SK-023, SK-024, SK-025** (K1 §4.11); **SK-035, SK-036, SK-037**
  (K5 §4.15); invariant **SK-INV-006** (delivery totality, §5).
- agent-input.md: **AIS-019, AIS-020** (K1 reachability substrate, §4.10).

## Normative text and full rationale

- Design detail: `04-design/session-keeper-design.md` (K1, K5 sections) and
  `04-design/agent-input-design.md`.
- Normative drafts: `05-spec-drafts/session-keeper-amendment.md` (SK-022…SK-025, SK-035…SK-037,
  SK-INV-006) and `05-spec-drafts/agent-input-amendment.md` (AIS-019, AIS-020).
- Cluster spec-draft index: `05-spec-drafts/delivery-reachability.md`.
- Grounding research (all file:line citations): `03-research/delivery-reachability/findings.md`.
