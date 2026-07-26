# Design — crew extension disposition (K7)

Codename: `2026-07-18-keeper-restart-delivery` · pass 4
Grounded by `C6-findings.md` (crew reliability data) + operator direction 2026-07-18.

## Disposition: DEFER the crew message; ship the config hook (default OFF); gate activation on the reliability bugs

**Decision (operator-directed):** the crew finish-then-self-restart message is **deferred**,
but its wording lives in the K4 config surface (`.harmonik/config.yaml` →
`keeper.warn_messages`) **default off**, so it can be turned on and tuned on the fly without a
code change or a new spec later. This work ships only the **config hook**, not a crew-side
behavioral change.

### Why defer (evidence)
C6 proved crews do NOT have the timing problem the leader message solves: no operator
conversation, warn/restart ≈ 0.28, restarts already land at idle pauses, and crews survive on
durable substrate rather than the handoff. So a crew *message* is low-value **until** the
crew *reliability* failures are fixed — otherwise the message just fails the same way the
current restart does (dead watcher can't deliver it; a parked crew can't answer it).

### Activation gate (external dependency — NOT fixed here)
Turning the crew message on is safe only after the `keeper-reliability` bug track lands,
specifically:
- **hk-220lv** (dead keeper watcher, no auto-revive) — a message can't be delivered by a dead
  watcher.
- **hk-4tjyj** (reboot discards a written handoff) — a crew that pays to write a handoff must
  have it consumed.

The self-restart command (K3) is *especially* valuable for crews once activated: a crew parked
to wake hourly cannot answer the keeper's 300s watch, so **self-restart-on-wake is the only
mechanism that reaches it** — which is exactly why the crew message rides the same K3 command
the leaders use.

### What this work delivers for K7
1. The `keeper.warn_messages` config accepts a **crew-message key**, default empty/off.
2. The `self_service.crews_enabled` flag (already exists, `watcher.go:409-416`, default-off
   pattern) governs whether crews receive the actionable form — this is the on/off switch.
3. Spec text in crew-handoff-schema.md / park-resume-protocol.md records the disposition and
   the two-bug activation gate.

**No crew-side implementation beyond the config hook.** The four `keeper-reliability` bugs are
the captain-delegated bug track, out of scope here (NG2).
