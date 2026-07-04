# Vision 4 — Non-software structural models (biology, military doctrine)

## Metaphor 1 — Cell differentiation from a shared genome
**Insight.** Every cell carries *identical* DNA; a neuron vs a liver cell differ only by which genes
are **expressed** vs **silenced**, held stable by an epigenetic state *actively re-asserted every
division*. Differentiation is **maintained**, not one-time; remove the maintenance signal and cells
**de-differentiate** or transdifferentiate into the wrong type. That is admiral→captain drift: the
wrong frame "self-reinstantiates verbatim through every `/clear`" = a cell dividing while copying a
corrupted epigenetic mark.
**Mechanism.** Treat identity as **expression state re-asserted on every division (`/clear`)**,
re-read from an **external immutable master**, never copied from the daughter's drifted state. Add
one field: `soul_source: roles.yaml#admiral` — daemon re-expresses it, model never authors it. The
handoff carries *state*; the genome carries *identity*; they MUST come from different files or you
get Xerox-of-a-Xerox drift.

## Metaphor 2 — Apoptosis & the immune self/non-self check
**Insight.** A cell doing another type's job is precancerous; tissue defends via **apoptosis** (self-
detected identity failure → self-destruct) and **immune surveillance** (external patrol kills cells
presenting wrong markers). Neither trusts the drifting cell to self-diagnose — which is why in-cell
self-check fails in biology, exactly as harmonik's self-check "fires but is answered through the
already-wrong frame."
**Mechanism.** Drift detection must be **external and marker-based, not introspective.** Give each
role cheap **surface markers** — deterministic observable behaviors: "admiral NEVER calls `harmonik
crew start`," "captain NEVER writes `admiral-initiatives.md`," "crew NEVER touches the main queue."
The **watch** session (already always-on, already consuming the bus) becomes the immune patrol: it
matches emitted commands against the role's allowed marker set from `roles.yaml`, and on a non-self
action escalates or triggers forced re-differentiation (identity re-pin). This makes Layer-1's "what
I do NOT do" **executable negative-space**, checked by a different organism than the one drifting.

## Metaphor 3 — Commander's Intent & standing-vs-frag orders
**Insight.** Military orders separate three lifetimes: **commander's intent** (durable *why*,
survives loss of comms — a cut-off subordinate still acts correctly), **standing orders** (durable
*how*, always in force), **frag orders** (ephemeral *what-now*). Intent is written to **degrade
gracefully** — forget everything else and intent alone lets you improvise correctly. This is
isomorphic to SOUL / operating-instructions / handoff, but doctrine adds a missing test: the
**two-levels-up rule** — every unit knows its own intent AND its commander's, so it fills a gap
without asking.
**Mechanism.** Each role's SOUL embeds not just "who I escalate to" but a **one-line copy of the
parent role's intent** — captain's SOUL carries admiral's intent verbatim; crew's carries captain's.
The anti-stall lever: a captain who internalized admiral's intent can self-authorize a known lane
*without escalating* ("does this serve the intent one level up?"). Add `intent:` per role in
`roles.yaml`; launcher composes each seed as `own intent + parent.intent` — a 2-line graft.

## (c) Single most non-obvious idea conventional lenses MISS
**Identity must be re-expressed from an external master on every division, and the master must be a
DIFFERENT file than the one the drifting session wrote.** The software lens says "store config once,
load at launch" (static). Biology reveals identity is a **maintained homeostatic condition** that
decays without re-assertion, and the failure mode is **self-copy corruption**: a session re-seeding
itself from its own HANDOFF faithfully propagates its drift forever, because each copy looks locally
valid. The build: **the keeper's restart path must be structurally forbidden from sourcing Layer-1
from the outgoing session's artifacts.** SOUL re-read from `roles.yaml` (genome, immutable); HANDOFF
supplies only Layer-3 state. This is a **provenance rule on the injection seam**, not a content
rule — and no better-worded principle-text can substitute (fact 5). Fusing identity+state in HANDOFF
builds a drift amplifier.

## (d) The arbitrary assumption exposed
**That "captain" is the privileged hardcoded root and every other role is defined relative to it.**
Both biology and doctrine say this is backwards: in a body there is no master cell type — all types
differentiate as peers from the same genome; the zygote (generic root) differentiates *away* and is
gone. In doctrine the privileged thing is **intent** (role-independent, flows through whoever holds
the post). harmonik hardcodes a *specific role* (captain) as the root of the `start` verb + identity
chain, when the invariant root is the **genome (`roles.yaml`) + the intent chain**. The tell:
`admiral` already works as "a crew with a different mission file" — role identity is already just
expressed config; captain's specialness is legacy, not structural. Make `start` resolve *every* role
(captain included) through the registry.

**Theater caution (actor≠character):** the harness (claude/codex/pi) is the *actor*; the role is the
*character*. A `roles.yaml` entry names the character's requirements and lets any capable actor play
it — don't let a SOUL assume claude-specific behavior, or you can't cast codex/pi in it.
