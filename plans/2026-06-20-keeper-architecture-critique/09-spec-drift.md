# 09 — Spec-vs-Code Drift Critique

**Lens:** Is there a single authoritative keeper spec, does the code match it, and does the
existence of TWO competing specs indicate a design that was never settled?

**Verdict (one line):** The keeper is governed by **no normative spec at all** — two
conflicting bench drafts, neither finalized to `specs/`, neither matching the code — so it is
maintained **by accretion**, and that ungoverned drift is a structural cause of its repeated failure.

---

## 1. There is no authoritative spec. There are two non-canonical drafts.

CLAUDE.md is explicit: *"Specs live in `specs/` at the repo root. These are normative: the
spec is always right, and code is expected to match it."* The keeper has **nothing there**:

- `ls specs/ | grep -i keep` → **no match** (exit 1). 38 specs in `specs/`; zero keeper.
- `grep -i keep specs/_registry.yaml` → **no keeper entry**.

Both keeper "specs" sit on the kerf bench and **both carry DRAFT + "finalize copies to
`specs/`" headers that were never honored**:

- `session-keeper-spec.md` line 3: *"Normative once copied to `specs/` at finalize."* Never copied.
- `keeper-identity-and-liveness.md` lines 2-4: *"On `kerf finalize`, this is copied to
  `specs/keeper-identity-and-liveness.md`. NORMATIVE."* `specs/keeper-identity*` → no match.

So per the project's own rule, **neither file is normative**. The thing that actually defines
keeper behavior is the code plus `.claude/skills/keeper/SKILL.md` — i.e. accretion, not spec.

---

## 2. The two drafts directly CONFLICT on every load-bearing axis.

| Axis | `session-keeper-spec.md` (original) | `keeper-identity-and-liveness.md` (redesign) | Conflict |
|---|---|---|---|
| **Identity model** | §4.2 / §6.1: keeper **infers/latches** the session_id from the gauge; binds "first observed id"; resolves tmux by convention. | §1-2 (I1.1–I2.6): identity is **authoritative, launch-supplied** (`--session-id <uuid4>`), written to `.managed` **exactly once**, **MUST NOT** be scraped/derived/self-healed. | **Diametrically opposed.** One latches-from-gauge; the other forbids that absolutely. |
| **Auto-clear / self-heal** | §7.2: anti-loop logic keyed on observing a **new session_id** the keeper detects; §13 "rebind" escape hatch implied. | I1.2/I1.3: **NO auto-clear**, **NO flap/latch/cooldown/suppress loop**; D1–D11 enumerate ~150 LOC to **DELETE**. | Redesign exists *specifically to delete* what the original's mechanism grew into. |
| **Warn threshold** | §10: `warn_pct` **80**, no absolute-token gate. | I4.2: **270_000** abs / 0.70 ceil. | Different model **and** different numbers. |
| **Act threshold** | §10: `act_pct` **90**. | I4.1: **300_000** abs / 0.85 ceil. | Same. |
| **Force-act** | **Absent** (no force concept). | I4.3: **340_000** / 0.95, bypasses idle gate. | Whole gate exists only in redesign. |
| **Restart mechanism** | §5: handoff→clear→resume, idle-gated on a Stop-hook marker (§4.5). | §6 keeps inject step but adds force-restart of a **live dead-gauge pane** (`maybeLivePaneRecover`→`ForceRestart`, I3.2). | Redesign adds a recovery path the original never contemplated. |
| **Liveness** | §3.3: stale gauge ⇒ "agent not live" ⇒ **do nothing**. | I3.1: stale-but-pane-alive ⇒ **derive occupancy from transcript** and refresh `.ctx`. | Original fails *open* (no action); redesign fails *active* (recover). |

A redesign that contradicts the original on identity, thresholds, auto-clear, and liveness,
where **neither is canonical**, is the textbook signature of a design that was **never settled**.

---

## 3. NEITHER spec matches the current code. The code is a third thing.

### 3a. Thresholds: code matches *neither* draft — a silent third value set.
`internal/keeper/thresholds.go:35-39` ships **warn=200_000 / act=215_000 / force=240_000**
(the "TA1 band-retune" hk-8hr1, 2026-06-17, per `git log` and `thresholds_test.go:27-59`).

- Original draft says 80/90 pct. → wrong.
- Redesign draft says 270k/300k/340k. → also wrong.
- The skill (`SKILL.md` §"two thresholds", lines 85-87) **also asserts 270k/300k/340k** — so
  the operator-facing doc is **stale by 70k tokens** against the code it claims to source from.
- Even `cycle.go:45-57` doc-comments say "default 300000 / 340000" while `thresholds.go`
  (the self-declared "SINGLE source of truth") ships 215k/240k. **In-tree drift.**

This is the cleanest possible proof of governance-by-accretion: a band was retuned by bead
hk-8hr1 and **no spec was updated**, because there is no spec to update — the redesign draft
that *defines* the band (§4 DEFAULTS-PIN, "MUST NOT move") was bypassed entirely.

### 3b. Identity: the redesign's core invariant is NOT implemented.
- Redesign I2.1: keeper "MUST accept `--session-id`." Reality: `grep '"session-id"'
  cmd/harmonik/keeper_cmd.go` → **0 hits**. There is **no `--session-id` flag.** Identity is
  still **latched from the gauge** at `watcher.go:713-721` ("latch: first valid gauge seen —
  bind its session_id into .managed") — exactly the inference the redesign forbids (I2.3).
- Redesign I1.1: `WriteManagedSessionFn` called **exactly once**. Reality: it is wired into
  the **tick loop** and called on adopt (`watcher.go:697`) and latch (`:717`) — i.e.
  potentially **many times at runtime**, the opposite of the invariant.
- Redesign I1.2: "**no auto-clear**, no foreign-session rewrite." Reality: `watcher.go:684-712`
  is a live **foreign-session / post-/clear adopt-and-rewrite branch** — a *softened* version
  of the very machinery D1–D7 ordered deleted.
- Redesign §3.2 deletion checklist (LOC must go **negative**): `isUUIDv7`/`IsUUIDv7`,
  `isUppercaseUUID`/`IsUppercaseUUID` UUID-version heuristics are **still present**
  (`keeper.go:144-160`, `watcher.go:493-502`, `heartbeat.go:161`, `cycle.go:1401`). D11
  `keeper rebind` *is* gone (good), but the identity heuristics the redesign called the root
  cause **survive**.

**Net:** the code is a *partial, uncommitted compromise* between the two drafts — it kept the
original's gauge-latching identity model, softened (not deleted) the auto-clear machinery, and
adopted neither draft's thresholds. It implements a fourth design that is written down nowhere.

### 3c. Code behavior not specced anywhere.
- `DefaultBootGracePeriod` / young-session guard (`thresholds.go:63-74`, hk-8hr1) — a
  5-minute post-resume restart suppression that is **load-bearing** under the aggressive band.
  Neither draft mentions it.
- `--respawn-cmd` / `--force-restart` supervised respawn (`keeper_cmd.go:70-71`). Original
  draft: absent. Redesign: only `ForceRestart` (different mechanism, behind a flag).
- The `.sid` single-writer channel (`watcher.go:666`, hk-8prq) — the actual identity source
  today — appears in **neither** spec; it is a post-hoc fix the redesign's authoritative-launch
  model would have made unnecessary.

---

## 4. The skill openly admits the implementation diverges from intent (red-flag catalog).

`.claude/skills/keeper/SKILL.md` is a contract that repeatedly documents its own subject as broken:

1. **§"KNOWN DRIFT — the keeper is NOT wired for crews on the live deployment"** (lines
   325-361): *"the source ships the gauge OFF by default"*; *"the live fleet ships WITHOUT
   the gauge"*; *"these two coexist… the docs disagree"*; *"whether the gauge is armed… is not
   something to assume from the docs — they disagree."* A skill that tells the reader its own
   sourced docs contradict each other is admitting there is no settled truth to document.
2. **Stale thresholds** (lines 85-87): asserts 270k/300k/340k "REAL values from code" — but
   code is 200k/215k/240k. The skill is **wrong about the numbers it claims to mirror**.
3. **§"the 27% warn… is CORRECT-BY-DESIGN"** vs. the operator's lived "persistent trouble":
   the doc has to pre-emptively defend a confusing behavior, a tell that the design surprises
   even its maintainers.
4. **Captain skill prescribes `--warn-pct 25 --act-pct 30`** (line 352) while the deployment
   docs say the gauge isn't armed — the skill catalogs **its own internal contradiction**.

A "load-bearing, must-not-rot" contract that has already rotted (stale thresholds) and that
spends a whole section reconciling docs that "disagree" is the documentation-layer symptom of
the spec-layer vacuum.

---

## 5. Verdict

**The keeper's repeated failure is, in material part, a governance failure: there is no
settled, enforced specification, so behavior changes by bead-accretion and the artifacts that
should constrain it (two drafts + one skill) have drifted out of agreement with the code and
with each other.**

Concretely, this lens found:
- **0** normative keeper specs in `specs/` despite CLAUDE.md's "specs are normative" rule.
- **2** bench drafts that contradict on identity, auto-clear, all thresholds, force-act, and liveness — neither finalized.
- **3+ disagreeing threshold value-sets** live simultaneously (code 215k; both drafts/skill 300k; cycle.go comments 300k).
- The redesign's **central fix (authoritative `--session-id`, single-write, delete-the-heuristics) is NOT implemented** — the gauge-latching identity model the redesign was written to kill is still in `watcher.go`.

The fix-of-fix history the redesign laments (D1–D11) is *itself* the predictable output of an
ungoverned subsystem: every incident produced a patch, no patch was anchored to a ratified
spec, so patches accreted into the ~16k-LOC, mutually-inconsistent state seen today.
**Recommendation: finalize ONE keeper spec to `specs/` (the redesign is the better basis —
authoritative identity, explicit deletion target, defaults-PIN test), register it, and make
the threshold + identity invariants RED-tested against it before any further keeper bead lands.**

### Evidence index
- No normative spec: `ls specs/ | grep -i keep` (empty); `specs/_registry.yaml` (no keeper).
- Drafts unfinalized: `.kerf/works/session-keeper/05-specs/session-keeper-spec.md:3`;
  `.kerf/works/keeper-redesign/05-spec-drafts/keeper-identity-and-liveness.md:2-4`.
- Code thresholds: `internal/keeper/thresholds.go:35-46`; `thresholds_test.go:27-59`; `git log` hk-8hr1.
- No `--session-id` flag: `cmd/harmonik/keeper_cmd.go:63-71`.
- Gauge-latching identity (forbidden by redesign I2.3): `internal/keeper/watcher.go:681-721`.
- Surviving UUID heuristics (redesign D8/D9 deletion targets): `keeper.go:144-160`,
  `watcher.go:493-502`, `heartbeat.go:161`, `cycle.go:1401`.
- Skill self-admitted drift: `.claude/skills/keeper/SKILL.md:325-361, 85-87, 352`.
