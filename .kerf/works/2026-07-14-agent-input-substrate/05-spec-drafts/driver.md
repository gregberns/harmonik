# 05 spec-draft index — component `driver` → target spec files

> The `driver` research/design component (C2 structured-protocol driver, C7 WAL-guard fold-in)
> produces NO spec file of its own. Its normative text lives in the target-spec drafts below.

- **`agent-input.md`** (NEW, prefix AIS) — AIS-006 second Substrate impl over `substrate.Run[E,A]`
  (claudewire codec + Event/Action + pure Step), AIS-007 corpus-first codec-freeze spike gate,
  AIS-008 billing gate, AIS-009 direct-child stdio ownership, AIS-010 StdinDevNull disposition,
  AIS-015 substrate-selection axis + twin-blind, AIS-016 remote seam preserved, AIS-017 WAL-guard
  adapt-not-delete.
- **`event-model.md`** — §8.21 registration of the two driver-emitted `agent_input_*` events +
  §6.3 payload structs (the driver's emission surface).
