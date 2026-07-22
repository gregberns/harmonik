# E2b — what you are being asked to sign off on

**Status: DECIDED 2026-07-22 — DEFERRED (option A).** The operator deferred E2b and directed that the
E5 RT slice stream be staffed instead. `crewstart.go` stays in `internal/daemon` as sanctioned residue.
No waiver was granted; `_plan.md` §1's no-new-seam rule remains in force for every unit. Revisit only
after E5's substrate/ports work matures, at which point the required seam may already exist and no
waiver will be needed.

Original decision memo follows, unchanged, as the record of what was weighed.

---

**Status when written:** awaiting operator decision. Nothing has been done. E2a (the portable half) landed as
`12663fac`; E2b is the residue.

---

## 1. What E2b is

`internal/daemon/crewstart.go` — 712 lines — is the crew *handler*: the thing that receives a
"start a crew" RPC and does the work. It holds `crewHandlerImpl`, `HandleCrewStart`, queue conflict
checks, mission pasting, and the post-spawn keeper liveness probe. Plus ~1,613 lines of tests.

E2a already moved the *contract* half out (launch spec, harness resolver, mission front-matter, idle
reaper → `internal/crewrun`). E2b would move the handler itself.

## 2. Why it is blocked — a real compile error, not a style objection

I verified this in the code rather than taking the plan's word for it.

`internal/daemon/crewstart.go:533`:

```go
sa, ok := h.substrate.(substrateWithAdapter)
```

`internal/daemon/tmuxsubstrate.go:1364`:

```go
type substrateWithAdapter interface {
	tmuxAdapter() tmux.Adapter        // <-- lowercase: UNEXPORTED
}

func (s *tmuxSubstrate) tmuxAdapter() tmux.Adapter { return s.adapter }
```

**In Go, an unexported method name is qualified by the package that declares it.** An interface
declared in `internal/crewrun` with a method `tmuxAdapter()` requires a method named
`crewrun.tmuxAdapter`. `*tmuxSubstrate` has `daemon.tmuxAdapter`. Those are different method names, so
the interface **can never be satisfied** from outside `internal/daemon`, and the type assertion cannot
even be written there.

So this is not "awkward to move." The code physically cannot compile in another package as written.

## 3. What removing the blocker requires

Breaking that dependency means introducing a new abstraction — the plan calls it a `crewSpawner` port —
that `crewrun` defines and `daemon` implements, so the handler stops reaching for the concrete tmux
substrate.

**That is inventing a new seam, and it is exactly what P2's own rules forbid:**

- `_plan.md` §1: the goal is *"moving implementations out of the two god packages behind seams that
  **ALREADY EXIST** — not by inventing new seams."*
- `_plan.md` §5.1 makes it a review-gate **rejection**: *"extraction must NOT invent a new seam."*

This rule has been load-bearing, not decorative. Every one of the nine landed slices exited behind a
pre-existing seam, and the `internal/gitprobe` package doc explicitly cites this rule as its reason for
duplicating a helper rather than injecting one. Waiving it for E2b weakens a constraint that has so far
kept ten commits honest.

## 4. What you would actually be approving

**Not** "let the agent move some files." You would be approving:

1. **A new port that does not exist today** (`crewSpawner`), defined in `crewrun`, implemented in
   `daemon`. This is new design, not code motion.
2. **A change that is not behavior-preserving by construction.** Every other P2 slice could be verified
   as a pure move — the diff was mechanically checkable and eight of eight came back
   `is_pure_move: true`. E2b cannot be verified that way, because it restructures. The safety net that
   caught problems in every prior slice does not apply here.
3. **Follow-on prep work** the plan already identifies: narrowing `KeeperConfig` (a 40+-field struct of
   which `crewstart.go` reads exactly one field) to a `time.Duration`, narrowing
   `map[string]CrewConfig` → `map[string]string`, and re-declaring six interfaces
   (`pasteInjecter`, `enterSender`, `paneCaptureAdapter`, `paneTargeter`, `crewSessionSpawner`,
   `crewSessionStopper`).
4. **One thing that cannot be duplicated at all:** `errPaneCaptureUnsupported`
   (`pasteinject.go:224`) is a sentinel that `injectAndVerifySeed` does `errors.Is()` against. A copy
   in another package is a *different* sentinel and `errors.Is` silently returns false. It must stay in
   the same package as the verify loop — which means `pasteinject.go` and `crewstart.go` are coupled,
   and `pasteinject.go` is assigned to **E5**, not E2.

Point 4 is the one I would weigh most heavily: E2b is entangled with E5's territory, so doing it now
means doing part of E5 early, out of order, in the hottest file region.

## 5. Options

| Option | What happens | My read |
|---|---|---|
| **A. Defer E2b** (recommended) | `crewstart.go` stays in daemon as sanctioned residue. E2a's 616 LOC already landed. Revisit after E5's ports mature, when the substrate seam may already exist and no waiver is needed. | The blocker is partly an artifact of E5 not having happened yet. Waiting may make the waiver unnecessary rather than merely delayed. |
| **B. Waive the rule, do E2b now** | You accept a new seam, a non-pure-move slice, and early entanglement with E5/`pasteinject.go`. | Buys ~712 non-test LOC out of daemon (~1.4% more shrink) at the cost of the constraint that has kept this stream verifiable. |
| **C. Waive narrowly** | Approve *only* the `crewSpawner` port, explicitly scoped, with the rule intact for every other unit. | Viable if you want E2b, but I would still sequence it after E5's substrate work to avoid the `pasteinject.go` collision. |

**Recommendation: A (defer).** Not because E2b is unimportant, but because the thing blocking it is
scheduled to be dismantled by E5 anyway. Paying for a new seam now, in E5's hottest file, to save
waiting is the worse trade. If you want E2b regardless, take **C**, not B — keep the waiver scoped to
one named port so the rule survives for the rest of the stream.

## 6. If you choose B or C, say this

> "Waive `_plan.md` §1's no-new-seam rule for unit E2b only, for the `crewSpawner` port. E2b is not
> required to pass the pure-move gate."

That is the precise sentence the review gate needs; without it an `agent-reviewer` pass on E2b will
return `unwanted-abstraction` and block the commit, correctly.
