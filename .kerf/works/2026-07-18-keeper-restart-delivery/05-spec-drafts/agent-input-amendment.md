# Amendment to specs/agent-input.md (v0.1.0 → v0.2.0)

## Frontmatter

- `version: 0.1.0` → `version: 0.2.0`
- `last-updated: 2026-07-14` → `last-updated: 2026-07-18`

## New requirements

The highest occupied requirement ID is AIS-018 (§4.9). The two new requirements introduced
by this kerf (`2026-07-18-keeper-restart-delivery`, K1 reachability substrate) are appended
as AIS-019 and AIS-020 in a new §4.10, after the highest occupied ID — matching the
sequential append pattern used for AIS-000…AIS-018. This amendment is deliberately THIN: the
keeper becomes a new caller on the existing comms/presence surfaces; it does not redesign the
bus. No prior IDs are renumbered or retired; the at-least-once delivery guarantee,
`event_id` dedupe (N3), and the subscribe contract are UNCHANGED and are cited as
non-changes below.

---

### Add new §4.10 — The keeper as a comms producer and presence consumer (K1). Add after §4.9:

#### AIS-019 — The keeper is a recognized comms producer (`--from keeper`, `--topic keeper`)

The system MUST recognize `keeper` as a `--from` producer identity and `keeper` as a
`--topic` value on the comms surface. The keeper delivers a leader nudge by shelling
`harmonik comms send --from keeper --to <agent> --topic keeper` (it holds no daemon handle
and is depguard-denied a library bus call), so an agent MAY filter or route on the `keeper`
topic. The message MUST be a normal durable `agent_message`; it reaches the target
as a turn ONLY via an armed `comms recv --follow` (hk-b51bg). This introduces no new delivery
mechanism: the keeper is a new producer, not a new transport. The keeper MUST NOT join comms
or hold a subscription — it sends fire-and-forget and reads presence out-of-band (AIS-020).
The consuming keeper contract is [session-keeper.md §4.11 SK-022].

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

#### AIS-020 — Presence-Online is the reachability read a producer may rely on, with a stated limitation

The system MUST document that presence-Online (age `< presence.TTL` = 120s) is the signal a
comms PRODUCER may read to decide a target is reachable before delivering, and its known
limitation: a live `comms recv --follow` refreshes presence every 60s, but a bare `comms
join` also produces Online, so presence-Online is **necessary but not sufficient** for an
armed inbox. A producer relying on it MUST treat an Online read as reachable-not-guaranteed
and MUST NOT assume the target's inbox is armed. The sharper "recv-follow-armed" signal (a
distinct beat reason, or a daemon subscriber list) is recorded as a future enhancement, NOT
a current guarantee. The keeper is the first such producer; the reachability decision it
builds on this read is [session-keeper.md §4.11 SK-023].

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent

## Non-changes (stated explicitly)

- No change to the at-least-once delivery guarantee, `event_id` dedupe (N3), or the
  subscribe contract.
- No new bus port, no new event class, no widening of `InputPort`/`Ack`. The keeper
  consumes existing comms-send and presence-read surfaces only.

## Amendment to §9.2 (reverse dependencies — informative note)

Add to the informative reverse-dependency note: the keeper ([session-keeper.md §4.11]) is a
CONSUMER of AIS-019 (the recognized `keeper` producer identity/topic) and AIS-020 (the
presence-Online reachability read).

## Revision-history entry

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-07-18 | 0.2.0 | foundation-author | Keeper-as-comms-producer substrate (codename: 2026-07-18-keeper-restart-delivery, K1). New §4.10 records the keeper as a new caller on existing surfaces: AIS-019 recognizes `keeper` as a `--from` producer identity and `--topic keeper` as the nudge topic, delivered by shelling `harmonik comms send` (no daemon handle, no join, no subscription; reaches the agent only via an armed `comms recv --follow`); AIS-020 documents presence-Online (age < 120s) as the reachability read a producer may rely on, with the necessary-but-not-sufficient limitation (a bare `comms join` also shows Online) and the sharper recv-follow-armed signal recorded as a future enhancement. Thin by design — no bus redesign. Non-changes stated: at-least-once delivery, `event_id` dedupe (N3), and the subscribe contract are unchanged; no new port/event/`InputPort` widening. The consuming keeper contract is [session-keeper.md §4.11 SK-022/SK-023]. No AIS IDs renumbered or retired; AIS-000…AIS-018 unchanged (AIS-007/AIS-008 remain RETIRED). Status remains `draft`. |
