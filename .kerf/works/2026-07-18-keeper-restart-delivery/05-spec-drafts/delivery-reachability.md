# Cluster spec-draft index — delivery-reachability (R-A)

Codename: `2026-07-18-keeper-restart-delivery` · pass 5 (spec-draft)

Index of every normative requirement cluster R-A adds (K1 delivery+reachability, K5
situational read, agent-input substrate). One-line summaries below; the full normative text
lives in the spec-named amendment files pointed to per row.

## session-keeper.md → `05-spec-drafts/session-keeper-amendment.md` (v0.2.0 → v0.3.0)

### K1 — Delivery channel & reachability (new §4.11)

- **SK-022** — Leader-session nudge is delivered as a durable comms `agent_message` (shell
  `harmonik comms send --from keeper --to <agent> --topic keeper`), never a terminal paste, and
  never via `--wake`; no `PanePort.Inject` on the comms path.
- **SK-023** — Reachability check is presence-Online (age < TTL 120s), read in-process via
  `presence.ComputePresenceRegistry` + `GetPresenceState` on the warn tick; stated
  necessary-but-not-sufficient limitation.
- **SK-024** — Delivery channel is a total, deterministic function of role × reachability with
  no silent no-op: Online leader → comms; Stale/Offline leader → terminal fallback; crew
  unchanged.
- **SK-025** — Terminal fallback reuses the existing `injectTextClocked` warn path unchanged,
  preserving the operator-attached-gated text and the 750ms-settle + retry-Enter loop (NG3).

### K5 — Situational-read sharpening (new §4.15)

- **SK-035** — Operator-attached is re-sampled during the handoff wait (in-cycle TOCTOU), not
  only once at cycle entry.
- **SK-036** — The SK-023 reachability/liveness pre-check feeds the delivery decision so a
  Stale/Offline target routes to terminal fallback, never a comms send into a dead inbox.
- **SK-037** — A hook-bridge keystroke "operator-actively-here" signal is named as an external
  dependency on claude-hook-bridge.md, OUT OF SCOPE here; SK-022's comms path makes its absence
  non-fatal.

### New invariant (§5)

- **SK-INV-006** — Leader nudge delivery is total: every fired leader warn tick resolves to
  exactly one of {comms `agent_message`, terminal fallback}, never a silent no-op.

## agent-input.md → `05-spec-drafts/agent-input-amendment.md` (v0.1.0 → v0.2.0)

### K1 reachability substrate (new §4.10)

- **AIS-019** — The keeper is a recognized comms producer (`--from keeper`, `--topic keeper`),
  sending fire-and-forget durable `agent_message`s; no join, no subscription, no new transport.
- **AIS-020** — Presence-Online (age < 120s) is the reachability read a comms producer may rely
  on, documented with the necessary-but-not-sufficient limitation and the future
  recv-follow-armed signal.

## Notes

- ZERO threshold changes (NG1/SC-9): SK-016, the bands, and the gate ladder are untouched.
- No prior IDs renumbered or retired; these are sequential appends.
- Full normative prose, axes/tags, co-references, and revision-history entries: see the two
  spec-named amendment files above.
