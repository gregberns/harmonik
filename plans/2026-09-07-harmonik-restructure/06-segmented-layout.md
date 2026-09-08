# Segmented code layout — restructure + kernel

The operator's directive A3 (`../2026-09-07-harmonik-bus/07-operator-amendments-2026-09-07.md`):
segment moved code by OWNER. The kernel must not share a folder with keeper. Keeper must not share a
folder with dispatch. A small shared segment is allowed, but only with an admission rule so it does
not become a junk drawer. Every package pulled out of the not-clean daemon gets an owner segment
BEFORE the `git mv`. "Drop it in the shared pile" is not an allowed default.

This document states that layout, the one-way dependency rules, the shared-admission rule, the
placement procedure, a worked mapping of real packages, and how it reconciles the two seed layouts.

It builds on `00-seed-playbook.md` (strangler-fig, one-way wall) and `01-base-plan.md` (core is a
131k-line, fan-in-60 leaf; the daemon is the fan-in-50 god-package; split core LAST). It applies the
new constraint A1: **plugins are subprocesses**, each its own binary, cross-compiled
`CGO_ENABLED=0`, that speak a wire contract to the kernel — not in-proc Go libraries. A1 changes the
recommended module strategy, so read §6 for why.

---

## 1. The target segmented layout

Each top-level directory is a **Go module** with its own `go.mod`, tied together by a top-level
`go.work`. A module is the unit A3 asks for: a compiler-enforced wall, not a naming convention. It is
also the unit A1 needs — one subprocess binary per plugin, cross-compiled on its own.

```
harmonik/
  go.work                       # use ./legacy ./contract ./shared ./kernel ./tools/* ./cmd/harmonik

  legacy/                       # THE NOT-CLEAN ZONE — the current module, unchanged
    go.mod                      #   module github.com/gregberns/harmonik  (path stays)
    internal/core               #   the 131k-line leaf — STAYS here; split LAST (base-plan §3)
    internal/daemon             #   the fan-in-50 god-package — shrinks as segments below grow
    internal/... (everything not yet ported)

  contract/                     # THE WIRE — proto-generated Go for the kernel<->plugin boundary
    go.mod                      #   module .../contract
    fleet/kernel/v1/            #   kernel.proto + plugin.proto generated types; opaque []byte payloads
                                #   NO logic, NO domain nouns. A pure leaf. Imports nothing of ours.

  shared/                       # GENUINELY-SHARED vocabulary + ports — admission-gated (see §3)
    go.mod                      #   module .../shared
    clock/                      #   the ClockPort (today internal/substrate)
    cli/                        #   the Env + Run harness every tool binary shares
    log/                        #   structuredlog, once a second segment imports it

  kernel/                       # THE SUBSTRATE — one segment, one module (P1's internal/kernel, promoted)
    go.mod                      #   module .../kernel
    transport/                  #   the bus: in-mem + NATS; leaderless (DECISIONS C3)
    channels/                   #   the four channel types; roster / lookup / state
    pluginhost/                 #   subprocess host + the ~40-line drain gate (A1)
    cmd/harmonikd/main.go       #   the kernel daemon binary (host process)
                                #   imports contract + shared. NEVER imports a tool.

  tools/                        # ONE SEGMENT PER TOOL — a tool's library never mixes with another's
    keeper/
      go.mod                    #   module .../tools/keeper
      keeper/                   #   the PURE decision core (decide(reading,cfg,clock) -> Decision)
      adapter/                  #   the plugin adapter — speaks contract over the socket
      cmd/keeper/main.go        #   the standalone plugin binary (CGO_ENABLED=0)
    dispatch/ go.mod  ...       #   same shape
    comms/    go.mod  ...       #   same shape
    queue/    go.mod  ...       #   same shape (queue is an application on the transport, not the kernel)

  cmd/
    harmonik/                   # THE COMPOSITION ROOT — the umbrella CLI + supervisor
      go.mod                    #   module .../cmd/harmonik
      main.go                   #   wires the kernel host, registers plugin launch specs, supervises
                                #   subprocesses. Almost no logic of its own.
```

Per-tool shape (unchanged from the seed's "library + thin main"): a **pure library core**, a **plugin
adapter** that speaks the `contract` wire, and a **thin `cmd/<tool>/main.go`**. `go build
./tools/keeper/cmd/keeper` gives the standalone plugin binary; the kernel host execs it and talks to
it over a unix socket. Two entrypoints, one library.

One correction to the seed's embedded-vs-networked story: the seed (written under the old in-proc C6)
said the umbrella hands each tool an in-memory bus by direct function call. A1 reverted C6. The
deploy model is now subprocess: `cmd/harmonik` (or `kernel/cmd/harmonikd`) execs the plugin binaries
and they speak gRPC over a local socket. The library-core + thin-main split still holds per tool; the
composition root wires the KERNEL and registers plugin specs, it does not link tool code in-proc.

---

## 2. The dependency rules — one-way only

```
   contract  <----  shared  <----  kernel
      ^                ^             (host)
      |                |
      +------- tools/* +
                 |
              cmd/harmonik  (imports tool libs to embed subcommands + kernel host to supervise)

   legacy  ---->  may import any clean segment (the one-way wall; the reverse is forbidden)
```

| From \ May import | contract | shared | kernel | a tool | legacy |
| --- | --- | --- | --- | --- | --- |
| `contract` | — | no | no | no | no |
| `shared` | yes | — | no | no | no |
| `kernel` | yes | yes | — | **no** | no |
| `tools/<a>` | yes | yes | **no** | **no (never tool→tool)** | no |
| `cmd/harmonik` | yes | yes | yes | yes | no |
| `legacy` | yes | yes | yes | yes | — |

Two rules carry the segmentation:

- **A tool never imports the kernel, and a tool never imports another tool.** This is the big A1 win.
  The kernel↔plugin boundary is a wire contract (`contract/`), not a Go import. A tool links
  `contract` + `shared` only; it reaches the kernel and every other tool THROUGH the socket, never by
  import. So "keeper's library never mixes with dispatch's" is enforced by the compiler, not by
  discipline — neither module lists the other in its `go.mod`.
- **The kernel stays a substrate.** It imports `contract` and `shared` and nothing of a tool's. The
  recurring urge to make the kernel import a tool is the DECISIONS "healthy kernel stops growing"
  smell — investigate it, do not satisfy it.

Enforcement — three mechanical checks in `make full`, each a signal that prompts thought, not a law:

1. **Module boundaries.** A module cannot import another module's `internal/`. Free wall for segment
   isolation.
2. **The legacy wall.** For every clean module: `go list -deps ./... | grep gregberns/harmonik/internal`
   must be empty (the base-plan §4B closure check; catches transitive breaches, not just direct).
3. **The tool-isolation check.** No `tools/<a>/go.mod` may `require` another `tools/<b>` module, and
   no tool module may require `kernel`. A three-line CI grep over the `go.mod` files.

---

## 3. The shared-segment admission rule

`shared/` earns its name only against a rule that a paper claim cannot pass. A package may enter
`shared/` only when ALL hold:

1. **A real second consumer exists TODAY.** Two DISTINCT owner segments (kernel, or two different
   tools) already import it in committed code. Mechanical: list the importers; count distinct
   top-level segments; fewer than two → rejected. "Might be reused" and "keeper will want this later"
   both fail. A package with exactly one consumer lives INSIDE that consumer until a second appears.
2. **No process or effect concerns.** No `os.Exit`, no global flags, no `time.Now()` / `exec.Command`
   called (only injected through a port). It is vocabulary and ports — values in, values out.
3. **It imports no tool and not the kernel.** `shared` is a sink at the bottom of the graph. If a
   candidate needs a tool or the kernel, it is not shared vocabulary; it belongs to that owner.

The rule is hard to game because rule 1 is a `grep` over real imports (a promise of future reuse
scores zero) and rules 2–3 are the same purity checks the clean-room bar already runs. When a package
fails rule 1 by one consumer, the correct move is to leave it in the single segment that uses it and
promote it later — never to pre-place it in `shared` "to be safe".

---

## 4. The placement rule for moved code

Run this BEFORE the `git mv`. The extraction PR names the target segment AND the matched rule number.
There is no default bucket; if no rule matches, the package stays in `legacy`.

1. **Is it the substrate itself** — transport, the four channel types, roster / lookup / state, the
   plugin host, the drain gate? → `kernel/`.
2. **Is it a pure wire message** — a proto type on the kernel↔plugin boundary? → `contract/`.
3. **Does exactly ONE tool own the behavior** — keeper's watching, dispatch's hand-off, comms'
   messaging, queue's work-holding? → `tools/<that tool>/`.
4. **Is it effect-free vocabulary or a port that ≥2 owner segments import TODAY** (the §3 rule)? →
   `shared/`. If only one segment imports it, it goes to THAT segment, not to shared.
5. **None of the above, or still fused to `core` / `daemon`?** → it STAYS in `legacy`. Invert the seam
   with a consumer-owned port (PRINCIPLES §4) so the clean side depends on a behavior, not on legacy.
   Port it later when rule 1–4 can answer.

The order matters: check kernel and single-tool ownership BEFORE shared, so a package with one clear
owner never lands in shared by accident. Shared is the narrowest gate, reached last.

---

## 5. Worked mapping — real current `internal/` packages

Measured on this machine at current `main` (transitive internal deps in brackets).

| Current package | Target segment | Rule | Reason |
| --- | --- | --- | --- |
| `substrate` [1] | `shared/clock` | 4 | The ClockPort — effect-free leaf, injected everywhere. The shared vocabulary the purity gate needs. |
| `eventbus` [2] | `kernel/transport` | 1 | The bus IS the kernel's transport (DECISIONS C3, leaderless). One `time.Now` to route through the shared clock. |
| `presence` [3] | `kernel/channels` | 1 | Roster / liveness / state is kernel work per C3, not a tool. Clean once eventbus lands. |
| `queue` [2] | `tools/queue` | 3 | Work-dispatch is an application ON the transport (C3), not the kernel. The PRINCIPLES §4 exemplar of consumer-owned ports; only tentacle is core. |
| `dispatch` [3] | `tools/dispatch` | 3 | Hand-off to a worker is one tool's job. Tentacles core + queue — reaches queue over the bus, not by import. |
| `keeper` [13] | `tools/keeper` | 3 | Its own tool segment. Sever the `digest` and `dashboard` tentacles first (base-plan §6) — they drag the whole run machine and fail the wall. |
| comms (in `cmd/harmonik` + `crew`/`lifecycle`/`presence`) | `tools/comms` | 3 | The agent bus is one tool. Today it is glued into `cmd` and reaches `crew`/`lifecycle`; invert those into ports on the move. |
| `structuredlog` [2] | `shared/log` (candidate) | 4 | Tiny, only tentacle is core. Enters `shared` the moment a SECOND segment imports it; until then it rides with its one consumer. |
| `handlercontract` [3] | `shared/` (candidate) or `legacy` | 4/5 | Contract vocabulary. Enters shared only if ≥2 segments import it today; otherwise it waits in legacy behind a port. High afferent (19) so high value once the seam is cut. |
| `runexec` / `runloop` / `run` [3–17] | `legacy` for now → later a `tools/run` | 5 | The run machine, the high-risk keystone (17 deps). Stays in the not-clean zone; port LATE, after the spine. |
| `core` [leaf, fan-in 60] | `legacy` — STAYS | 5 | Do NOT split (two prior programs proved the 13 type families are a mutual cycle). Port only the minimal types each moved package needs into `shared`/`contract` on demand. |
| `daemon` [fan-in 50] | `legacy` — STAYS | 5 | The god-package and top of the graph. It shrinks as kernel + tools grow; its socket listener eventually becomes `cmd/harmonik`. Never moves as a unit. |

The recurring fact from the base-plan holds: almost every good candidate's only real tentacle is
`core`, so the enabling first move is porting a minimal typed vocabulary — that lands in `shared/` (or
`contract/` for wire types), NOT as a copy of the 131k-line `core`.

---

## 6. Reconciling the two seed layouts

Two layouts are on the table, and they disagree on the module strategy:

- **The restructure seed (`00-seed-playbook.md`):** `libs/ · tools/ · cmd/` as separate go.work
  modules, one-way wall.
- **P1 platform-architecture:** `internal/kernel` as a package in the SAME module, kept honest by a
  dep-allowlist test.

**Recommend the multi-module go.work layout, not the single-module internal/kernel.** The reason is
A1. P1 chose the same-module in-proc kernel under decision C6 — plugins were in-proc Go libraries, and
an in-proc Go interface mocks more cleanly than cross-process gRPC. **A1 reverted C6.** Plugins are now
subprocesses that speak a wire contract, so:

- The kernel↔plugin boundary is a socket, not a Go import. The single-module dep-allowlist that P1
  needed to police that import edge has nothing left to police — there is no import edge. Modules give
  the same isolation for free.
- Each plugin is an independently deployed, independently versioned, `CGO_ENABLED=0`-cross-compiled
  binary. A module is exactly the unit of independent build and version. One `go build` per tool
  module produces one plugin artifact.
- A3's "no flat library folder" is satisfied by construction: `kernel/`, each `tools/<name>/`, and
  `shared/` are separate modules that cannot reach into each other's `internal/`.

One refinement over the raw seed: do NOT keep the seed's single `libs/` bucket — that is itself the
flat folder A3 warns against. Split it into three owner-labelled modules: `contract/` (the wire),
`shared/` (admission-gated vocabulary), and the `kernel/` module (the substrate). "libs" as one folder
would mix the wire, the clock port, and the transport in one place — the exact mixing A3 forbids.

Staging (honor the base-plan, do not big-bang 8 modules on day one): start with `legacy` (today's
module, unchanged) + one `clean` module + the wall check (base-plan §4A). Carve the segments above as
directories inside `clean` first, then PROMOTE each to its own module at the moment it gains a
subprocess binary and a wire adapter — which A1 forces early for the live-reload story. The end state
is the multi-module tree in §1; the path there is promotion, not a rename storm.
