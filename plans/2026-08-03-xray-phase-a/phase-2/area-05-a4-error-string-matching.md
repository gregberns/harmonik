# Area 5 — A4: control flow that depends on the text of an error message

**Class:** A4. **Phase:** A. **Detector:** `exact`. **Lock unit:** file. **14 findings, 8 files — the whole
set is below.** Data: `data/area-05-a4-error-strings.json`.

The best first task in this directory for an agent that has not worked this codebase before: closed set,
exact detection, no ambiguity bucket, and a fix with a named idiom in the standard library.

## The set

```
cmd/harmonik/comms.go:1851
cmd/harmonik/comms.go:2115
cmd/harmonik/decisions.go:462
cmd/harmonik/smoke.go:608
cmd/harmonik/subscribe.go:264
cmd/harmonik/subscribe.go:274
cmd/harmonik/subscribe.go:398
cmd/harmonik/subscribe.go:498
internal/brcli/terminaltransition_bi010.go:348
internal/lifecycle/tmux/osadapter.go:671
internal/lifecycle/tmux/osadapter.go:672
internal/workflow/loader.go:90
internal/workflow/loader.go:153
internal/workflow/loader.go:209
```

Four files carry 12 of the 14: `cmd/harmonik/subscribe.go` (4), `internal/workflow/loader.go` (3),
`cmd/harmonik/comms.go` (2), `internal/lifecycle/tmux/osadapter.go` (2).

## Why it blocks decomposition

This is A1's disease one level up. A contract between the code that raises an error and the code that
handles it, maintained by convention, invisible to the compiler, and **silently broken by rewording a
message**. Neither side can be moved safely because nothing tells you the other side exists — no call edge,
no type edge, nothing in the graph.

The failure mode is worse than A1's: an A1 mistake is usually a build error, whereas rewording an error
string produces code that compiles, passes the tests that do not exercise that branch, and takes the wrong
path in production.

## The fix

Per site, one of two standard idioms:

- **Sentinel** — `var ErrX = errors.New("...")` at the raiser, `errors.Is(err, ErrX)` at the handler. Use
  when the condition carries no data.
- **Typed error** — a struct implementing `error`, matched with `errors.As`. Use when the handler needs a
  field out of the error (a path, an exit code, a name).

**The judgement is at the raiser, not the matcher.** Three of these almost certainly match errors raised by
the standard library or by an external process, not by harmonik:

- `internal/lifecycle/tmux/osadapter.go:671-672` — two adjacent matches, very likely against `tmux(1)`
  stderr text. There is no sentinel to reach for: tmux's messages are the API. The right fix is a **parser
  at the boundary** that turns tmux's output into a harmonik sentinel *once*, in the adapter, so exactly one
  place in the repo knows tmux's wording. That is a genuinely valuable change and it is not a
  find-and-replace.
- Any match against `os` / `fs` errors should become `errors.Is(err, fs.ErrNotExist)` and friends — the
  standard library already publishes sentinels and string-matching them is pure loss.

So: read each site, classify it as *harmonik raises this* (sentinel — free), *stdlib raises this*
(use the published sentinel — free), or *an external process raises this* (boundary parser — a design
task). Report the classification even for the sites you do not fix.

## Gate

Not a no-op — control flow changes shape — so the byte-identical trick does not apply.

```sh
go test -count=1 ./cmd/harmonik/... ./internal/workflow/... ./internal/brcli/... ./internal/lifecycle/tmux/...
```

Confirm green **before** editing; `cmd/harmonik` is large and some of harmonik's ~30 baseline failures live
in that neighbourhood. If a package is red at baseline, that package's sites need a narrower gate — the
specific test that covers the branch — or they need to be deferred, not fixed blind.

Exit metric: no `strings.Contains(err.Error(), …)` or relative remains at the site, and re-running
`refactor/cmd/detect` reports fewer than 14 A4 findings by exactly the number fixed.

## Worth knowing

`cmd/harmonik/comms.go` appears here (2), in area 3 (49 A1 findings), and in area 7 (14 B1/B2 findings) —
**the most finding-dense file in harmonik across every class**. It is a strong candidate for a
single-file, multi-class deep pass by one agent, which is the opposite of how the wave protocol says to
work (one class per wave). The protocol is right about attribution and this file is the case that tests it.
If someone does try a multi-class pass, commit each class separately so a regression is still attributable.
