# Code-Revamp — Consolidated Decision Digest

> Single confirm-and-go sheet from the forward-planning fan-out (2026-07-13, admiral, autonomous).
> Merges PLAN §8 open decisions + the reconciliation calls + new findings, all Fable-verified.
> Signoffs are WAIVED, so I've **decided the reversible ones autonomously** (Group B) — flag any you'd
> reverse. Only **Group C** genuinely wants your eyes. Evidence: `PLANNING-LOG.md`, `reconciliation.md`.

## ✅ OPERATOR VERDICT (2026-07-13)
**All decisions approved** — Group B stands, Group C **C1/C2/C3 all APPROVED** on the recommended defaults.
Directives: **do NOT file beads** — create the task definitions only (see `TASKS.md`). **Track C is executing this session.**

## Group A — already RESOLVED (no call needed; noted for the record)
| # | Question (PLAN §8) | Resolution | Why it's closed |
|---|---|---|---|
| §8.2 | Substrate genericization: generics vs `any` | **Generics — landed.** | P1 shipped `internal/substrate` with `EventSource[E]`/`Effector[A]`/`Run[E,A]`; `go test ./internal/substrate/` green. Verified. |
| §8.3 | Dead typed-decode path: adopt vs delete | **ADOPT — landed.** | `internal/replay` consumes it; bench D6 `[OPERATOR-LEAN CONFIRMED]` + EV-048 normative at `specs/event-model.md:720`. Do NOT delete. |
| §8.1 | Confirm session-restart as first vertical (SR4 headline) | **Confirmed by execution.** | P1 is on T7 building exactly this; SR9→SK-INV-005 landed in `specs/session-keeper.md:264`. |

## Group B — decided autonomously (reversible; flag to override)
| # | Decision | What I chose (default) | Reversal cost |
|---|---|---|---|
| B1 | `subsystem-proofs → M5` mapping | **Struck** — naming collision; M5 stays net-new (subsystem-proofs is DONE test-lanes). ROADMAP corrected. | trivial (doc) |
| B2 | `quality-system` role | **= acceptance ORACLE / DOGFOOD gate**, not a Track C source. Reference, don't rebuild. Track C = direct config. | trivial (doc) |
| B3 | Enforcement: ratchet vs cleanup (§8.5) | **Ratchet** — and the ratchet infra already exists (`--new-from-rev` + coverage-gate.sh). Grandfather legacy, cap new code. | n/a |
| B4 | census PLAN disposition (§8.6) | **Keep both** — census = diagnosis, code-revamp = the how. No supersede. | trivial |
| B5 | `testing-strategy-uplift` (stalled, 0 beads) | **Supersede** — harvest coverage-gate→Track C, 5-layer taxonomy→M1. | low |
| B6 | M2/M3 design-pass timing | **Hold at `decompose`** until P1 proves the reactor seam; then mirror the proven keeper template. | n/a (staging) |

## Group C — genuinely wants the operator
| # | Call | Recommended default | Why it needs you |
|---|---|---|---|
| **C1** | **Implement Track C now?** (enable complexity linters + coverage-carve + depguard `daemon` ceiling + fix the queue↔uuid gap) | **YES — do it now, out-of-pipeline.** Auto-grandfathers all legacy; catches P1's own output going forward. Ready-to-apply diff in `track-c-enforcement.md`. | It changes the **repo-wide CI merge gate** for every contributor. Reversible (revert the `.golangci.yml` diff) but broad — worth your explicit go before I apply. |
| **C2** | **M4 seam: reverse `remote-substrate-phase2`'s DEC-A?** Phase2 *rejected* `handler.Substrate` for a `CommandRunner`-only seam; the revamp uses the proven substrate seam. | **Adopt the proven substrate seam for M4**; treat phase2 DEC-A as superseded. | This reverses a **documented design decision** inside an existing work — I don't auto-reverse prior decisions. |
| **C3** | **M4 scope: split container-isolation/egress OUT of M4?** Phase2 bundled it; it's out of the remote-rebuild scope. | **YES — M4 = remote rebuild only; container/egress → its own later work.** | Scope boundary / product-direction call on a future phase. |

## FYI (not a blocking call)
- **`validation-net` is hollow:** 12 of its 13 spec'd VN beads (incl. flagship VN4 `hk-ukhzu`) are **absent from the `br` DB** — rebasing it means RE-FILING beads, not re-labeling. Track C's config work does **not** depend on those beads; re-file only the protective-net scenarios you still want.
- **Two latent bugs found in passing** (now scoped as beads B1/B2 in `track-b-m1.md`): queue.json two-writer lost-update (`rpc.go:1006-1016`), and the git-log-`--grep` false-close (`orphansweepbeads.go:230`). Both bead-sized, out-of-pipeline, ready to file.
- **M1 test-theater:** only `specaudit` is safe-relocate (with a 3-file product-import carve-out); `operatornfr` + `scenario` keep/prune calls need your sign-off before any bulk delete (census Q4).
