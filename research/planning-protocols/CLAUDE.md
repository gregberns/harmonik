# planning-protocols/ -- Research Track Entry Point

Researching **planning protocols**: reusable shapes of human-agent interaction during the idea -> plan -> spec phase of software work. The research goal is to reduce the human-attention cost of reaching alignment between human intent and agent output, while preserving the plan quality that makes implementation cheap.

"Planning protocols" is a working term. Do not rename without user sign-off.

## Read order for every session

1. [STATUS.md](STATUS.md) -- Current phase, what's done, what's next, session history.
2. [METHODOLOGY.md](METHODOLOGY.md) -- The process we are following. Do not diverge without user sign-off.
3. Whatever STATUS.md tells you to read next.

## Hard rules

- **Do not reopen "Locked-in choices"** in METHODOLOGY.md without user sign-off.
- **Do not overwrite prior artifacts.** Append to them, or create dated new files. Prior research is not a scratchpad.
- **Do not skip phases.** The methodology is deliberately sequenced -- extraction before pattern analysis. Jumping ahead pollutes the evidence.
- **Append a session entry to STATUS.md** at session end so the next session knows what happened and why.
- **Use sub-agents liberally** when work fans out (per-project, per-lens). Default is 3+ parallel sub-agents for any corpus-wide task. User explicitly wants parallelism here.
- **Sub-agent prompts must be self-contained.** Include the research question, the sub-phase goal, and any locked choices. Do not assume the sub-agent has read this file.
- **Term "planning protocols"** is the chosen working term -- don't rename it or invent a replacement without user sign-off.

## What this track contains

- `METHODOLOGY.md` -- research question, phased approach, rules
- `STATUS.md` -- live state, session history
- `01-corpus/` -- extracted planning session dialogs (input data)
- `02-analysis/` -- findings from applying analytical lenses (Phase 1D output)
- `references/` -- external/imported source material
- `research-statement.md` -- Phase 1 final output (not yet written)
