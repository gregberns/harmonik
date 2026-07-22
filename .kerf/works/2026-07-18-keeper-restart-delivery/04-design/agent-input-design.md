# Design — agent-input.md change (K1 reachability substrate)

Codename: `2026-07-18-keeper-restart-delivery` · pass 4
Grounded by `03-research/delivery-reachability/findings.md` Q2/Q3.

## Scope: thin — the keeper is a NEW caller, not a bus redesign

The keeper becomes a **new producer on the comms surface** and a **new consumer of
presence**. agent-input.md records these two facts; it does not change the bus.

### 1. Keeper as a comms producer (send path)
- The keeper delivers a leader nudge by shelling `harmonik comms send --from keeper --to
  <agent> --topic keeper` (it holds no daemon handle; depguard `.golangci.yml:189` denies a
  library call). Document `keeper` as a recognized `--from` identity and `--topic keeper` as
  the nudge topic, so an agent can filter/route it.
- The message is a normal durable `agent_message` (F-class, `commshandler_nbrmf.go:140`); it
  reaches the agent as a turn only via an armed `comms recv --follow` (hk-b51bg). No new
  delivery mechanism.

### 2. Presence as the reachability signal (read path)
- Document that presence-Online (age < `presence.TTL` = 120s, `presence.go:48`) is the signal
  a *producer* may read to decide a target is reachable, and its **known limitation**: a live
  `comms recv --follow` refreshes presence every 60s (`comms.go:1517`), but a bare `comms
  join` also produces Online, so presence-Online is **necessary but not sufficient** for an
  armed inbox. Record the sharper "recv-follow-armed" signal (a distinct beat reason or a
  daemon subscriber list) as a future enhancement, not a current guarantee.

### Non-changes
- No change to at-least-once delivery, `event_id` dedupe (N3), or the subscribe contract.
- The keeper does not join comms or hold a subscription — it sends fire-and-forget and reads
  presence out-of-band.
