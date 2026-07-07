# Field survey — world models for agents (mid-2026)

Four threads. Numbers are author-reported unless noted; **2026 arxiv preprints (26xx.*) are recent and
not independently replicated** — the "beats real-environment training" headlines especially.

## 1. DeepMind — Genie & SIMA (generate a world / act in a world)
- **Genie 3** (DeepMind, Aug 5 2025): generates a navigable 3D world from a text prompt in real time —
  720p, 24fps, a *few minutes* of interaction, ~1 min of visual memory, with "promptable world events"
  (inject weather/objects mid-stream). Real leap over Genie 2 (~10–20s).
- **SIMA 2** (Nov 13 2025, tech report arxiv 2512.04797): a generalist embodied agent whose policy is a
  Gemini model; reasons about goals, generalizes to unseen games, and self-generates tasks/rewards to
  learn "independently of human demonstrations." Was *tested* inside Genie-3 worlds.
- **Honest maturity: advanced research demo.** Both halves work in isolation. The closed loop —
  generate worlds → train agents in them at scale → measurably better real agents — is **not shown**.
  DeepMind's own limits disqualify training use today: minutes (not the *hours* training needs),
  limited agent action space, no multi-agent, limited-access research preview.

## 2. Ha & Schmidhuber → Dreamer (the proven core)
- **Ha & Schmidhuber, "World Models"** (2018): compress observations, learn latent dynamics, train the
  policy **entirely "inside its own dream,"** then transfer. Solved VizDoom Take-Cover from dream
  training. The still-valid motivation: a learned sim is far cheaper than the real engine and can be run
  arbitrarily often.
- **DreamerV3** (Hafner, 2023; *Nature* 2025): trains actor+critic "in imagination." **One fixed config
  beats specialists across 150+ tasks**; **first to mine Minecraft diamond from scratch** with no human
  data; ~130× more sample-efficient than IMPALA on DMLab.
- **Honest maturity: the ONLY thread that clears "world models demonstrably train competitive agents."**
  Peer-reviewed, reproducible, open-source. **But** only in **bounded, resettable, low-dimensional
  simulators** (games/robotics/pixels) — not open-ended or messy digital environments. Not production
  outside research labs.

## 3. LLM / language world models (newest, most hype-prone)
Two sub-uses:
- **(A) LLM-as-world-model for planning** — **RAP** (Hao 2023, EMNLP): one LLM is both world model
  (predict next reasoning state) and agent, guided by MCTS; ~33% relative gain. Reviewed and real — but
  it simulates *reasoning states*, not environments (this is "Tree-of-Thought as world-model search").
- **(B) LLM-as-environment-simulator to train agents** — the **Qwen-AgentWorld family**:
  - **Qwen-AgentWorld** (Jun 2026): simulates 7 agent domains as text; claims "Sim RL" against the
    world model surpasses real-env training on OpenClaw-style envs.
  - **WebWorld** (Feb 2026): web-state simulator on >1M trajectories; agents fine-tuned on its synthetic
    trajectories gain +10.9% WebArena / +9.9% MiniWob++; but admits it "generates overly optimistic
    outcomes that cater to the agent's action" (sycophancy).
  - Related: DreamGym, Simia-RL, AgentGym-RL, MobileGym.
- **Honest maturity: research demos, self-reported.** The idea is a legit extension of Dreamer to
  text/GUI agents; the "better than real environments" headline rests on **fresh, unreplicated 2026
  preprints** whose own limitations sections concede the killers below.

## 4. Where they break (from the papers themselves, not critics)
- **Compounding error / hallucinated states** — long trajectories accumulate error; Qwen-AgentWorld:
  **factuality is the weakest dimension even after RL** (~11% relative gain).
- **Sycophancy / optimistic bias** — WebWorld's sim rewards actions that would fail in reality.
- **Reward hacking / collapse** — agents insert self-praise to fool the judge.
- **Distribution shift** — sims drift off the real environment's distribution.

**Bottom line:** the Dreamer lineage is the empirical bedrock (works in *bounded* sims); Genie/SIMA are
the flashiest but are training-previews by DeepMind's own admission; the 2026 LLM-simulator wave is the
most exciting **and** the most over-claimed. For any harmonik use, treat "cheaper than a real run" as a
hypothesis to test, not a proven property — and expect the failure to be **plausible-but-wrong outputs**,
which is fatal for a deterministic CI/daemon environment.

*Sources: DeepMind Genie 3 & SIMA 2 blogs; Ha & Schmidhuber 2018 (arxiv 1803.10122); DreamerV3 (2301.04104,
Nature 2025); RAP (2305.14992); Qwen-AgentWorld (2606.24597); WebWorld (2602.14721); AgentGym-RL;
"Text World Models for LLM-based Agents" (2606.09032); "LLM-Based World Models… Rigorous Evaluations
Needed" (2411.08794).*
