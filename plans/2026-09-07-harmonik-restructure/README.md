# Harmonik Restructure — Track 1 of 2

Started 2026-09-07. Operator-initiated. Sibling track: [`../2026-09-07-harmonik-bus/`](../2026-09-07-harmonik-bus/).

## Why this exists (operator intent, recorded verbatim in spirit)

The project is too massive. `core` especially has far too much going on. After a
month of refactoring, building, and testing, the system still runs dog slow. Two
big fixes already shipped and were not enough.

The restructure pulls the genuinely good code out of the tangle into a **clean
room** — a separate Go module behind a compiler-enforced one-way wall. The nasty
packages stay where they are. The good ones move across, one testable piece at a
time. Then builds and tests must run **really damn fast**.

> **The bar is high on purpose.** It will be tempting to decide "Harmonik is fine,
> nothing to fix here." That is a lie. This is a mess. A package earns a move into
> the clean room only with measured evidence against `PRINCIPLES.md` — never on
> vibes. The default answer to "should this move?" is **no** until proven.

## What we do first

1. **Get the scaffolding in place** so more development can proceed — the clean
   module, `go.work`, the wall check. This is the enabling step; everything else
   waits on it.
2. Focus first on **`core`** (the idea of "core functionality" is documented — the
   base plan pins down exactly what it is) and on **keeper** (believed relatively
   clean; should move early and also build as its own standalone binary).

## Files

- `00-seed-playbook.md` — the operator's strangler-fig / clean-room playbook. North star.
- `01-base-plan.md` — the grounded base plan: core definition, admission bar,
  current-state measurements, clean-room setup, port order, keeper-as-binary. *(In progress.)*

## Relationship to the bus track

The bus (Track 2) lives in `libs/transport` in the target layout. It needs the
clean-room module to exist first. Interface design and planning for the bus can
proceed in parallel; the code that lands in the clean room waits on the scaffolding
this track delivers.
