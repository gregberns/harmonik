# Bash-version fix for `ubs` on macOS (2026-06-27)

## Problem statement

`ubs` (Ultimate Bug Scanner, `/Users/gb/.local/bin/ubs`) is a bash script whose
first executable lines hard-require bash >= 4.0:

```bash
if [ "${BASH_VERSINFO[0]:-0}" -lt 4 ]; then
  echo "ERROR: UBS requires bash >= 4.0 (you have ${BASH_VERSION:-unknown})." >&2
  ...
```

Its shebang is `#!/usr/bin/env bash`. On this machine:

- `/bin/bash` → **3.2.57** (Apple's frozen GPLv2 build — never updated).
- `/opt/homebrew/bin/bash` → **5.3.15** (Homebrew, Apple Silicon).

In **non-interactive** contexts (scripts, git hooks via lefthook, Makefile
recipes, CI runners) `env bash` resolves to `/bin/bash` 3.2 and `ubs` aborts
with the version error. An interactive `bash → /opt/homebrew/bin/bash` alias
exists but aliases are a shell feature of interactive zsh/bash sessions — they
do nothing for `env bash`, scripts, or hooks.

## Why `env bash` picks 3.2

`env bash` does a `PATH` search and runs the first `bash` it finds. The default
**non-interactive** PATH on this machine is:

```
/usr/gnu/bin:/usr/local/bin:/bin:/usr/bin:.
```

`/opt/homebrew/bin` is **not on it** (Homebrew adds it only via the interactive
`~/.zprofile` `brew shellenv`, which login/interactive shells source — hooks and
`env bash` do not). The first `bash` on that PATH is `/bin/bash` (3.2). Note the
ordering detail that matters below: **`/usr/local/bin` precedes `/bin`**, and it
currently has no `bash` in it.

Verified: `env -i bash -c 'echo $PATH'` → the PATH above; `env bash -c 'echo
$BASH_VERSION'` → `3.2.57`.

## Options

| Option | Fixes hooks + Makefile + CI? | Invasiveness | Risk to /bin/bash-3.2 consumers | Recommendation |
|---|---|---|---|---|
| **A. Repo wrapper `scripts/ubs.sh`** (find bash>=4, exec real ubs) | **Yes** — every caller invokes the wrapper, which picks bash 5 explicitly regardless of PATH | None — repo-local file, no system change | **None** — nothing else is touched | **IMPLEMENTED. Primary fix.** |
| **B. Symlink `/usr/local/bin/bash → /opt/homebrew/bin/bash`** | **Yes** — `/usr/local/bin` is ahead of `/bin` on the default non-interactive PATH, so `env bash` then finds bash 5 first | Low–moderate — one system symlink, root-owned dir | **Low** — only changes what *unqualified* `env bash` / PATH lookups resolve to; anything calling `/bin/bash` or `#!/bin/sh` explicitly is unaffected. Risk is a 3rd-party `#!/usr/bin/env bash` script that *secretly depended on* 3.2 behavior (rare; such scripts are the ones that break *because of* 3.2). | **Optional operator step** — makes bare `ubs`/`env bash` "just work" everywhere. Not required given A. |
| **C. PATH reorder** (put `/opt/homebrew/bin` before `/bin` in profiles) | **Partial** — only helps contexts that source the edited profile. Lefthook hooks, `env bash`, and Makefile sub-shells generally do **not** source `~/.zprofile`/`~/.bashrc`, so this does **not** reliably reach hooks/CI | Moderate — edits user dotfiles | Low | **Not recommended** — doesn't actually reach the non-interactive contexts that are the problem. |
| **D. `chsh -s /opt/homebrew/bin/bash` + add to `/etc/shells`** | **No** | High — changes the **login shell** | Low | **Reject.** Login shell is zsh and changing it does nothing for `env bash` resolution inside scripts/hooks. Wrong tool for this problem. |

## What I implemented

`scripts/ubs.sh` — a POSIX `/bin/sh` wrapper (so it runs even under the ancient
shells). It:

1. **Resolves a bash >= 4.0**, preferring `/opt/homebrew/bin/bash`, then
   `/usr/local/bin/bash`, then `command -v bash`, then a hand-scan of every
   `PATH` entry — accepting the first whose `BASH_VERSINFO[0]` is >= 4. If none
   is found it fails loudly with a `brew install bash` hint.
2. **Locates the real `ubs`** — `command -v ubs` first, then
   `~/.local/bin/ubs` (and the absolute `/Users/gb/.local/bin/ubs`) as
   fallbacks. So it works whether or not `ubs` is on PATH.
3. **Defaults `UBS_MAX_DIR_SIZE_MB=5000`** if the caller hasn't set it (the
   repo tree + caches can exceed ubs's 1000 MB default; callers can still
   override, including `=0` to disable).
4. **`exec`s** `"$BASH_BIN" "$UBS_BIN" "$@"` — forwarding all args under modern
   bash.

It does **not** modify `ubs`, dotfiles, `/etc/shells`, or anything global.

**Callers should switch from `ubs ...` to `scripts/ubs.sh ...`** (Makefile
`agent-review`/scan targets, lefthook hooks, any CI step). Today no
Makefile/lefthook/CI step references `ubs`, so this is purely additive — the
wrapper is ready for whoever wires the pre-commit scan in.

### Verification

```
$ scripts/ubs.sh --version
UBS Meta-Runner v5.3.3 (git 448c039d)

# simulated non-interactive / git-hook PATH (the exact default):
$ env -i HOME="$HOME" PATH="/usr/gnu/bin:/usr/local/bin:/bin:/usr/bin" scripts/ubs.sh --version
UBS Meta-Runner v5.3.3 (git 448c039d)

# invoked through /bin/sh explicitly:
$ /bin/sh scripts/ubs.sh --version
UBS Meta-Runner v5.3.3 (git 448c039d)
```

All print the UBS version — the bash-3.2 error is gone.

## Optional system-wide step for the operator (NOT required given the wrapper)

If you want bare `ubs` and any `#!/usr/bin/env bash` script to pick bash 5
**without** going through the wrapper, add one symlink (Option B). This is a
**system config change** — it changes what unqualified `env bash` resolves to
machine-wide:

```sh
# Apple Silicon Homebrew bash 5.x → first-on-PATH for non-interactive contexts.
sudo ln -s /opt/homebrew/bin/bash /usr/local/bin/bash
# verify:
env -i PATH="/usr/gnu/bin:/usr/local/bin:/bin:/usr/bin" bash -c 'echo $BASH_VERSION'
#   expect 5.3.x, not 3.2.57
```

Why it works: `/usr/local/bin` precedes `/bin` on the default non-interactive
PATH, so `env bash` finds the symlink first. Why it's safe: scripts that ask
for 3.2 specifically use `#!/bin/sh` or `#!/bin/bash` (absolute) and are
unaffected; only unqualified `bash`/`env bash` lookups change — and those are
exactly the ones that *want* a modern bash. Caveat: a 3rd-party
`#!/usr/bin/env bash` script silently relying on 3.2 quirks could change
behavior (uncommon). To undo: `sudo rm /usr/local/bin/bash`.

`chsh` / `/etc/shells` (Option D) is **not** the fix — it changes the login
shell, not `env bash` resolution, and the login shell here is zsh.

**Recommendation:** the wrapper (Option A) is sufficient and risk-free; do the
symlink only if you also want ad-hoc bare `ubs` invocations to work without
remembering the wrapper path.

## CI angle

CI is fine without any change. GitHub Actions `ubuntu-latest` images ship
bash 5.x as `/bin/bash` (Ubuntu 22.04/24.04 → bash 5.1/5.2), so `env bash`
already resolves to bash >= 4 there and `ubs` runs natively. The 3.2 problem is
macOS-specific. The repo's workflows (`ci.yml`, `release.yml`, `scenario.yml`)
currently reference `ubs` nowhere; if a `ubs` scan is added to CI it can call
either `ubs` directly (Linux runners) or `scripts/ubs.sh` (uniform across
macOS + Linux self-hosted) — the wrapper is harmless on Linux since it will
just find the system bash 5.
