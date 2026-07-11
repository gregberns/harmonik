# Pi Multi-Provider Slot Accounting — Design

> Epic: hk-8ziid (MR2: Pi multi-provider concurrent — local DGX/ornith + OpenRouter at once).
> Component: hk-8ziid.1 (C1-design). Consumed by hk-8ziid.2 (C2-wiring) and hk-8ziid.3 (C3-wiring).

## Problem

[`pi-provider-switch`](../../specs/pi-provider-switch.md) makes Pi's provider tuple
switchable **per bead**, but says nothing about *how many beads run against each
provider at once*. Today the daemon has exactly two concurrency dimensions:

- **Global** — `RunRegistry.Len()` vs. `ConcurrencyController` (`max_concurrent`).
- **Per-queue** — `RunRegistry.LenForQueue(name)` vs. `Queue.Workers`
  (`specs/queue-model.md` §9.3 QM-062).

Neither dimension is provider-aware. With two substrates in play — a local
DGX/ornith box and OpenRouter — an operator who wants **both substrates kept
full simultaneously** has no lever: dispatch either serializes behind one
global ceiling or has to be manually balanced by hand-labeling every bead's
`profile:` to alternate providers. This epic adds a third, orthogonal
dimension — **per-provider slot accounting** — so the daemon can keep N runs
in flight on OpenRouter and M runs in flight on ornith concurrently, each
bounded independently, without either substrate starving or overrunning its
own capacity.

This bead (C1) is the design + struct-contract component. It does not wire
provider-aware dispatch decisions into the work loop — that is C2 (carry the
resolved provider onto the run handle + emit `provider_selected`) and C3
(read the per-provider tally at the dispatch gate). C1 defines the shape both
of those land into.

## Model

### Identity: provider string, not profile name

A `profile:<name>` (e.g. `ornith-dgx`, `openrouter-cloud`) is a named bundle
of `{provider, model, api_key_env, api_key_file, base_url, api}`
(`specs/pi-provider-switch.md` §5–7). Multiple profiles can point at the same
underlying substrate (e.g. two ornith profiles pinning different reasoning
models on the same DGX box) — slot accounting must cap the **substrate**, not
the profile. So the accounting key is the resolved `provider` string itself
(`profile.Provider`, e.g. `"openrouter"` / `"ornith"`), the same value that
already rides `RunCtx.Provider` / `PiHarness.provider` per the C1 contract in
`pi-provider-switch.md` §5. This keeps the model correct even as new profiles
are added against an existing substrate.

### Config: `harnesses.pi.provider_slots`

```yaml
harnesses:
  pi:
    provider_slots:
      openrouter: 4
      ornith: 2
```

Added in this bead as `rawHarnessesPiConfig.ProviderSlots` /
`PiHarnessConfig.ProviderSlots` (`internal/daemon/projectconfig.go`), a plain
`map[string]int` copied verbatim by `parseHarnessesBlock` — no validation
added here (existence/shape validation, if any, is C2/C3's concern once a
dispatch gate actually reads it, mirroring how C2 owned config validation in
the profile-switch design). Absence (nil/empty map, or a provider with no
entry) means **unbounded** for that provider — gated only by the existing
global `max_concurrent` and per-queue `Workers` ceilings. This is the
backward-compat invariant: a deployment that never sets `provider_slots`
behaves byte-identically to today, exactly as the profile-switch work
preserved byte-identical behavior for beads with no `profile:` label.

An entry `<= 0` is treated as unbounded, not zero — a slot count of zero
would silently wedge every bead resolved to that provider, which is never
what an operator setting `provider_slots` wants; to actually exclude a
provider, remove/disable its profile(s) instead.

### Run-handle seam: resolved provider

`RunHandle` (`internal/daemon/runregistry.go`) gains a `resolvedProvider
atomic.Pointer[string]` field with `SetResolvedProvider` /
`GetResolvedProvider` accessors, mirroring the existing
`agentType`/`SetAgentType`/`GetAgentType` pattern used for the resolved
harness type. The pointer (not a plain string) distinguishes three states
that a plain string cannot:

1. **Not yet resolved** — `GetResolvedProvider()` returns `("", false)`.
   Before C2 runs the claim-time profile resolver (or for a non-Pi run,
   permanently).
2. **Resolved to the harness-global default** — `("", true)`. A Pi run with
   no `profile:` label still has a provider (the daemon-global
   `harnesses.pi.provider`); C2 sets it explicitly even though the string is
   informationally "no override", so that `LenForProvider` can still count
   it against that provider's slot ceiling.
3. **Resolved to a named provider** — `("openrouter", true)` etc.

Distinguishing (1) from (2) matters because a run that hasn't been resolved
yet must not be silently double-counted as `""` against whatever provider
happens to also resolve to the empty string — there is no such provider, but
the explicit `ok` boolean keeps the contract unambiguous as the accounting
logic evolves.

### Registry query: `RunRegistry.LenForProvider`

`RunRegistry.LenForProvider(name string) int` mirrors `LenForQueue` /
`LenForQueueLocal`: it walks the live handle map and counts runs whose
`GetResolvedProvider()` returns `(name, true)`. Runs with `ok == false` are
excluded from every per-provider tally (same convention as
`LenForQueueLocal` excluding remote runs from the local tally).

## How C2/C3 build on this

- **C2** (hk-8ziid.2): at claim time, after the existing `resolvePiProfile`
  step (`specs/pi-provider-switch.md` §7), call
  `runHandle.SetResolvedProvider(resolvedProfile.Provider)` (or the
  harness-global `PiHarnessConfig.Provider` when no profile matched) and emit
  a `provider_selected` event carrying the same value, so operators can see
  the per-run routing decision without reading the run handle directly.
- **C3** (hk-8ziid.3): thread `ProviderSlots` into `workLoopDeps` and add a
  capacity check — parallel to the existing global/per-queue gates in
  `runWorkLoop` — that skips dispatch to a bead resolved to a full provider
  and tries the next ready item, the same "block only when actually full"
  shape the per-queue gate already uses (`workloop.go` "Block only when
  local is full AND no remote worker has a free slot" comment,
  `RunRegistry.LenForQueueLocal`). C3 is dispatch-gate wiring only; it does
  not change what provider a bead resolves to (C2's job) or which profile a
  bead's labels select (`pi-provider-switch` C3's job).

## Non-goals (this bead)

- No dispatch-gate wiring (C3).
- No `provider_selected` event emission (C2).
- No validation of `provider_slots` values (deferred to whichever of C2/C3
  first needs it — currently unvalidated, consistent with "no product code
  changes below this bead's contract").
- No change to `pi-provider-switch`'s profile resolution, model precedence,
  or credential handling — this model is additive and orthogonal.
