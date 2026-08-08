# Roles

A role is a set of instructions an agent reads and follows. It is a document, not a process.

Nothing here is launched. There is no command that "starts a role". An agent that is already
running becomes the assessor by reading `roles/assessor/`, and it stops being the assessor when it
posts its verdict. The same is true of every role in this folder.

That sentence exists because the opposite was assumed, repeatedly, and it cost real time. The role
instructions used to live inside `.harmonik/agents/<role>/`, which made them look like
configuration for a running fleet. They are not. They work with harmonik running, and they work
with nothing running at all.

## What is here

    roles/
      assessor/     operating.md · soul.md · personality.md · good-enough-principles.md
      captain/      operating.md · soul.md
      admiral/      operating.md · soul.md

`soul.md` is who the role is — what it does, what it refuses to do, who it escalates to.
`operating.md` is how it works — the loop, the bounds. Roles may add their own files; the assessor
carries a personality and the good-enough bar it grades against.

## Using a role with no harmonik running

Read `soul.md`, then `operating.md`, then any other file in the folder. Do what they say.

Each `operating.md` marks the steps that need a live daemon with **[FLEET]**. With nothing running,
skip those and substitute the plain equivalent:

| [FLEET] step | What to do instead |
|---|---|
| `harmonik comms join` / `comms recv --follow` | Nothing. There is no bus to join. |
| Post status or a verdict over `comms send` | Say it to the operator in your own session. |
| `harmonik start crew <name>` | Ask the operator to task an agent, or use your own sub-agents. |
| Read a mission via `--mission <path>` | Read the brief in your handoff file. |

Everything else — reading a diff, running `make core`, standing up a scratch daemon, filing
issues — works exactly the same either way. The scratch daemon in particular is a throwaway process
a role starts for itself; it does not need the fleet daemon and never touches it.

## Using a role with harmonik running

`.harmonik/agents/<role>/manifest.yaml` names its folder here with a `role:` key:

    role: roles/assessor

`harmonik agent brief` then reads `soul.md` and `operating.md` from that folder instead of from the
type folder. The type folder keeps only the harmonik-side wiring — cardinality, triggers, context
refs, keeper thresholds, markers.

A manifest with no `role:` key reads both files from its own folder, as it always has. `crew`,
`commodore` and `watch` still do that; they have not been moved.

**There is one copy of a role's instructions, and it is the one in this folder.** Do not add a
second copy under `.harmonik/agents/` — a stub that says "see roles/" is still a file that can
drift, and drift between two files that both look authoritative is the thing this layout removes.

## Adding a role

1. Create `roles/<name>/` with `soul.md` and `operating.md`.
2. Mark any step that needs a running daemon with **[FLEET]**.
3. If harmonik should also know about it, add `role: roles/<name>` to
   `.harmonik/agents/<name>/manifest.yaml`.

The role path is resolved from the repo root. An absolute path, or one that climbs out of the
repo, is refused — a role folder is a checked-in part of the project.

## The assessor writes down what it did

The assessor is the one role that keeps a durable record. Every assessment gets its own dated
folder under `assessments/`, outside this folder, in the same spirit as `plans/`. See
[`assessments/README.md`](../assessments/README.md). Roles do not store their output here; `roles/`
holds instructions only.
