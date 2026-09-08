# Harmonik Restructure Playbook

How to pull the good code out of the tangle into a place that is *provably* clean,
then port the rest across one testable piece at a time — without a big-bang rewrite,
and without ever having to rebuild/retest the whole system to land a change.

This is the **strangler fig** pattern, done the Go way.

---

## The two goals (keep them separate)

These get conflated. They are won with different tools:

1. **"A change should only rebuild/retest what it touches."**
   Won by **small packages + consumer-defined interfaces**. This shrinks the
   compile graph so a change has a small blast radius.

2. **"Independent, composable tools + libraries, reassembled into one binary."**
   Won by **module boundaries + `go.work`**. This is about enforced isolation
   and independent versioning — *not* build speed on its own.

Do #1 and you get most of the verification-speed win. Do #2 on top and you get
composability and hard walls. Don't expect modules alone to make anything faster.

---

## The Go facts this all rests on

- **The package is the atom.** Go compiles and test-caches a package as a single
  unit — all its files at once. A change in one file recompiles the whole package
  *and every package downstream of it*, and invalidates their test cache. The
  granularity of "what must rebuild" is decided entirely by how you draw package
  lines. Small, single-purpose packages = small blast radius.

- **Go's caches are already incremental.** `go test ./...` prints `(cached)` for
  anything whose package + dependencies are byte-for-byte unchanged. Slow
  verification is not "Go is slow" — it's "too much depends on one big thing, so
  nothing is ever unchanged." Fix the shape; the caching you already have does the rest.

- **Interfaces are satisfied implicitly, and you define them at the consumer.**
  The package that *uses* a capability declares a tiny interface for exactly what
  it needs; the provider never names it. That severs the import edge back to the
  implementation — the seam that keeps one tool's change from rippling into another,
  and lets each be tested against a trivial fake.

  ```go
  // consumer package declares only what it needs
  type Store interface {
      Get(id string) (Item, error)
  }
  ```

- **Wire concretes in exactly one place: `main`.** Constructor injection, plain
  function calls. No DI framework — idiomatic Go doesn't use one. Every package
  below `main` stays ignorant of its siblings, so each is independently buildable
  and testable.

  > Rule of thumb: **accept interfaces, return concrete types.**

---

## Step 0 — Build the clean room as a separate module

Not a folder in the existing module. A **real module** with its own `go.mod`,
tied to the old one with a `go.work`.

```
harmonik/
  go.work            # use ./legacy and ./clean
  legacy/  go.mod    # the existing tangle, untouched
  clean/   go.mod    # empty. this is "the good code" from now on
```

Why a separate module and not just a `clean/` package: **modules give a
compiler-enforced boundary.** That is what turns "I intend this to stay clean"
into "this *cannot* become dirty."

---

## The invariant that makes it "known good": a one-way wall

> **`clean` may never import `legacy`. `legacy` may import `clean`.**

That direction is everything:

- Nothing from the tangle can leak *into* the good zone — the good zone literally
  does not know the tangle exists.
- The tangle gets rewired to call the good code, so as `clean` grows, `legacy`
  shrinks. New strangles old, and both compile the entire time.

**Enforce it so it isn't up to discipline.** A CI check is enough:

```bash
# fails the build if the clean module ever imports legacy
cd clean && go list -deps ./... | grep 'harmonik/legacy' && exit 1 || exit 0
```

Now `clean` is *provably* good: it compiles on its own, tests on its own, and by
construction cannot depend on anything unvetted. That is the guarantee.

---

## The port loop (repeat until `legacy` is empty)

For each piece of good functionality still trapped in `legacy`:

### 1. Move it, don't copy it. Preserve history.
```bash
git mv legacy/foo clean/foo
```
Rewrite its import path (module prefix `harmonik/legacy/foo` → `harmonik/clean/foo`)
and run `goimports -w`. Its tests move with it and immediately run in the clean
module's fast suite.

### 2. Fix what the wall now rejects.
The moved code probably still reaches back into `legacy` for something not yet
ported. Two choices at that seam:
- If the thing it reaches for is small and good — **pull it along too.**
- If it's still-messy or not-yet-ready — **invert it into an interface** the clean
  package declares and the legacy side satisfies for now:
  ```go
  // clean/foo declares only what it needs
  type Sink interface{ Write(Record) error }
  ```
  `clean/foo` now depends on a behavior, not on `legacy`. The wall is satisfied.

### 3. Keep old callers working with an adapter.
Legacy code that used to call the old implementation now calls the ported one.
Wrap the new clean API in a shim that satisfies whatever interface legacy expects:
```go
// legacy/shim.go — throwaway bridge, deleted later
type fooShim struct{ impl *clean.Foo }
func (s fooShim) OldUglyMethod(x LegacyType) { s.impl.Do(convert(x)) }
```
Legacy keeps running; it just runs *on top of* the good code now. The shim's
existence marks a caller you haven't finished porting.

### 4. Verify the seam, not the world.
The moved package brought its tests and lives in an isolated module:
```bash
cd clean && go test ./foo/...   # seconds, not minutes
```
The legacy build proves the shim wired up. You never re-verify the whole system
to land one port.

### 5. Rewrite in place, safely.
Now `foo` is in the clean room with its own tests and no inbound tangle. Restructure
it freely — split it into concern-sized packages, add consumer-side interfaces at
its seams. The wall guarantees you can't accidentally re-couple it to garbage while
you clean it up.

---

## Finishing

Each port shrinks `legacy` and deletes a shim. When `legacy` has nothing left but
shims, delete the module, drop it from `go.work`, and `clean` becomes the whole
project.

At that point, promote `clean`'s standalone pieces (keeper, task processing, queue,
…) into their own `go.work` modules — the target architecture below.

---

## Target architecture (the end state)

The goal is UNIX-style tools: self-contained, testable, each its own binary — then
**composed** into the larger harmonik. Three layers, one-way dependencies.

```
harmonik/
  go.work
  libs/                       # shared, importable by anyone
    transport/  go.mod        # the bus: interface + in-mem & NATS impls
    cli/        go.mod        # the Env + Run harness every tool shares
  tools/
    keeper/     go.mod        # library logic + a thin main + a bus adapter
    queue/      go.mod
    taskexec/   go.mod
  cmd/
    harmonik/   go.mod        # umbrella: imports the tools, composes them
```

- **`libs/`** — shared vocabulary. No process concerns, no `os.Exit`, no global
  flags. Types, interfaces, and the transport. Anyone imports them.
- **`tools/`** — self-contained tools. Each is a *library* + a *thin main* + a
  *bus adapter*. Independently built, independently tested.
- **`cmd/harmonik`** — the composition root. Imports the tool libraries and wires
  them. Almost no logic of its own. This is the fix for "cmd holds everything."

Dependency direction: `tools/*` and `cmd/*` may import `libs/*`; **nothing in
`libs/*` imports a tool.** One-way, enforced (same wall check as above).

### One entrypoint → both standalone binary and embedded subcommand

Every tool exposes a single function with a UNIX shape — explicit in, explicit
out, an exit code — but *injected* so it's a pure, testable call:

```go
// libs/cli
type Env struct {
    Args   []string
    In     io.Reader          // stdin
    Out    io.Writer          // stdout
    Err    io.Writer          // stderr
    Getenv func(string) string
    Bus    transport.Bus      // the composition seam — see harmonik-bus.md
}

// package keeper
func Run(ctx context.Context, env cli.Env) error
```

Both binaries call the **same** `keeper.Run`:

```go
// tools/keeper/cmd/keeper/main.go  — standalone binary
func main() { os.Exit(cli.Wrap(keeper.Run)) }

// cmd/harmonik/main.go  — embedded as a subcommand
root := cli.NewRoot()
root.Add("keeper", keeper.Run)
root.Add("queue",  queue.Run)
root.Add("task",   taskexec.Run)
os.Exit(root.Main(ctx, cli.OSEnv()))   // `harmonik keeper ...` → keeper.Run
```

- `go build ./tools/keeper/cmd/keeper` → a standalone `keeper` binary.
- `go build ./cmd/harmonik` → the whole system with `keeper` as a subcommand.

Same logic, zero duplication. **The rule that makes this work: logic lives in
libraries; binaries are thin wrappers.** A binary is just an entrypoint that calls
a library function; two entrypoints calling the same function give you both forms
for free.

### Testability comes free

Never spawn a process to test a tool — call `Run` with buffers and assert:

```go
var out bytes.Buffer
err := keeper.Run(ctx, cli.Env{
    Args: []string{"--once"},
    Out:  &out,
    Bus:  transport.InMem(),
})
```

### Composition seam

Tools coordinate through an injected `transport.Bus`, never by importing each
other. Embedded, the umbrella hands every tool an in-memory bus (function calls in
RAM); distributed, each tool gets a NATS bus over the wire — **the tool code is
identical**; embedded-vs-networked is a wiring decision made in `main`. The bus
and its plugin contract are specified in **`harmonik-bus.md`**.

---

## One honest caveat: slow integration tests

Some verification time is **integration tests that spin up daemons/sessions** —
slow no matter how packages are sliced. Decomposition doesn't speed those up; it
**quarantines** them. Tag them so they're excluded from the fast per-package loop
and run only at the assembly stage:

```go
//go:build integration
```

Decomposition makes the fast loop fast; build tags keep the slow tests out of it.

---

## The mental model

The good code doesn't get "protected" — it gets **physically relocated behind a
compiler-enforced one-way wall**, and everything else is dragged across one
testable package at a time, on adapters, while both halves keep compiling. No
big-bang cutover. "The good zone" is trustworthy because *the wall* — not your
vigilance — keeps it clean.

---

## Sequencing, if you want a starting order

1. Stand up `clean/` + `go.work` + the CI wall check. (An afternoon.)
2. Move the *one* cleanest, most self-contained package across. Watch the loop turn once.
3. Repeat, picking the next piece by how few legacy tentacles it has.
4. When a ported piece is stable and standalone, promote it to its own module.
