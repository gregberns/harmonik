# Agent world models & harmonik — recommendation for the operator

*schmidhuber (research crew), 2606.24597 + field survey. Full detail: `01-explainer.md`,
`02-field-survey.md`, `03-harmonik-mapping.md`.*

## What an agent world model is (30 seconds)
It's a model that **plays the environment** instead of the agent: give it the situation-so-far plus the
agent's next action, and it **predicts what the agent would see back** — the terminal output, the file
diff, the tool response. It's the *simulator*, not the decision-maker. Qwen's AgentWorld (Jun 2026) is
the leading text-native example; the classic version is Ha & Schmidhuber (2018) / DreamerV3, where a
policy is trained "in imagination" against a learned copy of the world.

## Is there a real use for harmonik?
Harmonik's environment (repo + build/test/lint/merge, with agents editing and a daemon gating merges)
is a near-exact match for AgentWorld's strongest domains (SWE 68.5, Terminal 57.7, OS 67.9). So the
*concept* fits. But mapping it to the three things we'd actually want, honestly:

| Want | Verdict | Why |
|---|---|---|
| **Testing** — simulate the fleet to break the binary faster | **Doesn't need a world model** | We have a large *real* failure corpus (events.jsonl + ~90 incident notes). Scenario generation from that beats a learned sim, and runs against the real binary. |
| **Implementation** — predict a change's effect before running | **Low value** | Agents already have the real toolchain; `go build` is seconds and exact. A prediction only helps when the real feedback is expensive — which is #3. |
| **Validation** — predict a change passes without a full live run | **Not yet, and maybe never for the case we care about** | Our expensive-to-validate failures are daemon **races/wedges** — long-horizon, timing/disk/concurrency-dependent. That's the *worst* case for a language sim: compounding error + hallucinated outputs + documented **sycophancy** (WebWorld's sim rewards actions that would fail in reality). A false "this passes" is worse than no answer. |

**The maturity reality:** the only thread proven to *train* competitive agents is Dreamer, and only in
**bounded, resettable, low-dimensional** sims — not messy multi-process digital infra like ours. The
2026 LLM-simulator papers claiming "cheaper than real environments" are unreplicated preprints whose own
limitations sections concede exactly the failure modes that would bite us.

## Recommendation: **not worth a build now — but there's one cheap experiment worth running.**
Don't invest in an agent world model for daemon validation. It targets our hardest-to-simulate surface
(concurrency/timing) with the technique's weakest property (factual fidelity), and the real feedback we'd
replace is already cheap and exact for everything *except* the very races a sim can't capture.

**The one experiment that would prove-or-kill the strongest sub-claim** — that a world model predicts
build/test/merge outcomes accurately enough to skip real runs:

> **Zero-build validation probe.** Take ~50 recent merged/failed beads. For each, give an off-the-shelf
> model (no training) the diff + repo context and ask it to predict: does it build? pass tests? merge
> clean, or hit `non_ff_merge` / `merge_build_failed`? Score predictions against the *known* real
> outcomes from events.jsonl. **Kill criterion:** if it can't beat ~90% precision on the "will pass"
> prediction (i.e. it green-lights changes that really failed), the idea is dead for validation — the
> sycophancy/hallucination gap is real for us. **Green criterion:** if precision is high on the
> deterministic cases, there's a narrow, real win: pre-screen cheap-deterministic beads and reserve live
> runs for the uncertain ones. Cost: one afternoon, no training, no new infra.

This probes the claim without building anything, and its result is decisive either way.

## Separately — the operator's "break the binary" quality goal
That goal is real and worth pursuing, but it's an **adversarial scenario harness**, not a world model:
mine the real failure corpus for signatures (concurrent same-file → merge race; disk<10GiB → cache wipe;
reviewer-pane death → wedge), replay/perturb them against a scratch daemon. A world model could *later*
prioritize which scenarios to run, but it's not the starting move. **Recommend coordinating this framing
with admiral as part of the quality initiative — it's higher-leverage than the world-model angle.**

## One-line answer for the operator
Interesting and real as a field, but for harmonik it's **a solution aimed at our hardest surface with its
weakest property** — don't build one; run the one-afternoon zero-build validation probe to settle the
only sub-claim that could change that, and put the "break the binary" energy into a scenario harness
mined from our real failures.
