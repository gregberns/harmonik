# leakcheck-hook.zsh — warn at the shell prompt when this user has abandoned
# processes. Sourced from ~/.zshrc. Safe to delete; nothing depends on it.
#
# ─────────────────────────────────────────────────────────────────────────────
# IF YOU ARE AN AGENT AND THIS FILE CONFUSED YOU, READ THIS PARAGRAPH.
#
# This hook prints a warning banner BEFORE AN INTERACTIVE PROMPT. It is not
# output from any command anyone ran, and it never writes to a command's stdout.
# It cannot appear in your tool output, because it does not register at all in a
# non-interactive shell — see the guard below. If you somehow see its banner,
# treat it as a notice about the MACHINE, not about the command you just ran.
# Turn it off for good with:  export HARMONIK_LEAKCHECK=0
# ─────────────────────────────────────────────────────────────────────────────
#
# WHY IT EXISTS. A test-support script span up 30 shell loops to load the
# machine, ran a test, and failed to kill them. They ran for 13 hours at about
# 551% CPU before a human noticed. The detection logic lives in leakcheck.sh
# beside this file; this hook only decides WHEN to run it.
#
# WHAT IT COSTS. One `ps` at most once every $HARMONIK_LEAKCHECK_INTERVAL
# seconds, not on every prompt. A few milliseconds, throttled.
#
# WHAT IT NEVER DOES. It never kills anything, never blocks a prompt, never
# changes an exit status, and prints NOTHING when the machine is clean. Silence
# means healthy.
#
# TO CHANGE IT
#   Disable for one shell   export HARMONIK_LEAKCHECK=0
#   Disable permanently     remove the leakcheck block from ~/.zshrc
#   Warn less often         export HARMONIK_LEAKCHECK_INTERVAL=900   (seconds)
#   Change what counts      edit leakcheck.sh beside this file
#   Run it by hand any time make leakcheck   (from the harmonik repo)

# GUARD 1 — interactive only.
# ~/.zshrc IS sourced in this project's non-interactive agent shells (verified),
# so without this guard every agent tool call would carry the hook. `precmd`
# would still not fire without a prompt, but not registering at all is the
# honest version and costs agents nothing.
[[ -o interactive ]] || return 0

# GUARD 2 — explicit off switch, checked at load AND at fire time.
[[ "${HARMONIK_LEAKCHECK:-1}" == "0" ]] && return 0

# Resolve leakcheck.sh relative to THIS file, so moving the repo does not
# silently disable the hook. ${(%):-%x} is the path of the file being sourced.
typeset -g _LEAKCHECK_SCRIPT="${${(%):-%x}:A:h}/leakcheck.sh"
[[ -x "$_LEAKCHECK_SCRIPT" ]] || return 0

typeset -g _LEAKCHECK_LAST=0

_leakcheck_precmd() {
    # Re-check the off switch every time, so `export HARMONIK_LEAKCHECK=0`
    # takes effect immediately in a shell that is already running.
    [[ "${HARMONIK_LEAKCHECK:-1}" == "0" ]] && return 0

    local interval=${HARMONIK_LEAKCHECK_INTERVAL:-300}
    local now=$EPOCHSECONDS
    (( now - _LEAKCHECK_LAST < interval )) && return 0
    _LEAKCHECK_LAST=$now

    local out
    out="$("$_LEAKCHECK_SCRIPT" 2>/dev/null)" && return 0 # exit 0 == clean, stay silent

    # Only reached when leakcheck.sh exited non-zero, meaning it found something.
    local bar=$'\033[1;33m\u2502\033[0m'
    print -u2 ""
    print -u2 "\033[1;33m┌─ leakcheck: abandoned processes on this machine\033[0m"
    # Prefix each line in zsh rather than with sed. BSD sed does NOT interpret
    # \033 in a replacement and emits it literally, which is the darwin-vs-GNU
    # trap docs/disk-reclaim.md warns about. A shell loop has no such dialect.
    local _line
    while IFS= read -r _line; do
        print -u2 "${bar} ${_line}"
    done <<<"$out"
    print -u2 "\033[1;33m│\033[0m"
    print -u2 "\033[1;33m│\033[0m This is a SHELL HOOK, not output from the command you just ran."
    print -u2 "\033[1;33m│\033[0m   logic    ${_LEAKCHECK_SCRIPT}"
    print -u2 "\033[1;33m│\033[0m   hook     ${${(%):-%x}:A}"
    print -u2 "\033[1;33m│\033[0m   enabled  ~/.zshrc  (search: leakcheck)"
    print -u2 "\033[1;33m│\033[0m   silence  export HARMONIK_LEAKCHECK=0"
    print -u2 "\033[1;33m└─ warns at most every ${interval}s; nothing was killed\033[0m"
    print -u2 ""
    return 0 # never change the prompt's exit status
}

zmodload zsh/datetime 2>/dev/null # provides $EPOCHSECONDS
autoload -Uz add-zsh-hook
add-zsh-hook precmd _leakcheck_precmd
