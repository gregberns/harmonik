# Installing Harmonik

This guide gets harmonik installed and ready to run on a fresh machine. It covers
the tools harmonik depends on, how to build harmonik itself, and a pre-flight
checklist to tick through before your first run.

When you're done here, go to **[QUICKSTART.md](QUICKSTART.md)** to run your first bead.

> New to harmonik? Read **[OVERVIEW.md](OVERVIEW.md)** for what it does and
> **[CONCEPTS.md](CONCEPTS.md)** for the vocabulary (beads, runs, worktrees, the review loop).

---

## Prerequisites

Harmonik is a Go program that drives Claude Code sessions inside tmux, tracks work
in a local task ledger, and merges results through git. You need a few tools in
place before it will run.

| Tool | Why it's needed | Required? | How to install | How to verify |
|------|-----------------|-----------|----------------|---------------|
| **Go** (1.25 or newer) | Harmonik is written in Go; you build it from source with `go install`. | **Required** | Download from <https://go.dev/dl/>, or `brew install go` (macOS) / your package manager (Linux). | `go version` → e.g. `go version go1.26.1 darwin/arm64` |
| **git** | Harmonik creates isolated git worktrees per bead and merges results back. | **Required** | Preinstalled on most systems. macOS: `xcode-select --install`. Linux: `apt install git` (or your package manager). | `git --version` → e.g. `git version 2.50.1` |
| **tmux** | Every Claude session (and the daemon itself) runs inside a tmux session so you can attach and watch. | **Required** | `brew install tmux` (macOS) / `apt install tmux` (Linux). | `tmux -V` → e.g. `tmux 3.6a` |
| **Claude Code** (`claude`) | The agent runtime. Harmonik spawns a `claude` session to implement and review each bead. | **Required** | Install from <https://claude.ai/code> and sign in. | `claude --version` → e.g. `2.1.177 (Claude Code)` |
| **`br`** (Beads) | The task ledger. Each unit of work is a "bead" stored in a local database; harmonik reads and writes bead state through `br`. | **Required** | See **"Installing `br` (Beads)"** below — this one needs care. | `br --version` → e.g. `br 0.2.10` |
| **`kerf`** (planning) | Optional planning and prioritization layer. Gives you ranked work feeds (`kerf next`) and structured planning passes. The core daemon runs fine without it — you can submit beads directly. | *Optional* | See **"Installing `kerf`"** below. | `kerf --help` (kerf has **no** `version` command — see notes) |

The four **required** non-Go tools are: **git, tmux, Claude Code, and `br`**.
**`kerf` is optional** — skip it if you just want to run beads.

---

### Installing `br` (Beads)

`br` is the task ledger CLI from the [beads_rust](https://github.com/Dicklesworthstone/beads_rust)
project. It is a **Rust** program (a self-contained native binary backed by a local
SQLite database), not a Go tool.

The project README publishes this install path:

```bash
cargo install --git https://github.com/Dicklesworthstone/beads_rust
```

This installs `br` into your Cargo bin directory (`~/.cargo/bin`), which must be on
your `PATH`. If you don't have Rust/Cargo, install it first from <https://rustup.rs/>.

> **Honesty note:** the `cargo install` path above is the documented one, but it
> has **not been verified end-to-end on a clean machine**. On the machine this guide
> was validated on, `br` was a prebuilt native binary placed in `~/.local/bin`
> rather than installed via Cargo. If the `cargo install` command fails, check the
> current instructions in the
> [beads_rust README](https://github.com/Dicklesworthstone/beads_rust) — the project
> may also publish prebuilt binaries you can download and drop onto your `PATH`.

Whatever path you use, the result must be an executable named `br` on your `PATH`.
Verify with:

```bash
br --version       # e.g. br 0.2.10
br --help          # should list subcommands including 'init'
```

---

### Installing `kerf` (optional)

`kerf` is the planning/prioritization layer. It lives in its **own separate
repository** ([github.com/gregberns/kerf](https://github.com/gregberns/kerf)), not
inside the harmonik repo. It's a Go program, so you install it with `go install`
from a checkout of the kerf source:

```bash
git clone https://github.com/gregberns/kerf
cd kerf
go install ./cmd/kerf
```

This installs `kerf` into your Go bin directory (the same place harmonik goes —
see the next section), which must be on your `PATH`.

> **Note:** `kerf` does **not** have a `version` subcommand or a `--version` flag.
> To confirm it installed, run `kerf --help` and check that the command list appears
> (`next`, `triage`, `new`, `show`, etc.).

You can always add `kerf` later. Nothing below requires it.

---

## Install Harmonik

Harmonik builds from its own repository with one command.

```bash
git clone https://github.com/gregberns/harmonik
cd harmonik
go install ./cmd/harmonik
```

`go install` compiles the `harmonik` binary and places it in your **Go bin
directory**. Find that directory with:

```bash
go env GOPATH    # e.g. /Users/you/go  → binary lands in /Users/you/go/bin
```

So the harmonik binary ends up at `$(go env GOPATH)/bin/harmonik` (for example,
`~/go/bin/harmonik`).

### Put the Go bin directory on your PATH

For `harmonik` to be runnable from anywhere, `$(go env GOPATH)/bin` must be on your
`PATH`. If `which harmonik` finds nothing after `go install`, add it. For example,
in `~/.zshrc` or `~/.bashrc`:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

Open a new shell (or `source` the file) and confirm:

```bash
which harmonik     # e.g. /Users/you/go/bin/harmonik
harmonik version   # prints the build version
```

> A freshly built local copy may report `harmonik dev (commit: unknown)` for the
> version — that's normal for a build from a working tree.

---

## Pre-flight checklist

Tick through this before your first run. Each line has the exact command to confirm it.

**1. Every required tool is installed and on PATH**

```bash
go version          # Go 1.25+
git --version
tmux -V
claude --version    # and you are signed in
br --version        # the Beads ledger
```

- [ ] Go reports 1.25 or newer
- [ ] git, tmux, and `claude` all print a version
- [ ] you are signed in to Claude Code
- [ ] `br --version` prints a version

**2. (Optional) kerf is installed**

```bash
kerf --help         # command list appears (no version command exists)
```

- [ ] *(optional)* `kerf --help` shows its commands

**3. Harmonik is built and on PATH**

```bash
which harmonik      # resolves to your Go bin directory
harmonik version    # prints a build version
```

- [ ] `which harmonik` finds the binary
- [ ] `harmonik version` runs

**4. Your project is initialized**

Run this from the root of the git repository you want harmonik to work on:

```bash
cd /path/to/your/repo
harmonik init
```

`harmonik init` bootstraps everything the project needs in one step:

- creates the `.harmonik/` runtime directory (gitignored state, events, worktrees)
- initializes the Beads database (`br init`) so you can create beads
- writes the project config files (`.harmonik/config.yaml` and `.harmonik/branching.yaml`)
- renders an `AGENTS.md` from the template and creates the `CLAUDE.md` → `AGENTS.md` symlink
- starts harmonik's supervisor

It's safe to re-run — each step is skipped if its output already exists (use
`--force` to overwrite). To check preconditions **without changing anything**:

```bash
harmonik init --doctor
```

- [ ] `harmonik init` completed (or `harmonik init --doctor` passes)
- [ ] a `.harmonik/` directory now exists in your repo
- [ ] `br ready` runs without error (the bead database is live)

> **Safety — read before pointing harmonik at a repo.** By default harmonik
> **merges and pushes to `main` on every successful bead.** Only run it against a
> personal or throwaway repo until you've configured branch protection
> (`.harmonik/branching.yaml` → `protect_branches`). Branch-protection setup is
> covered in **[CONFIGURATION.md](CONFIGURATION.md)**. (Note: as of this writing,
> `harmonik init --target-branch` only accepts `main`; configure other target
> branches via `.harmonik/branching.yaml`.)

**5. The daemon can start**

You don't have to leave it running yet — QUICKSTART.md walks through the first
real run — but you can confirm it launches in a detached tmux session:

```bash
tmux new-session -d -s harmonik-daemon \
  'harmonik --project /path/to/your/repo --no-auto-pull --max-concurrent 4'
harmonik queue status     # should report the live queue (not "daemon not running")
```

- [ ] the daemon starts and `harmonik queue status` responds

---

## Verified on

This guide was checked against a working setup on **2026-06-12** (macOS, Apple
Silicon / arm64). The tool versions present and confirmed working were:

| Tool | Version found | Location |
|------|---------------|----------|
| Go | `go1.26.1 darwin/arm64` | `/opt/homebrew/bin/go` |
| git | `2.50.1 (Apple Git-155)` | `/usr/bin/git` |
| tmux | `3.6a` | `/opt/homebrew/bin/tmux` |
| Claude Code | `2.1.177` | `~/.local/bin/claude` |
| `br` (Beads) | `0.2.10` (native binary) | `~/.local/bin/br` |
| `kerf` (optional) | built from `github.com/gregberns/kerf` (no version command) | `~/go/bin/kerf` |
| harmonik | `dev` (local build) | `~/go/bin/harmonik` |

Newer versions of each tool are expected to work; these are recorded as a
known-good baseline, not a ceiling.

---

## Next step

Your environment is ready. Head to **[QUICKSTART.md](QUICKSTART.md)** to create a
bead, submit it to the daemon, and watch harmonik run it end to end.

See also: **[CLI-REFERENCE.md](CLI-REFERENCE.md)** (every command),
**[CONFIGURATION.md](CONFIGURATION.md)** (branch protection and daemon settings),
and **[OPERATING-GUIDE.md](OPERATING-GUIDE.md)** (running it day to day).
