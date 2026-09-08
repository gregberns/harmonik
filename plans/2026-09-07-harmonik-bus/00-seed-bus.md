# Harmonik Bus

The composition seam. How self-contained tools, and later agents, coordinate
without importing each other — in-process today, over a network tomorrow, with
**no change to the tool code.**

See `harmonik-restructure.md` for the surrounding layout (libs → tools → composition
root). This doc specifies the bus itself and the plugin contract tools use to
attach to it.

---

## The idea in one sentence

> A tool depends only on the **`Bus` interface** and exposes a **`Service`**;
> whether it's wired to an in-memory bus (embedded) or a NATS bus (distributed)
> is decided in `main`, not baked into the tool.

The bus is the pipe. Subjects are the addresses. Tools and agents are the
processes. It's UNIX pipes, generalized to a network.

---

## The two interfaces

Both live in `libs/transport`. They are the entire public contract.

### `Bus` — the transport

```go
// libs/transport
type Bus interface {
    // fire-and-forget, one-to-many
    Publish(ctx context.Context, subject string, msg []byte) error

    // register interest; handler runs per delivered message
    Subscribe(ctx context.Context, subject string, h Handler) (Subscription, error)

    // request/reply, one-to-one, returns the reply payload
    Request(ctx context.Context, subject string, msg []byte) ([]byte, error)
}

type Handler func(ctx context.Context, m Message) error

type Message struct {
    Subject string
    Reply   string            // set on requests; publish here to answer
    Data    []byte
    Header  map[string]string // correlation id, content-type, trace, …
}

type Subscription interface{ Unsubscribe() error }
```

Three verbs cover everything: **Publish** (events), **Subscribe** (listen),
**Request** (ask and wait). Anything richer (streaming, queue groups) is added as
optional interfaces a concrete bus *may* also satisfy — never forced on callers.

### `Service` — the plugin contract

What it means for a tool to attach to a bus. **This is the plugin system** — not
`.so` files (see the steer below).

```go
type Service interface {
    Name() string                                        // stable identity, e.g. "keeper"
    Subjects() []string                                  // what it listens on
    Handle(ctx context.Context, m Message) ([]byte, error) // reply payload, or error
}
```

A tool implements `Service`. A small helper mounts it on any `Bus`:

```go
func Mount(ctx context.Context, b Bus, s Service) (Subscription, error) {
    // subscribe s.Handle to each of s.Subjects(); on a request, publish the
    // returned payload to m.Reply. One place, works for every service.
}
```

That's the whole extension model. A new capability = a new `Service`.

---

## Subjects: the addressing scheme

Hierarchical, dotted, stable. Treat them as your public API — version them, don't
break them casually.

```
keeper.session.restart          # command: restart a session
queue.task.enqueue              # command: push a task
task.exec.run                   # command: run a task through the pipeline
task.exec.completed             # event: a task finished  (fan-out)
keeper.session.grew             # event: context crossed the threshold
```

Convention: `<tool>.<noun>.<verb>` for commands (used with `Request`),
`<tool>.<noun>.<pastTense>` for events (used with `Publish`). Wildcards
(`task.exec.*`) let an observer subscribe to a whole family — how agents and
monitors watch without coupling to a specific tool.

---

## Two composition modes, one tool

The payoff: the same tool runs both ways because it only ever sees the `Bus`
interface.

### Embedded — in-memory bus (one binary)

`cmd/harmonik` constructs an in-memory bus and mounts every tool's `Service` on
it. "Messages" are function calls routed in RAM — no serialization cost worth
worrying about, no network. One process, tools coordinate over the bus abstraction.

```go
bus := transport.InMem()
transport.Mount(ctx, bus, keeper.NewService(...))
transport.Mount(ctx, bus, queue.NewService(...))
transport.Mount(ctx, bus, taskexec.NewService(...))
```

### Distributed — NATS bus (many processes)

Each tool is its own binary. It connects to the real transport and mounts the
**same** `Service`. The umbrella need not import it at all.

```go
bus, _ := transport.DialNATS(ctx, os.Getenv("HARMONIK_BUS"))
transport.Mount(ctx, bus, keeper.NewService(...))
// process blocks, serving keeper.session.* off the wire
```

**The tool's `Service` code is byte-for-byte identical in both.** Embedded vs
networked is the `Bus` you inject — a wiring decision in `main`.

> Start embedded. It's fully type-checked, needs no infrastructure, and ships
> today. Moving a tool out-of-process later is a wiring change, not a rewrite —
> which is exactly why the seam is an interface.

---

## Registration: compile-time vs runtime

Two ways a service becomes known to the system. Same `Service` interface; pick per
tool, or offer both.

**Compile-time (embedded).** Discovery = "which tool packages did `cmd/harmonik`
import and `Mount`." Fully static, type-checked, no dynamic loading. This is the
default and what you get first.

**Runtime (distributed).** A tool process starts, connects, and announces itself
on a well-known subject:

```
_harmonik.register   →  {name, subjects, version, pid}
```

The umbrella (or a directory service) subscribes to `_harmonik.register`, learns
what's out there, and can route to it — **without importing it**. This is the real
extensibility story: a new tool, or an agent, is just a new process that speaks
the protocol. No recompile of harmonik to add one.

A tiny registry lib tracks live services, handles heartbeats, and expires the
dead — but it's ordinary code subscribed to ordinary subjects, not a special
mechanism.

---

## Agents are just services

An agent that wants to participate implements the same `Service` (or just uses
`Request`/`Subscribe` as a client). It requests `task.exec.run`, subscribes to
`task.exec.completed`, publishes its own events. To the bus there is no difference
between "a tool" and "an agent" — both are participants addressed by subject. That
uniformity is the point of putting the transport at the center.

---

## Testing

The in-memory bus **is** the test harness. No mocks of the transport needed.

```go
bus := transport.InMem()
transport.Mount(ctx, bus, keeper.NewService(...))

reply, err := bus.Request(ctx, "keeper.session.restart", mustJSON(req))
// assert on reply
```

For a tool in isolation, inject `transport.InMem()` into its `cli.Env` (see
`harmonik-restructure.md`) and drive it through `Run`. Cross-tool interaction
tests mount two real services on one in-memory bus and let them talk — still one
process, still milliseconds.

---

## Important Go steer: do NOT use the `plugin` package

Go's `plugin` package (`.so` loading) is a trap. It requires the exact same
compiler and dependency versions for host and plugin, has no Windows support, and
makes versioning miserable. The word "plugin" lures people into it.

**Harmonik's plugin system is the `Service` + `Bus` pair, not dynamic libraries.**
Extensibility comes from *either* importing a tool package at compile time *or*
running it as a separate process that speaks the protocol — both statically
compiled, both robust. Never `plugin.Open`.

---

## What belongs in `libs/transport` vs a tool

- **`libs/transport`**: the `Bus` and `Service` interfaces, `Message`, `Mount`,
  the in-memory bus, the NATS bus, the registry. Zero tool-specific knowledge.
- **A tool** (e.g. `tools/keeper`): its domain logic (a plain library), plus a
  thin `NewService()` adapter that maps its subjects to method calls. The adapter
  is the only part that knows about the bus; the core logic doesn't import
  `transport` at all — it's called *by* the adapter.

That keeps the domain logic pure and independently testable, and the bus concern
isolated to a small, obvious seam.

---

## Payload format

Start with JSON — debuggable, no schema tooling, good enough. Put a
`content-type` in `Message.Header` so a subject can carry protobuf/msgpack later
without breaking callers. Don't over-engineer this before the transport is
carrying real traffic.

---

## Summary

- **`Bus`** = Publish / Subscribe / Request. Three verbs, one interface.
- **`Service`** = Name / Subjects / Handle. The plugin contract.
- **`Mount`** attaches any Service to any Bus.
- **In-memory bus** for embedded (one binary) and for tests; **NATS bus** for
  distributed — the tool doesn't know which.
- **Registration** is compile-time (import + Mount) or runtime (announce on
  `_harmonik.register`).
- **Never** the Go `plugin` package.
