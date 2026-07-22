# Amendment to specs/session-keeper.md (v0.2.0 → v0.3.0)

## Frontmatter

- `version: 0.2.0` → `version: 0.3.0`
- `last-updated: 2026-07-14` → `last-updated: 2026-07-18`

## New requirements

The highest occupied requirement ID is SK-021 (§4.10) and the highest invariant is
SK-INV-005 (§5). The new requirements introduced by this kerf
(`2026-07-18-keeper-restart-delivery`, K1–K5) are appended as SK-022…SK-037 in five
new subsections §4.11–§4.15 after §4.10, and one new invariant SK-INV-006 in §5, after
the highest occupied IDs — matching the sequential append pattern used for SK-001…SK-021.
No prior IDs are renumbered or retired; SK-002's PL-021d keeper carve-out, SK-014's
model-done gate, SK-016's band-preservation clause, and SK-INV-001 are unchanged and are
cited, not modified.

**Non-change (NG1 / SC-9), stated normatively.** This amendment changes ZERO threshold
values. The warn / act / force-act / hard-ceiling / window constants preserved by SK-016
and owned by [operator-nfr.md §4.13 ON-059] are untouched. Every requirement below either
adds a delivery channel, a message template, a config surface, or a situational read; none
alters a band, a gate order, or a threshold. SK-016 remains in force verbatim.

---

### Add new §4.11 — Delivery channel & reachability (K1). Add after §4.10:

#### SK-022 — Leader-session nudge is delivered over comms, not a terminal paste

For a leader session (captain or admiral), the keeper MUST deliver the warn/restart nudge
as a durable comms `agent_message` — NOT as a `PanePort.Inject` terminal paste — whenever
the target is reachable per SK-023. The keeper holds no daemon handle and is depguard-barred
from a library bus call ([agent-input.md §4.10 AIS-019]); it MUST send by shelling
`harmonik comms send --from keeper --to <agent> --topic keeper -- <body>`. The keeper MUST
NOT use `comms send --wake` for this delivery, because `--wake` re-enters the pane-paste
primitive this requirement exists to avoid; the non-collision property comes from the
comms subscribe path (no pane write), not from a wake. On the comms path the keeper MUST
NOT issue any `PanePort.Inject` / `SendEscape` write to the leader's pane for that cycle.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

#### SK-023 — Reachability check is presence-Online, read in-process

Before choosing the comms channel the keeper MUST evaluate a named reachability check:
the target agent's presence state, read in-process via `presence.ComputePresenceRegistry`
+ `GetPresenceState` (the keeper is depguard-permitted to import `internal/presence`).
The target is REACHABLE iff its presence is Online with age `< presence.TTL` (120s). A live
`comms recv --follow` reader emits a presence refresh beat every 60s, so presence-Online is
the existing signal that tracks a live follower. The reachability read MUST run on the warn
tick, feeding the SK-024 decision.

> NOTE (stated limitation, SC-2). Presence-Online is NECESSARY BUT NOT SUFFICIENT for an
> armed inbox: a bare `comms join` also shows Online with no reader attached, and a
> follower's 60s beat can hold presence Online for a moment after a `/clear` before the new
> session re-arms a reader. The comms path MAY therefore occasionally deliver to an unread
> inbox. This is non-catastrophic: the unchanged FORCE-ACT / hard-ceiling ladder (SK-028)
> remains the backstop, so an unread nudge still hits the force ceiling and is cut, exactly
> as today. Presence-Online is the v1 reachability definition; a sharper
> "recv-follow-armed" signal (a distinct beat reason, or a daemon subscriber list) is a
> future enhancement, NOT a v1 requirement.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent

#### SK-024 — Delivery channel is a deterministic function of reachability; no silent no-op

The delivery channel MUST be a total, deterministic function of the session role and the
SK-023 reachability read. No branch MAY resolve to a silent no-op. The decision is:

| Session role | Reachability (SK-023) | Delivery |
|---|---|---|
| Leader (captain / admiral) | Online (age < 120s) | comms `agent_message` (SK-022) carrying the K2 defer template (SK-026) + the K3 self-restart command (SK-029) — no pane write |
| Leader | Stale / Offline | terminal fallback (SK-025) |
| Crew | (unchanged this work) | existing behavior; the crew message rides SK-032 config, default off ([park-resume-protocol.md §9 · Crew keeper-message disposition]) |

Every leader-session warn tick MUST resolve to exactly one of {comms, terminal-fallback};
a tick that produces neither is a conformance failure (SK-INV-006).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### SK-025 — Terminal fallback preserves the existing warn path and the retry-Enter loop

When SK-024 routes to terminal fallback, the keeper MUST reuse the existing warn injection
path (`injectTextClocked`) unchanged: it MUST keep the operator-attached-gated text
selection (advisory vs actionable), and it MUST preserve the 750ms-settle + retry-Enter
loop in full (NG3; hk-89g / hk-ip33d / hk-7rgqs — a lone immediate Enter is intermittently
dropped). The fallback MUST NOT be a naive single-Enter send; the retry loop is a
load-bearing reliability fix and MUST NOT be deleted.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

---

### Add new §4.12 — Deferral framing & the good-stopping-point contract (K2). Add after §4.11:

#### SK-026 — The comms nudge body is a normative template with four required structural elements

The leader comms nudge body MUST be a normative template containing exactly these four
structural elements (their prose is tunable per SK-033; their presence is not):

1. **Defer condition A** — if mid-conversation with the operator, finish the exchange first.
2. **Defer condition B** — if mid-task, finish the in-flight unit first.
3. **The good-stopping-point self-test** — the concrete criteria of SK-027.
4. **The self-restart command** — the K3 `keeper restart-now` command (SK-029) carrying the
   cycle nonce (SK-031).

A template that drops any of the four elements is invalid and MUST fall back to the compiled
default per SK-033.

Tags: mechanism

#### SK-027 — The good-stopping-point self-test is the agent-legible four-part criterion

The good-stopping-point self-test embedded per SK-026 element 3 MUST state that a good stop
is one where nothing needed to continue lives only in the agent's context — specifically:
(i) between discrete units, not mid-edit / mid-plan / mid-tool-sequence; (ii) in-flight work
committed or trivially re-derivable; (iii) no unanswered operator question is held; and
(iv) the next session resumes from the handoff plus durable substrate with no redo and no
lost decision. This self-assessment is agent-owned; the keeper nudges and bounds it (the
keeper cannot read the agent's context and MUST NOT claim to detect a task boundary).

Tags: mechanism

#### SK-028 — The deferral sits under the unchanged FORCE-ACT backstop

The K2 deferral MUST sit under the existing FORCE-ACT ceiling and the hard ceiling, both
unchanged. A never-idle session MUST still be cut unconditionally by FORCE-ACT
(`aboveForceThreshold`), and the hard ceiling MUST still trip session-independently. This
requirement changes NO threshold value (NG1 / SC-9): the deferral is legitimized only
because the backstop is preserved verbatim (SK-016 remains in force). "Take your time" is
bounded, not open-ended.

Tags: mechanism

---

### Add new §4.13 — Agent-run self-restart as the default payload (K3). Add after §4.12:

#### SK-029 — The agent-run restart-now command is the default nudge payload

Every leader nudge MUST carry, as its default payload, the command the agent runs to trigger
its own restart: `harmonik keeper restart-now --agent <name>`. That command runs a fully
synchronous verify → freshness-check → ACK → `/clear` → brief in its own process, wholly
independent of the cycle's `HandoffTimeout` (300s) watch window. A handoff written at
T+301s (after the keeper's watch aborted) MUST therefore still restart cleanly, because
restart-now never consults the already-aborted cycle timer (SC-4). The restart-now path MUST
uphold SK-INV-001: it MUST NOT inject `/clear` without a confirmed, fresh handoff — the
agent-run command enforces the same handoff-write-done-precedes-clear ordering, it does not
bypass it (NG5).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

#### SK-030 — restart-now gains a net-new `--nonce` flag, carried for audit

The system MUST add a `--nonce <id>` flag to `keeper restart-now` (matching the existing
`ping --nonce` surface). `RestartNow` MUST record the supplied nonce on its emitted events
and cycle journal, so the self-restart is traceable to the keeper's originating cycle in
`events.jsonl`. The v1 semantics are **carry-for-audit, NOT hard-validate**: the separate
restart-now process does not hold the keeper's live cycle id, so it MUST NOT reject on a
nonce mismatch; attribution is sufficient, matching how `ping` already treats its nonce.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

#### SK-031 — Nonce provenance flows keeper cycle → message → command, with no shared state

The cycle nonce MUST flow by a provenance channel with no shared runtime state: the keeper
mints the `cyc-<ts>-<seq>` `cycle_id` at cycle entry (§7.1, D7), the SK-026 comms message
embeds it verbatim in the `restart-now --nonce <cycle_id>` command string, and the agent
runs that command string verbatim. The keeper's auto-cycle `KEEPER:<cycleID>` marker and
restart-now's `--nonce` echo MUST carry the same `cycle_id` value so the emitted
restart-now events join to the originating cycle (SK-030).

Tags: mechanism

---

### Add new §4.14 — Configurable message text (K4). Add after §4.13:

#### SK-032 — Nudge wording lives in external config, editable without a rebuild

All nudge wording MUST live in the existing `.harmonik/config.yaml`
`keeper.warn_messages` block, threaded to `WatcherConfig`. Editing the YAML MUST NOT
require a rebuild. This block MUST accept the new leader defer-message keys alongside the
existing `default_warn_text` / `actionable_warn_text`, and (per K7) a crew-message key
defaulted empty/off ([park-resume-protocol.md §9 · Crew keeper-message disposition]). A wording change MUST be expressible
as a config edit alone (SC-6b).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### SK-033 — Message structure is normative; prose is tunable via templated slots

The four SK-026 structural elements MUST be exposed to the operator as fixed templated slots
(defer-A, defer-B, stopping-point test, restart-now+nonce command), the way
`restartNowCmdToken` is already templated. An operator override MUST fill only the prose
around those fixed slots and MUST NOT silently drop a load-bearing element. The existing
`containsRestartNowCmd` validation (which rejects a custom actionable text that drops the
`keeper restart-now` command and falls back to the compiled default) MUST be extended to
validate all four required structural elements the same way: any override that omits a slot
MUST fall back to the compiled default rather than ship an incomplete nudge.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### SK-034 — warn_messages is re-read per tick, mtime-gated, for on-the-fly editing

To honor on-the-fly wording edits, the keeper MUST add an mtime-gated per-tick re-read of
just the `keeper.warn_messages` block: it MUST stat the config file each poll and re-parse
only that sub-block when the mtime advances. This live-reload MUST be scoped to
`warn_messages` only; thresholds, bands, and self-service flags MUST stay startup-bound (no
live-reload of any load-bearing decision constant). The strict unknown-key validation
(`ErrUnknownConfigKey`) MUST continue to apply to the re-read.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent

---

### Add new §4.15 — Situational-read sharpening (K5). Add after §4.14:

#### SK-035 — Operator-attached is re-checked in-cycle (TOCTOU)

The keeper MUST re-sample the operator-attached signal during the handoff wait, not only
once at cycle entry. Today operator-attached is sampled once at cycle entry and not
re-checked across the up-to-300s wait; the keeper MUST re-sample it so an operator who
starts typing after cycle entry is respected on the terminal-fallback path. (On the comms
path a present operator is already harmless, because no pane write occurs.)

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent

#### SK-036 — A reachability/liveness pre-check feeds the delivery decision

The keeper MUST run the SK-023 reachability/liveness pre-check before firing, so a
Stale/Offline target routes to terminal fallback (SK-024) rather than firing a comms message
into a dead inbox. This pre-check is the K5 input to the K1 delivery decision; a cycle MUST
NOT dispatch a comms nudge to a target the pre-check reports unreachable.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent

#### SK-037 — A hook-bridge keystroke signal is a named external dependency, out of scope here

A sharper "operator-actively-here" signal (a keystroke/attention beat from the Claude-Code
hook bridge) is the only true fix for remote/mobile operator-present blindness, and it is
NOT reachable without a Claude-Code / hook-bridge change. The system records this as an
external dependency on [claude-hook-bridge.md] and declares it OUT OF SCOPE to implement in
this work. SK-022's comms path makes its absence non-fatal: because the leader nudge no
longer writes the operator's pane, a missed operator-present read cannot corrupt operator
input. This requirement adds no new keeper detection of remote/mobile presence.

Tags: mechanism

---

### Add new invariant SK-INV-006 to §5. Add after SK-INV-005:

#### SK-INV-006 — Leader nudge delivery is total: comms or terminal fallback, never a silent no-op

For every leader-session warn tick that fires, the keeper MUST resolve delivery to exactly
one of {comms `agent_message` (SK-022), terminal fallback (SK-025)}, chosen by the
deterministic SK-024 decision. A fired warn tick that produces neither a comms send nor a
terminal injection is a conformance failure. This is the delivery-totality peer of the
bounded-liveness invariant SK-INV-005: no leader nudge is ever silently dropped.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

## Amendment to §9.3 Co-references (read-only consumption — additive, not renumbering)

These are co-references, NOT frontmatter `depends-on` entries: adding agent-input.md to the
frontmatter dependency list would create a cycle (agent-input.md already depends-on
session-keeper.md). Add to the §9.3 "Co-references" list:

- **[agent-input.md §4.10 AIS-019 / AIS-020]** — the keeper-as-comms-producer identity
  (`--from keeper`, `--topic keeper`) SK-022 shells, and the presence-Online reachability
  read (necessary-but-not-sufficient) SK-023 consumes.
- **[claude-hook-bridge.md]** — the out-of-scope external keystroke/operator-present signal
  named by SK-037.
- **[scenario-harness.md §10.3 SH-035]** — the session-twin integration-tier carve-out that
  covers this spec's pane/timing/handoff/operator-typing behavior outside the SH YAML
  contract (reciprocal to SH-035's reference to this spec).

## Revision-history entry

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-07-18 | 0.3.0 | foundation-author | Keeper restart-delivery redesign for leader sessions (codename: 2026-07-18-keeper-restart-delivery, K1–K5). New §4.11 K1 delivery+reachability: comms `agent_message` for reachable leaders instead of a terminal paste, no `--wake` (SK-022); presence-Online in-process reachability read with the necessary-but-not-sufficient limitation stated (SK-023); the deterministic delivery decision table with no silent no-op (SK-024); terminal fallback preserving `injectTextClocked` + the retry-Enter loop per NG3/hk-89g (SK-025). New §4.12 K2 deferral: the four-element defer template (SK-026), the four-part good-stopping-point self-test (SK-027), the unchanged FORCE-ACT backstop (SK-028). New §4.13 K3 self-restart: the agent-run `restart-now` default payload independent of the 300s watch and upholding SK-INV-001 (SK-029), the net-new carry-for-audit `--nonce` flag (SK-030), the mint→embed→record provenance channel (SK-031). New §4.14 K4 config: the `keeper.warn_messages` external home editable without rebuild (SK-032), structure-normative/prose-tunable templated slots + extended `containsRestartNowCmd` validation (SK-033), the mtime-gated per-tick `warn_messages` re-read scoped away from thresholds (SK-034). New §4.15 K5 situational read: in-cycle operator-attached TOCTOU re-check (SK-035), the reachability pre-check feeding the K1 decision (SK-036), the hook-bridge keystroke signal named as an out-of-scope external dependency (SK-037). New invariant SK-INV-006 (delivery totality). §9.3 Co-references gains agent-input, claude-hook-bridge, and scenario-harness co-refs (co-references, not frontmatter depends-on — avoids a dependency cycle with agent-input.md). ZERO threshold changes (NG1/SC-9): SK-016, the bands, and the gate ladder are untouched; the retry-Enter loop (NG3) and SK-INV-001 (NG5) are preserved. No SK IDs renumbered or retired; SK-001…SK-021 and SK-INV-001…SK-INV-005 unchanged. Status remains `draft`. |
