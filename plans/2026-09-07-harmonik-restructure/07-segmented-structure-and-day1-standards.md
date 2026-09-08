# The segmented structure, and the standards that hold from commit one

**Date:** 2026-09-07 · **Status:** DIRECTION — authoritative for the platform re-grounding.
Referenced from `AGENTS.md` so every agent finds it. Supersedes the "clean room" / `clean/`
language in the seed docs (`00-seed-playbook.md`) and in `01`/`02` on this one axis.

## 1. There is no "clean" folder. There is a segmented structure.

The word "clean" implied a two-zone split (clean vs dirty) and a folder named `clean/`. Drop it.
The structure is defined by **ownership segments**, and the not-yet-moved code is simply
**unsegmented**, not "dirty".

**Top-level segments — each is a module (a `go.work` member), named by what it owns:**

```
harmonik/
  go.work
  contract/     module   # the wire proto (kernel.proto, plugin.proto) + generated types. A pure leaf.
  shared/       module   # vocabulary + ports used by ≥2 distinct owner segments TODAY. Admission-gated.
  kernel/       module   # the substrate: transport, channels, roster, lookup, state, plugin host.
  tools/
    keeper/     module   # a tool = library core + adapter + thin cmd/<name>/main.go. Its own binary.
    dispatch/   module
    comms/      module
    queue/      module
  cmd/
    harmonik/   module   # composition root / supervisor. Wires; holds almost no logic.
  internal/               # the UNSEGMENTED zone. Existing packages live here until assigned an owner.
```

- **No `clean/`. No "clean room".** Say "the segmented structure" or name the segment
  (`kernel`, `tools/keeper`, `shared`).
- **`internal/` is the unsegmented zone**, not "legacy" and not "dirty". A package leaves it only
  when it is assigned an owner segment AND meets the admission bar (§3). `core` and the daemon
  god-package stay here longest — the core split is LAST, by locked decision.
- **Why modules, not just folders (reinforced by the subprocess decision A1):** each tool is its
  own hot-reloadable, independently cross-compiled (`CGO_ENABLED=0`) binary, so a module is the
  natural unit. And a module physically cannot reach another module's `internal/` — so "kernel
  libraries mixed with keeper libraries" becomes impossible, not merely discouraged. That is the
  operator's segmentation directive (A3) enforced by the compiler.

## 2. The one-way dependency rule (replaces the "wall")

- `tools/*` → `{contract, shared}` only. **Never `tool → tool`** (tools coordinate over the
  kernel's socket, not by importing each other). **Never `tool → kernel`** (a tool is a plugin
  that speaks the wire contract; it does not import the substrate).
- `kernel` → `{contract, shared}` only.
- `shared` → nothing in this project (leaf; std + vetted third-party only).
- **Unsegmented `internal/` may import any segment; no segment may import `internal/`** — except
  through a consumer-owned port the segment declares. One-way, always.

Enforced by module boundaries + an import-closure check + a tool-isolation check, all in
`make full` from the first commit (§3).

## 3. Best practices hold from commit one — moved code earns entry, it is not grandfathered

The operator's rule: all new code, and everything that moves into a segment, follows the
practices in `PRINCIPLES.md` from day 1. Mechanisms, all live in `make full` before the first
segment package lands:

1. **Each segment module ships its own full-strength `.golangci.yml` with an EMPTY allow-list.**
   Zero grandfathered findings may cross from `internal/`. A package that cannot pass the full
   linter does not enter a segment — it stays in `internal/` behind a port until it is fixed.
2. **The boundary tests are code, not etiquette:** the import-closure check (no segment imports
   `internal/`), the tool-isolation check (no `tool → tool`, no `tool → kernel`), the kernel
   **vocabulary test** (no domain noun — bead, run, session, agent — appears in `kernel/`), and
   the **`[]byte`-payload rule** (the kernel cannot parse a payload). These are the platform
   guardrails, expressed as principle-backed checks per the locked OQ-1 decision.
3. **Verification-first (A2) is the acceptance gate, not unit tests.** A segment slice is "done"
   when its fault-injection harness survives (partition, kill, reload-under-load, backpressure) —
   see `../2026-09-07-harmonik-bus/08-verification-first-plan.md`. A green unit suite is
   necessary, never sufficient.
4. **`PRINCIPLES.md` is the enforced review bar.** Every non-trivial commit is reviewed against
   it; the admission bar (below) is its concrete form for a move.

**Admission bar for moving a package into a segment** (from `01-base-plan.md`, kept):
purity (effects behind consumer-owned ports — no direct `time.Now`/`exec.Command`/`os.Exit`/
package globals in domain code), the one-way rule holds, and full-strength lint passes with no
allow-list. Default answer to "should this move?" is **no** until it meets the bar.

## 4. The placement rule — every extracted package has an owner before the move

Before any `git mv`, match the package against this ordered gate and record the result in the PR:

1. Is it the substrate (transport/channel/roster/lookup/state/plugin-host)? → `kernel`.
2. Is it a wire type? → `contract`.
3. Does exactly one tool own it? → `tools/<that-tool>`.
4. Is it effect-free vocabulary already imported by ≥2 distinct owner segments TODAY? → `shared`.
5. Otherwise → it stays in `internal/` behind a consumer-owned port until one of the above is true.

"Drop it in `shared`" is never the default — `shared` admits a package only when a real second
consumer exists in committed code, never on a promise of future reuse.

## 5. Framing, made explicit for all agents

This is a **re-grounding** of the locked P1/P2/P3 platform program (2026-07-21), with three
operator amendments applied (`../2026-09-07-harmonik-bus/07-operator-amendments-2026-09-07.md`):
**A1** subprocess plugins (C6 reversed — live-reload + crash isolation), **A2** verification-first,
**A3** the segmented structure above. The design of record is **P1's scope on substrate-v2's
subprocess plugin model**; raw substrate-v2 is reference/evidence, `p1-kernel-fabric` is the
scope/boundary authority. Bus track = **P1 (kernel)** + **P3 (dispatch)** (P1/P3 were not
superseded). The **restructure track is decomposition of the daemon/core — the same target as
the ACTIVE `delete-and-rewrite` program**, NOT the superseded `p2-extraction` (whose task cards
are dead; see `05-relation-to-active-program.md`). Whether the segment-module strategy replaces,
follows, or runs alongside delete-and-rewrite's in-place decomposition is an **open operator
decision**. No agent should design a new bus, name it "transport", call the structure "clean",
or defer hub/spoke — those are settled here.
