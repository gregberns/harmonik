# Plan 009 — harmonik CLI help redesign

> Source: planning sub-agent, 2026-05-19. The CLI is meant to be agent-driven; an agent that runs `harmonik --help` and sees nothing useful cannot discover the surface. First-touch UX must be self-teaching.

## Observed problems

```
$ harmonik run --help
  harmonik run: unknown flag "--help"

$ harmonik --help
Usage of harmonik:
  -max-concurrent int
        maximum number of beads dispatched concurrently (default 1) (default 1)
  -project string
        project directory (default: current working directory)
```

1. `--help` is unknown on the `run` subcommand (and every other subcommand).
2. Top-level `harmonik --help` lists no subcommands — `run`, `handler`, `queue`, `reconcile`, `tmux-start`, `hook-relay` are invisible.
3. `--max-concurrent` prints `(default 1) (default 1)` — `flag` adds the default automatically AND the description string repeats it.
4. No per-subcommand help. No flag list for `run`, `run --beads`, `--context`, `--review-loop`.

## Done means…

1. `harmonik --help` exits 0 and names all six subcommands with one-line descriptions.
2. `harmonik run --help` exits 0 and lists `--beads`, `--max-concurrent`, `--context`, `--review-loop`, `--project`, an exit-codes table, and at least one example.
3. `harmonik handler --help` exits 0 and lists both verbs (`status`, `resume`) and their flags.
4. `--max-concurrent` default appears exactly once in `harmonik --help` output.
5. Smoke test: an agent that runs `harmonik --help` followed by `harmonik run --help` has enough information to construct a valid `harmonik run --beads hk-x,hk-y --max-concurrent 2` invocation without reading any other documentation.

## Diagnosis

**Mechanism.** `cmd/harmonik/main.go` uses a pre-`flag.Parse` `if os.Args[1] == "X"` chain to dispatch subcommands. Each subcommand (`run`, `handler`, `reconcile`, `queue`, `tmux-start`, `hook-relay`) receives `os.Args[2:]` and parses its own flags with a hand-rolled `for i, arg := range subArgs` loop using `strings.HasPrefix(arg, "--flagname")` and `strings.HasPrefix(arg, "-")` as the unknown-flag catch-all.

**Why `--help` is rejected.** In `run.go` line 115: `case strings.HasPrefix(arg, "-"):` is the catch-all that fires for every unrecognised flag, including `--help` / `-h`. The hand-rolled parser has no concept of help flags. Same pattern in `handler.go`, `reconcile.go`, and the `tmux-start` inner loop.

**Why top-level help is empty.** `flag.Usage` is the default which only enumerates registered flags (`--project`, `--max-concurrent`). No subcommand listing code exists.

**Duplicated default.** `main.go:245` — `flag.IntVar(&maxConcurrentFlag, "max-concurrent", 1, "maximum number of beads dispatched concurrently (default 1)")`. The `flag` package automatically appends the default; the description string adds it again. Fix: drop `(default 1)` from the string.

## Migration verdict

**No framework migration.** All five subcommands already share a consistent hand-rolled pattern. Existing tests (`main_test.go`, `handler_test.go`) call internal functions directly (`runBeadSubcommand`, `runHandlerSubcommandIO`) and depend on no `flag.FlagSet`. Adding `--help`/`-h` intercepts and usage functions costs ~80 lines and zero new dependencies. Cobra/kong would impose a larger diff and conflict with test wiring.

## Desired help output

### `harmonik --help` (top-level)

```
harmonik — agent-driven bead execution daemon

USAGE
  harmonik [--project DIR] [--max-concurrent N]
  harmonik <subcommand> [flags]

SUBCOMMANDS
  run          Execute one or more beads and exit on completion
  handler      Inspect or resume a paused handler
  queue        Submit or inspect the bead queue (daemon must be running)
  reconcile    Close in_progress beads whose implementation has merged
  tmux-start   Bootstrap a tmux session and start the daemon inside it
  hook-relay   Forward a Claude hook event to the daemon (internal use)

DAEMON FLAGS (used without a subcommand)
  --project DIR          Project directory (default: current working directory)
  --max-concurrent N     Max simultaneous beads (default 1)

EXAMPLES
  # Start the daemon in the foreground:
  harmonik --project /path/to/project

  # Run a single bead to completion:
  harmonik run hk-abc123

  # Run multiple beads in parallel:
  harmonik run --beads hk-abc123,hk-def456 --max-concurrent 2

Run 'harmonik <subcommand> --help' for subcommand-specific flags.
```

### `harmonik run --help`

```
harmonik run — execute beads and exit on completion

USAGE
  harmonik run <bead-id> [flags]
  harmonik run --beads id1,id2,... [flags]

FLAGS
  --beads id1,id2,...    Comma-separated bead IDs (mutually exclusive with positional <bead-id>)
  --max-concurrent N     Maximum simultaneous beads (default 1)
  --context TEXT         Free-form extra context injected into each agent task
  --context @FILE        Same, but read context from a file
  --review-loop          Route all beads through the review-loop workflow
  --project DIR          Project directory (default: current working directory)

EXIT CODES
  0   All beads succeeded
  1   At least one bead failed, or argument/validation error
  2   Unexpected queue state (diagnostic)
  5   Another harmonik instance is already running (pidfile locked)

EXAMPLES
  harmonik run hk-abc123
  harmonik run --beads hk-abc123,hk-def456 --max-concurrent 2
  harmonik run hk-abc123 --context "Focus on the migration spec only"
  harmonik run hk-abc123 --context @/path/to/context.txt
  harmonik run hk-abc123 --review-loop
```

### `harmonik handler --help`

```
harmonik handler — inspect or resume a paused handler

USAGE
  harmonik handler <verb> [flags]

VERBS
  status   Show handler pause state (no daemon required)
  resume   Resume a paused handler

FLAGS (status)
  --type AGENT-TYPE       Filter to a single handler type (e.g. claude-code)
  --format json|text      Output format (default text)
  --json                  Shorthand for --format json
  --project DIR           Project directory (default: current working directory)

FLAGS (resume)
  --type AGENT-TYPE       Handler type to resume (required)
  --force                 No-op if already live, instead of error
  --project DIR           Project directory (default: current working directory)

EXAMPLES
  harmonik handler status
  harmonik handler status --type claude-code --format json
  harmonik handler resume --type claude-code
```

## File-by-file change list

1. **`cmd/harmonik/main.go`** — drop `(default 1)` from the `--max-concurrent` description string; add `harmonikUsage()`; set `flag.Usage = harmonikUsage`; add a top-level `--help`/`-h` arm in the dispatch chain.
2. **`cmd/harmonik/run.go`** — in `runBeadSubcommand`'s flag-parsing loop, add `case arg == "--help" || arg == "-h":` before the catch-all → `runUsage()` → return 0.
3. **`cmd/harmonik/handler.go`** — intercept `--help`/`-h` on `subArgs[0]` before the verb dispatch; add `handlerUsage()`; add per-verb intercepts in `runHandlerStatus` and `runHandlerResume`.
4. **`cmd/harmonik/reconcile.go`** — intercept `--help`/`-h`; add `reconcileUsage()`.
5. **`cmd/harmonik/usage.go`** (new, optional consolidation) — house all `*Usage()` functions.
6. **`cmd/harmonik/help_test.go`** (new) — substring-assertion tests on stderr capture for each `--help` path.
7. **Subcommands with minimal help** (`queue`, `tmux-start`, `hook-relay`) — short `--help` intercepts in the existing dispatch parsers.

## Bead breakdown (6 beads)

- `<plan>` — file this plan + kerf work.
- `<help-top>` — top-level `harmonik --help` + fix duplicated default.
- `<help-run>` — `harmonik run --help`.
- `<help-handler>` — `harmonik handler --help` + per-verb help.
- `<help-rest>` — `reconcile`, `queue` (verb listing), `tmux-start`, `hook-relay`.
- `<help-tests>` — `cmd/harmonik/help_test.go` substring/golden tests.

Top-level and `run` beads are highest priority — they're what agents hit first.

## Kerf work

Warranted (`--jig plan`). Touches the agent-facing CLI surface; crosses four files plus a new test file; clear observable "Done means..." criteria. No spec change required (no spec document governs CLI help format).
