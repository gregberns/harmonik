# Amendment to specs/park-resume-protocol.md (v1.1.0 → v1.2.0)

## Home selection

The K7 crew-extension disposition lands in **park-resume-protocol.md**, not
crew-handoff-schema.md. Rationale for the choice (this section is amendment prose, not spec
text): park-resume-protocol.md §9 W4 ("Crew context-pressure handoff — L2→L0 self-stop") is
the exact behavior a crew keeper-WARN message would drive, so the disposition belongs beside
it. crew-handoff-schema.md owns the mission-file byte-contract (the captain-authored
frontmatter); the crew keeper message is NOT part of that frontmatter — its wording lives in
`.harmonik/config.yaml` `keeper.warn_messages`, a keeper config surface. Therefore
crew-handoff-schema.md is NOT amended by this work.

## Frontmatter

- `version: 1.1.0` → `version: 1.2.0`
- `last-updated: 2026-06-21` → `last-updated: 2026-07-18`

## New section

park-resume-protocol.md is a contract-shape spec with no requirement-ID prefix; the
disposition is added as new normative prose under §9 as a named subsection (matching §9's
existing named subsections W1/W3/W4/Teardown — §9 uses no decimal numbering), and a changelog
row is appended. No existing section is renumbered.

---

### Add new subsection "Crew keeper-message disposition (K7 — DEFERRED)". Add after the Teardown-as-transition subsection of §9:

The finish-then-self-restart keeper nudge that leader sessions receive (the leader defer
message + agent-run restart command, [session-keeper.md §4.11–§4.13]) is **DEFERRED** for
crews. This work delivers the config hook only, default OFF; no crew-side behavioral change
ships here.

**Disposition (operator-directed 2026-07-18): DEFER the message; ship the config hook
default-off; gate activation on the reliability bugs.**

1. The `keeper.warn_messages` config ([session-keeper.md §4.14 SK-032]) MUST accept a
   crew-message key, defaulted empty/off, so the crew message can be turned on and tuned on
   the fly without a code change or a new spec.
2. The existing `self_service.crews_enabled` flag (default-off) MUST govern whether crews
   receive the actionable self-restart form; it is the on/off switch.
3. No crew-side implementation beyond the config hook ships in this work. When activated, the
   crew message MUST ride the same K3 `keeper restart-now` command the leaders use
   ([session-keeper.md §4.13 SK-029]) — for a crew parked to wake hourly, self-restart-on-wake
   is the only mechanism that reaches it, since a parked crew cannot answer the keeper's 300s
   watch.

**Activation gate (external dependency — NOT fixed here).** Turning the crew message ON is
safe only AFTER the `keeper-reliability` bug track lands, specifically:

- **hk-220lv** (dead keeper watcher, no auto-revive) — a message cannot be delivered by a
  dead watcher.
- **hk-4tjyj** (reboot discards a written handoff) — a crew that pays to write a handoff must
  have it consumed.

Until both land, the crew-message key MUST stay default-off. The four `keeper-reliability`
bugs are the captain-delegated bug track and are OUT OF SCOPE here (NG2); this spec
references them only as the activation gate and does not specify their fixes.

> NOTE (why defer, not adopt). C6 evidence: crews do NOT have the timing problem the leader
> message solves — no operator conversation, warn/restart ≈ 0.28, restarts already land at
> idle pauses, and crews survive on durable substrate rather than the handoff. A crew
> message is therefore low-value until the crew reliability failures above are fixed;
> shipping it before then would fail the same way the current restart does. This is a
> disposition NOTE, not an operating rule.

---

## Changelog entry

| Version | Date | Author | Summary |
|---|---|---|---|
| 1.2.0 | 2026-07-18 | foundation-author | K7 crew keeper-message disposition (codename: 2026-07-18-keeper-restart-delivery). New §9 subsection "Crew keeper-message disposition (K7 — DEFERRED)" records the operator-directed disposition: DEFER the crew finish-then-self-restart message, ship the config hook default-off, gate activation on the `keeper-reliability` bugs. The `keeper.warn_messages` crew-message key (default empty/off) and the existing default-off `self_service.crews_enabled` flag are the on/off surface ([session-keeper.md §4.14 SK-032]); when activated the crew message rides the same K3 `keeper restart-now` command leaders use ([session-keeper.md §4.13 SK-029]). Activation is gated on hk-220lv (dead watcher) and hk-4tjyj (discarded handoff) landing; those four `keeper-reliability` bugs are out of scope here (NG2). No crew-side implementation ships beyond the config hook. Home chosen as park-resume-protocol §9 (beside W4 crew context-pressure handoff); crew-handoff-schema.md is NOT amended (the crew message is keeper config, not mission-file frontmatter). No existing sections renumbered. Status remains `draft`. |
