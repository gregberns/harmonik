# R5 — CE5 / hk-y7v8 review: `--wake` reaches the captain's bare session

**Bead:** hk-y7v8 (CE5) — fix `comms send --to captain --wake` pane mismatch.
**Reviewed commit:** `5d604109` (branch `worktree-agent-ab71056b941a6c655`), parent `530773c3`.
**Main:** `d47ebab4`.
**Verdict: APPROVE-WITH-NITS.** No blocking defects. Crew wake is not regressed; mis-target risk is negligible.

---

## 1. Crew-wake NOT regressed (key risk) — PASS

`commsWakePaneCandidates` (cmd/harmonik/comms.go:376-388) builds the list strictly
ordered: (1) crew-registry `Handle+".0"` (only if a record loads with a non-empty
Handle), then unconditionally `append`s (2) `...-crew-<name>` and (3) bare
`...-<name>`. The bare candidate is **appended after** the crew candidates, never
substituted. Verified against the committed source (`git show 5d604109:...`), not
just the working tree.

- Registered crew → handle pane is `candidates[0]`, tried first.
- Unregistered crew → crew-convention pane is candidate (1) of the remaining two,
  tried before the bare one.
- Captain (no record, bare session) → falls through to candidate (3).

`commsWakePaneForAgent` (comms.go:401-414) iterates and returns on the first
candidate `commsInjectTmuxPane` accepts. Crew panes always precede the bare pane,
so a crew never resolves to the bare candidate first. Trace confirms the contract.

## 2. Correctness of the captain bare candidate — PASS (with a NIT)

Candidate (3) = `lifecycle.TmuxSessionName(hash, "captain")` = `harmonik-<hash>-captain`,
which exactly matches captain-launch.sh:53 `CAP_TMUX=harmonik-${PROJ_HASH}-captain`.
Correct.

**NIT (LOW, pre-existing, not introduced):** the hash source differs in *symlink
resolution*. The wake call site (comms.go:244) derives `projectDir` via
`filepath.Abs` only, while captain-launch.sh's session name uses `harmonik
project-hash`, which applies `filepath.EvalSymlinks` (project_hash_cmd.go:76).
`ComputeProjectHash` hashes the raw string, so a symlinked project path yields
divergent hashes and the bare candidate would miss. This is NOT new in this diff —
the old code computed the crew-convention candidate from the identical `absProject`,
and crews work in practice — so it does not block CE5. Worth a follow-up bead: have
`comms` EvalSymlinks before hashing, for parity with session naming.

## 3. Mis-target risk (try-each-until-success) — PASS

Negligible. The candidate strings are distinct (`...-crew-captain` vs
`...-captain`), so they never collide for one agent. A crew literally named
"captain" would generate `...-crew-captain` (candidate 2) before `...-captain`
(candidate 3) — the crew's own pane is still hit first; only if that crew session
were absent would the loop reach the captain pane, which is the desired fallback,
not a mis-target. Hash collisions are out of scope (12-hex SHA-256; same risk as
all existing session naming). The "nudge the wrong live pane" scenario requires two
of the THREE candidates to simultaneously name distinct live panes for the same
agent, which the naming scheme precludes. No flag.

## 4. Test adequacy — PASS

`comms_wake_pane_hky7v8_test.go` covers all three branches. Assertions are
**ordering-aware**, not mere presence:
- captain: asserts `got[len(got)-1] == wantBare` — the bare session is the *final*
  candidate (load-bearing), and tolerates the crew variant as an earlier miss.
- registered crew: asserts `got[0] == Handle+".0"` — handle pane is *first*.
- unregistered crew: asserts the crew-convention pane is present.
Test compiles and PASSES on the branch (`go test -run TestCommsWakePaneCandidates`).
Full `Comms` suite green; `go vet` clean. Minor gap (non-blocking): no explicit
assertion that for a registered crew the bare candidate comes *after* both crew
candidates — but the ordering is structurally guaranteed by the append order and
the captain test pins the tail.

## 5. Scope + sync — PASS

The bead commit `5d604109` touches exactly 4 files: comms.go, the test, and BOTH
SKILL.md copies. Mirror is **byte-identical** (`diff` → IDENTICAL). The SKILL.md
addition is an accurate, in-scope doc note (re-arm `comms recv --follow`; correctly
states `--wake` now targets the bare session but remains best-effort).

**Stale-base note (not a defect):** a raw `git diff d47ebab4..5d604109` ALSO shows
captain-launch.sh CE6 work as removed. This is a **stale-base artifact**: the branch
parent `530773c3` is an ancestor of main but predates the CE6 (hk-9mpk)
captain-launch.sh merge. The bead's own commit does NOT touch captain-launch.sh.
**Action for the merger:** land via cherry-pick of `5d604109` or a 3-way merge onto
current main — do NOT replay the raw two-dot diff, or it would revert CE6's
verified-restart wiring.

**Pre-existing TUI failure confirmed:** "open terminal failed" is a headless/PTY
runtime artifact from a TTY-dependent test; the string appears in no touched file
and this diff opens no terminal. Unrelated to the change.

---

### Summary
APPROVE-WITH-NITS. Crew wake correctly preserved (bare candidate appended, not
substituted); mis-target risk negligible (distinct candidate names, crew-first
ordering); test is meaningful and passes. Two non-blocking follow-ups: (a) EvalSymlinks
parity between `comms` hash and session-name hash; (b) merger must cherry-pick/3-way
to avoid reverting CE6 captain-launch.sh from the stale base.
