#!/usr/bin/env bash
# secret-scan.sh — refuse content that adds a credential pattern or a .env file.
#
# WHY THIS FILE HAS MODES. Nothing called this scan for the twenty days from
# 2026-07-23, when lefthook was deleted, to 2026-08-12. lefthook's pre-commit
# block was its only caller. After that `make -n fast`, `make -n full`, `make -n
# gate-static` and `make -n core` held zero occurrences of this script's name;
# the only invocation anywhere was a leaf `make secret-scan` target that nothing
# depended on; it had never run in CI at any point in its life. Four documents
# said it ran.
#
# AND IT DID NOT WORK. Do not soften that into "it worked, but nobody called
# it" — an earlier draft of this comment said exactly that, and it is the shape
# of error this file keeps having to correct. The scan body was byte-identical
# from its creation on 2026-05-31 to 2026-08-12, and it had no test of any kind
# for those seventy-three days. The first test written against it, on the last
# of them, found it admitting a key in an ordinary-sized commit: `grep -q`
# behind a pipe under `pipefail` read a match as no-match, and the private-key
# pattern's leading dashes made grep exit 2 with the message discarded, so that
# pattern had never matched anything. A scan nobody runs is a scan that finds
# nothing, and nothing reports that — which is why the assertion that matters
# for this file lives in scripts/gate-fails-closed-test.sh and reads the
# expanded step list.
#
# THAT WIRING ASSERTION IS STRUCTURAL, and a structural one has a known blind
# spot: it reads what `make -n gate-static` PRINTS. A recipe line prefixed with
# '-' prints the same as a wired one while make ignores its status, so the scan
# could find a key and the gate could still exit 0. The Makefile carries no such
# line today. A BEHAVIOURAL assertion would subsume the structural one — stub
# this script to exit 1, then require that `make gate-static` fails — and it
# would catch a commented-out step, a deleted step, a '-' prefix, `.IGNORE` and
# `make -i` in one test instead of none. Noted here rather than done, because the
# wiring tests are not this file's to restructure.
#
# It could not simply be added to a gate. The gates run AFTER the commit is
# made, and this script read the INDEX, which the ordinary flow leaves empty by
# then. Wiring it as it stood would have installed a step that reads whatever a
# developer happened to leave staged rather than the change under test. NOT a
# guaranteed no-op — stage a key and the index scope does block it — but a step
# that answers a question nobody asked, and that is silent when it answers
# nothing. Hence the two new scopes below.
#
# THREE SCOPES. The flag spelling follows scripts/commit-msg-gate.sh, which
# solved the same problem for commit messages.
#   (default)     the INDEX — `git diff --cached`. Pre-commit and standalone
#                 use. Unchanged.
#   --head-only   the commit just made, against its first parent. Runs in
#                 gate-static, beside the commit-message tip check, for the same
#                 reason: the commit is yours, it is the tip, and amending it
#                 costs nothing.
#   --range       everything this branch adds on top of BASELINE. Runs in
#                 `make full`, the merge decision.
#
# EVERY SCOPE REFUSES A FINDING — which is not the same as every scope reading
# the whole span, and the fallback below is where those two come apart.
# --range does NOT copy the commit-message gate's advisory
# behaviour, and the difference is not an oversight. A bad message on a merged
# commit has no legal repair: this project refuses to amend a commit another
# lane can see, so a gate that failed on one would be deleted rather than
# obeyed. A leaked credential has a real repair — rotate the key, and take the
# value out of the tree before the merge lands. So this one fails.
#
# WHY --range READS THE NET DIFF AND NOT EACH COMMIT. `git diff BASELINE HEAD`
# is one tree diff: 0.33 seconds over the whole baseline-to-HEAD span on this
# branch, with a warm object cache, measured 2026-08-12. The commit and line
# counts for that span move with every commit, so the ratio below is the durable
# part and the absolute figures are deliberately not quoted here. Walking the
# same range one commit at a time takes 16.6 seconds on the same box in the same
# session — fifty times more. An earlier draft of this line said "1.25 seconds"
# and "costs minutes"; neither number could be reproduced, so both were replaced
# by a measured run.
#
# The walk also mostly repeats what the tip check already did. Say that as it
# is, because an earlier draft said "every commit passes through --head-only at
# the moment it is made" and that is not true: --head-only runs when somebody
# runs a gate, not when somebody commits. It reads whatever was the tip the last
# time a gate ran, so a push of five commits gets its tip read and the other
# four never read. Walking the range would close that, and it is not what the
# net diff buys. --range answers the merge question: what does this branch put in the
# target tree, INCLUDING everything that arrived by merge from a lane whose own
# gate-static never ran.
#
# Exit 0 = nothing found in scope; exit 1 = a finding, or the scan could not run.
set -uo pipefail

# Patterns that match known secret formats.
# Each entry is an ERE pattern applied to the added lines in scope.
SECRET_PATTERNS=(
    'ANTHROPIC_API_KEY[[:space:]]*=[[:space:]]*[A-Za-z0-9_-]{10,}'
    'sk-ant-api[0-9]+-[A-Za-z0-9_-]{20,}'
    'sk-ant-[A-Za-z0-9_-]{30,}'
    'AWS_SECRET_ACCESS_KEY[[:space:]]*=[[:space:]]*[A-Za-z0-9/+]{20,}'
    'GITHUB_TOKEN[[:space:]]*=[[:space:]]*(gh[ps]_|github_pat_)[A-Za-z0-9_]'
    'OPENAI_API_KEY[[:space:]]*=[[:space:]]*sk-[A-Za-z0-9_-]{20,}'
    '-----BEGIN (RSA|EC|DSA|OPENSSH) PRIVATE KEY-----'
)

# BASELINE — the tree --range measures against. Everything this branch adds on
# top of it is in scope; that tree itself is grandfathered.
#
# HOW IT WAS CHOSEN, and it is not the merge base with main. The widest range
# that is green was found by walking outward from HEAD: this commit is the
# oldest base whose net diff carries no finding, and it went in green over the
# hundreds of commits and the two hundred thousand added lines the branch
# already held, not over an empty set. The exact figures move with every commit
# and are not quoted here for that reason; what is stable is the pair of
# measurements below, which were re-taken on 2026-08-12.
#
# ONE COMMIT OLDER PICKS UP TWO, AND THE MERGE BASE PICKS UP SIX. An earlier
# draft of this comment gave the merge base's six as the figure for one commit
# older, and named internal/harness among the files. Measured: at one commit
# older the range reports TWO findings, both in internal/daemon, and none in
# internal/harness. At the merge base with main it reports SIX, spanning
# internal/daemon and internal/harness. All eight are the same shape — a test
# that sets `ANTHROPIC_API_KEY=` to a fake value to assert the key is stripped
# from a child process — and all eight match one pattern, the first in the list
# above. They are not credentials, and the pattern cannot tell them apart from
# one, so the honest move is to grandfather them and say so rather than to
# weaken the pattern.
#
# Those six are the ONLY thing standing between this line and the merge base
# with main, and that is measured rather than hoped: over the merge base's whole
# net diff the seven patterns match six lines in total, all of them that one
# shape, and the other six patterns match nothing. Fix those fixtures and this
# line can move back.
#
# It is a long way back. The merge base sits 460 commits below this baseline
# (measured 2026-08-12), so "one commit older" and "the merge base with main"
# are not neighbours, and nothing here has checked the 458 bases in between.
# What is checked is the two ends.
#
# The fixture is DESCRIBED here and not quoted, and that is not squeamishness.
# An earlier draft of this comment spelled the value out, so the comment matched
# the pattern three lines above it and the commit that added the comment was
# refused by its own scan. There is no allowlist and no inline pragma for a
# deliberate sentinel — bead hk-sb1ii is the record of that gap — so until there
# is one, do not write a credential-shaped string anywhere in this tree, not
# even to explain one.
#
# IT IS NOT AN ANCESTOR OF MAIN, and that has a consequence a reader must know
# before trusting a green --range: when the baseline does not resolve as an
# ancestor of HEAD, the scan falls back to HEAD^1..HEAD and can pass on a tree
# that holds a key. That is usually the tip commit alone, but not always: when
# the tip is a merge it is the whole merged side, and when HEAD has no parent at
# all the base becomes the empty tree and the scan reads the WHOLE tree, which
# is the widest scope rather than the narrowest. See the fallback below, and
# bead hk-254ea.
#
# THE SAFE DIRECTION IS BACKWARD, and an earlier draft of this comment said the
# opposite in the same breath as forbidding what it licensed. It read "it only
# ever moves FORWARD" and then, one clause later, "widening the grandfathered
# tree to turn a red gate green is the move this scope exists to prevent" —
# but the scope is BASELINE..HEAD, so moving the baseline forward IS widening
# the grandfathered tree. Older baseline, wider scan, stricter gate. Newer
# baseline, more of the tree grandfathered, weaker gate.
#
# So: moving this line toward HEAD is the edit a reviewer should look at
# hardest, because it is the one that turns a red gate green without removing a
# key. Rotate the key and take the value out of the tree instead. Moving it the
# other way, toward the merge base with main, only ever adds coverage — that is
# the move the paragraph above is working toward.
#
# THE FILE IS NOT THE ONLY PLACE THE BASELINE COMES FROM, and a reviewer has to
# know that before trusting the paragraph above. SECRET_SCAN_BASELINE overrides
# it from the environment, and the override is load-bearing rather than
# incidental: the suite uses it in eleven places to give a scratch repository
# its own baseline. But it means the dangerous edit does not have to be an edit.
# Running `make full` with SECRET_SCAN_BASELINE set to HEAD narrows --range to
# the tip and prints the same reassuring clean line, and no diff shows it.
# scope_note names the commit that was used, so the run's own output is the only
# place that answer appears — read it rather than this line.
#
#   0743e4dec daemon: pin the D2 credential refusal behaviorally
BASELINE="${SECRET_SCAN_BASELINE:-0743e4dec8b1f38bc311038a3e5b435415553d16}"

# EVERY POSITION IS READ, not only the first. The earlier form looked at "$1"
# alone, so `secret-scan.sh --head-only --range` and `--head-only --garbage`
# both ran the head scope and exited 0 — a caller that asked for the wide scope,
# or that made a typo, got a narrow pass and no word about it. Two scopes at
# once is refused as well: there is no reading of that where one of them is not
# being silently dropped.
MODE=staged
scope_flag=''
while [ "$#" -gt 0 ]; do
    case "$1" in
        --head-only|--range)
            if [ -n "$scope_flag" ]; then
                echo "secret-scan: '$1' cannot be combined with '$scope_flag' — pass one scope or none" >&2
                exit 1
            fi
            scope_flag="$1"
            case "$1" in
                --head-only) MODE=head ;;
                --range)     MODE=range ;;
            esac
            ;;
        *)
            echo "secret-scan: unknown argument '$1' (expected --head-only, --range or nothing)" >&2
            exit 1
            ;;
    esac
    shift
done

fail_hard() {
    echo "secret-scan: BLOCKED — $*" >&2
    exit 1
}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
diff_file="$work/diff"
scan_diff="$work/scan-diff"
names_file="$work/names"
added_file="$work/added"
names_lines="$work/names-lines"
loc_file="$work/loc"

# The empty tree, so a root commit can be diffed against something.
EMPTY_TREE=4b825dc642cb6eb9a060e54bf8d69288fbee4904

# EVERY CONTENT DIFF PASSES --text --no-color --no-ext-diff --no-textconv, and
# each flag closes a way to make this scan read nothing.
#
# --no-color. Git colour is configurable, and `color.ui = always` or
# `color.diff = always` forces it on even when the output is a pipe. Every line
# then arrives wrapped in ANSI escapes, and the damage is not that a pattern
# misses a decorated key — it is that the PARSER never opens a hunk, because a
# hunk header reads "\033[36m@@ -0,0 +1 @@" and the test for it is on the first
# three bytes. Measured 2026-08-12 in a scratch repository with git 2.50.1, a
# single committed key and `color.ui = always`: all three scopes printed
# "clean — 0 added line(s) read" and exited 0, and all three block with
# --no-color. Colour is set in local .git/config or on the command line, so it
# does not travel in a commit; like diff.external it is the accident case — a
# developer or a CI job that forces colour turns the scan into a no-op that
# still prints. Case 46 is the fixture and it covers all three scopes.
#
# The file list below is pinned the same way, and there the flag is precaution
# rather than a measured fix: git 2.50.1 does not colour `--name-only` under
# either config, measured the same day. It is pinned because a coloured path
# would defeat the .env anchor exactly the way C-quoting did, and that failure
# would be silent.
#
# --text. One line in .gitattributes — `creds.txt -diff` — makes git call the
# file binary. `git diff` then prints "Binary files a/creds.txt and b/creds.txt
# differ" and no '+' line at all, so the parser below reads zero added lines,
# prints clean and exits 0 with the key at HEAD. Measured in a scratch
# repository: the commit that adds the attribute and the key together reads
# "1 added line(s)" — the .gitattributes line — and exits 0. The same commit
# with --text blocks. Whoever commits the key commits the attribute, so the
# file that turns the scan off travels with the leak.
#
# --no-ext-diff. A `diff.external` command in .git/config, or the same program
# named in the GIT_EXTERNAL_DIFF environment variable, replaces git's own diff
# with whatever that program prints. A program that prints nothing gives a
# content diff of ZERO bytes — a total kill, worse than the -diff attribute,
# which at least still shows the other files in the commit. Measured in a
# scratch repository on 2026-08-12: a commit whose diff is 181 bytes reads 0
# bytes under either form, and 181 bytes again with --no-ext-diff.
#
# --no-textconv. A textconv filter behind a `diff=<driver>` attribute does the
# same for the files that attribute names. Measured the same day: 340 bytes
# without the driver, 238 bytes with it, 340 bytes again with --no-textconv.
#
# THESE TWO ARE LESS SEVERE THAN THE -diff ATTRIBUTE, AND THE REASON IS WHAT
# TRAVELS. `.gitattributes` is a tracked file, so `creds.txt -diff` rides inside
# the same commit as the key and turns the scan off on every machine that reads
# it. `diff.external`, `diff.<driver>.textconv` and GIT_EXTERNAL_DIFF live in
# local .git/config or in the environment. Neither is tracked, so neither can be
# committed: measured in the same scratch repository, the `diff=<driver>`
# attribute alone, with the driver unconfigured, left the diff at its full 340
# bytes. Somebody with write access to the box running the scan can still set
# them, so both flags stay — but the attribute is the vector a commit can carry
# on its own, and the case names below keep the two apart.
#
# THE FLAGS ARE FREE HERE. Over the whole --range span on this tree both forms
# print 270,018 lines, byte for byte, at the same runtime (measured 2026-08-12).
#
# --text HAS A COST, AND IT IS NOT THE ONE AN EARLIER DRAFT OF THIS COMMENT
# CLAIMED. That draft said a binary blob read as lines "can only ADD candidate
# lines, and one match refuses". It does not add the line. `git diff --text`
# prints the blob's raw bytes after a '+', NUL bytes and all, and what the
# parser below then makes of them depends on which awk is installed. Measured
# 2026-08-12 on the record `+HEADER<NUL>SECRETTAIL`:
#
#   awk 20200816 (the one-true-awk macOS ships)  length($0) = 7   tail LOST
#   GNU Awk 5.4.1                                length($0) = 18  tail kept
#   mawk 1.3.4                                   length($0) = 18  tail kept
#
# So the same commit answered differently on a developer's Mac and on a Linux
# runner: a scratch commit adding `HEADER<NUL><3 control bytes>` followed by a
# real key printed "clean — 1 added line(s) read", exit 0, key at HEAD. A gate
# that decides a merge must not depend on which awk is on the box, so the NULs
# are taken out of the diff before the parser sees it. See the transform below.
#
# The file list below keeps the plain form. `--name-only` prints paths, it does
# not run a diff driver, and a `-diff` attribute does not take a path off it.

scope_note=''

case "$MODE" in
staged)
    scope_note='the staged index'
    git diff --cached --text --no-color --no-ext-diff --no-textconv >"$diff_file" 2>"$work/err" \
        || fail_hard "the staged diff could not be read: $(cat "$work/err")"
    git diff --cached --name-only -z --no-color --diff-filter=ACMRT >"$names_file" 2>"$work/err" \
        || fail_hard "the staged file list could not be read: $(cat "$work/err")"
    ;;
head)
    git rev-parse --verify HEAD >/dev/null 2>&1 \
        || fail_hard "there is no HEAD to scan, and a scan that reads nothing is not a pass"
    # The FIRST parent, so a merge commit is measured against the branch it
    # merged into: everything the merge lands here is in scope. That content is
    # still refusable at this moment — the merge can be redone — and it is the
    # only moment before it becomes somebody else's history.
    if parent=$(git rev-parse -q --verify 'HEAD^1' 2>/dev/null) && [ -n "$parent" ]; then
        base="$parent"
        scope_note='the commit just made'
    else
        base="$EMPTY_TREE"
        scope_note='the root commit, read whole'
    fi
    git diff --text --no-color --no-ext-diff --no-textconv "$base" HEAD >"$diff_file" 2>"$work/err" \
        || fail_hard "the diff for HEAD could not be read: $(cat "$work/err")"
    git diff --name-only -z --no-color --diff-filter=ACMRT "$base" HEAD >"$names_file" 2>"$work/err" \
        || fail_hard "the file list for HEAD could not be read: $(cat "$work/err")"
    ;;
range)
    git rev-parse --verify HEAD >/dev/null 2>&1 \
        || fail_hard "there is no HEAD to scan, and a scan that reads nothing is not a pass"
    base=''
    if ! git cat-file -e "${BASELINE}^{commit}" 2>/dev/null; then
        scope_note="the baseline ${BASELINE:0:9} is not in this repository, so only the tip was read"
    elif ! git merge-base --is-ancestor "$BASELINE" HEAD 2>/dev/null; then
        scope_note="this history does not descend from the baseline ${BASELINE:0:9}, so only the tip was read"
    elif [ -z "$(git rev-list -n 1 "${BASELINE}..HEAD")" ]; then
        scope_note="HEAD is the baseline, so only the tip was read"
    else
        base="$BASELINE"
        scope_note="every line this branch adds on top of ${BASELINE:0:9}"
    fi
    # THE FALLBACK NARROWS THE SCOPE AND CAN THEN PASS. Say plainly what it
    # does, because the sentence that used to sit here said the opposite. It
    # said the fallback "still REFUSES". It does not refuse. It reads HEAD^1 to
    # HEAD — usually the tip commit alone, the whole merged side when the tip is
    # a merge, and the whole tree when HEAD has no parent — and if that is clean
    # it prints clean and
    # exits 0, whatever the rest of the tree holds. Measured in a scratch
    # repository: a real key in the tree, an unresolvable baseline and a clean
    # tip reads ONE line and exits 0; the same tree with a baseline that
    # resolves blocks. A comment stating an invariant that nothing enforces is
    # the defect, not a description of one.
    #
    # WHERE THIS BITES. The baseline below is NOT an ancestor of main
    # (`git merge-base --is-ancestor 0743e4dec main` returns 1, measured
    # 2026-08-12). So on main, and on any lane branched from main, `--range`
    # silently narrows to HEAD^1..HEAD and reports clean. Measured on
    # this branch on 2026-08-12: the wide scope reads over two hundred thousand
    # added lines and the same command with an unresolvable baseline reads a few
    # hundred. Neither figure is quoted exactly, because both move with every
    # commit — the tip fallback especially, since it is one commit's worth and
    # was measured at two very different values hours apart on the same day. The
    # ORDER OF MAGNITUDE between them is the point, and that is what the two
    # phrases above are for. The scope stays wide only on a branch that
    # descends from the
    # baseline.
    #
    # A scope of nothing would be worse still — that is the failure this whole
    # change is about — so the three cases above fall back to the tip rather
    # than to an empty scan, and the scope_note above says which case was hit.
    # Choosing between refusing outright and narrowing is a real decision and it
    # is not made here: bead hk-254ea is the record of the gap.
    if [ -z "$base" ]; then
        base=$(git rev-parse -q --verify 'HEAD^1' 2>/dev/null) || base=''
        [ -n "$base" ] || base="$EMPTY_TREE"
    fi
    git diff --text --no-color --no-ext-diff --no-textconv "$base" HEAD >"$diff_file" 2>"$work/err" \
        || fail_hard "the diff over ${base:0:9}..HEAD could not be read: $(cat "$work/err")"
    git diff --name-only -z --no-color --diff-filter=ACMRT "$base" HEAD >"$names_file" 2>"$work/err" \
        || fail_hard "the file list over ${base:0:9}..HEAD could not be read: $(cat "$work/err")"
    ;;
esac

# EVERY NUL IN THE CONTENT DIFF BECOMES A SPACE BEFORE THE PARSER READS IT, and
# that is what makes the answer the same under the three awks that were
# compared: one-true-awk, gawk and mawk. It closes the NUL divergence. It does
# not establish that no other awk divergence exists — bead
# hk-awk-dependent-gate-verdict-i80cv is the record of that, and the LC_ALL=C
# paragraph below is a second cross-machine divergence found the same week.
#
# --text above hands the parser a binary blob's raw bytes. The awk this project
# runs on macOS ends a record at the first NUL, so everything after it is gone:
# a scratch commit adding `HEADER<NUL><3 control bytes><a real key>` printed
# "clean — 1 added line(s) read" and exit 0 with the key at HEAD, while GNU Awk
# and mawk kept the whole record and would have refused it. Measured 2026-08-12.
#
# A SPACE, NOT A DELETION. Deleting the NULs would join the bytes on either side
# of one, which can build a match out of two unrelated fragments and refuse a
# commit that carries no credential. A space keeps every byte in the position it
# had, so a pattern matches only where the bytes really sit next to each other.
#
# "THE SAME ANSWER" IS NOT "FOUND", and the difference has to be said here or
# this comment reads as a claim that binary content is now scannable. It is not.
# The transform makes every machine agree; it does not make every shape match.
# A key that sits CONTIGUOUSLY after one NUL becomes readable, which is the
# shape case 44 pins. A key stored in UTF-16 has a NUL between every character,
# so the transform turns it into single letters separated by spaces and no
# pattern above matches — measured 2026-08-12: a UTF-16 file holding a key reads
# "clean — 2 added line(s) read" and exits 0, on this machine and on a GNU box
# alike. That was true before this change too and is not made worse by it, but
# the agreed answer for that shape is "clean", and a reader must not take the
# guarantee for more than it is. It is filed as its own gap.
#
# NOT A NEWLINE either, and this one would be a real defect. The parser counts
# lines against the count each hunk header declares. An extra newline inside a
# hunk moves every following line, so the reported locations go wrong and the
# parser can leave the hunk early.
#
# IT ALSO KEEPS THE REPORTING HONEST. grep answers "Binary file … matches" with
# no line number when the file it reads holds a NUL — measured with BSD grep and
# with GNU grep 3.12 — and the redaction loop below reads a line number out of
# that output. With no NUL left in the added lines, both greps report a real
# line number and the loop names a place to look.
#
# THE COST, MEASURED TWO WAYS on 2026-08-12. As a pass on its own it reads and
# writes the whole diff once: 0.8 seconds over the 15.6 MB the --range scope
# produces on this branch. End to end it moved the whole --range scan from about
# 4.4 seconds to about 5.4 seconds over four interleaved pairs of runs, one with
# the transform and one without. `make full` is about 338 seconds, so this is
# under half a percent of the merge decision. It is also a no-op on this tree's
# real content — that same 15.6 MB holds zero NUL bytes — so the second is what
# it costs, and what it buys is agreement across those three awks.
#
# LC_ALL=C IS LOAD-BEARING, and without it this line is the gate's own outage.
# BSD tr — /usr/bin/tr on macOS — reads its input as characters of the current
# locale, and under LANG=en_US.UTF-8, the default on a developer box here, it
# refuses any byte sequence that is not valid UTF-8: rc 1 and "tr: Illegal byte
# sequence". Measured 2026-08-12: 4096 bytes of /dev/urandom give rc 1 in the
# default locale and rc 0 under LC_ALL=C. --text above exists to put a binary
# blob's raw bytes into this file, so ANY commit that adds an ordinary binary
# file — a PNG, a test fixture, a compiled artifact — reaches this line with
# bytes no UTF-8 decoder accepts. Measured end to end before this flag was
# added: a scratch commit adding 8 KB of random bytes made --head-only print
# "BLOCKED — the scan could not run — the diff could not be read" and exit 1.
# It fails CLOSED, so it is not a leak. It is the other failure this file keeps
# naming: GNU tr does not do this, so the gate went red on macOS and green on
# the Linux runner, and the two machines disagreed about what the gate means.
# LC_ALL=C makes tr read bytes, which is what it is being asked to transform.
# Case 45 is the fixture.
LC_ALL=C tr '\0' ' ' <"$diff_file" >"$scan_diff" \
    || fail_hard "the scan could not run — the diff could not be read."

# THE FILE LIST IS NUL DELIMITED, and that is a fail-open fix rather than tidy
# quoting. With git's default core.quotepath, `git diff --name-only` renders a
# path that holds a non-ASCII or control character as a C quoted string —
# "caf \303\251/.env" — and the trailing quote defeats a pattern that anchors
# .env at the end of a line. A .env under a directory with an accent in its name
# therefore read as clean. `-z` turns the quoting off and ends each path with a
# NUL, so what arrives is the real path.
#
# The NULs become newlines here so the match below can read a file. A path may
# legally hold a newline, which splits one path across two lines; the .env match
# anchors on a path COMPONENT, so a split can add a candidate line but cannot
# hide the `.env` component from the pattern.
#
# The list is also filtered ACMRT rather than ACMR. T is a typechange — a .env
# that arrives as a symlink where a regular file was, or the reverse. Dropping T
# produced an EMPTY name list for such a commit, which reads exactly like a
# clean one. D stays out on purpose: a DELETED .env is good news.
#
# LC_ALL=C for the same reason as the transform above, on an input that is
# rarer but not hypothetical. -z turns off git's C-quoting, so a path whose
# bytes are not valid UTF-8 — a filename carried in from a filesystem with
# another encoding — arrives here raw, and BSD tr in a UTF-8 locale would
# refuse the whole file list and abort the scan. No fixture covers this one:
# committing such a path is awkward to do portably, so the flag is here on the
# argument rather than on a measurement, and this sentence is the record of
# which of the two it is.
LC_ALL=C tr '\0' '\n' <"$names_file" >"$names_lines" \
    || fail_hard "the scan could not run — the file list could not be read."

# NO --diff-filter ON THE CONTENT DIFF, and that is not tidiness either. It used
# to read `--diff-filter=ACM`, which drops a RENAMED file (R) from the diff
# entirely. Rename a file and add a key inside it in the same commit and the
# scan saw an empty diff and said clean — measured while writing the committed
# scopes below, in a scratch repository, with the key sitting in the tree. Every
# change type can carry an added line, so no change type is filtered out here.
# The FILE list further down keeps a filter, because a DELETED .env is good news
# rather than a finding.
#
# THE PARSER READS A FILE, and that is the whole usefulness of the scan rather
# than a style choice. The earlier form was `echo "$DIFF" | grep -qE …`. `grep -q`
# leaves the moment it matches; the writer feeding it then takes SIGPIPE; and
# under `pipefail` — set at the top of this file — the pipeline reports the
# WRITER's death as its own status. So the pipeline returned non-zero on the
# commits that DID contain a secret, and this scan read that as "no match" and
# let them through. Measured: a 40-byte staged diff carrying a fake key was
# blocked, and the same key inside a 527 KB staged diff — an ordinary commit —
# passed, five times out of five.
#
# Do not read a size out of this as a safe threshold. It is a race between the
# writer and the reader, and the same command on the same input was measured
# giving different answers at 24 KB and at 32 KB in the same session. Small does
# not mean safe; it means the race is usually won. A file has no writer process
# to kill, and --range now reads 20 MB of diff, so nothing here may depend on
# winning that race.
#
# ADDED LINES, AND WHERE EACH ONE CAME FROM, READ IN ONE PASS.
#
# added_file is a flat list of added lines with no path and no line number in it,
# so the only way to report a finding would be to print the matching line — that
# is, to copy the credential into a CI log, which more people can read than the
# tree it leaked into. loc_file holds one "path:line" for each line of
# added_file, in the same order, so a finding is reported as a place to look
# instead of as a value.
#
# THEY ARE BUILT BY THE SAME RULE IN THE SAME PASS, and that is the fix for a
# real fail-open rather than a tidy-up. The two used to be derived separately:
# added_file came from a grep for lines starting with '+' followed by a second
# grep that dropped lines starting with '+++', and loc_file came from an awk
# program carrying its own rule for a '+++' line. Both rules read the first
# characters of a line and GUESSED what the line was.
#
# THE GUESS IS WRONG ON REAL CONTENT. A source line that itself begins with two
# plus signs renders in a diff as '+++…' — the diff's own marker, then the line's
# own two — so the header filter deleted it before any pattern could see it. A
# committed file holding two plus signs and a key on one line read CLEAN in every
# scope, exit 0, "0 added line(s) read", with the key sitting at HEAD. Eleven
# tracked files in this repository carry such lines today, seven of them
# committed .patch and .diff dumps — which is exactly where a key lands by
# accident. The same guess also corrupted the index: it took the line's own text
# for a path and skipped the line counter, so the few findings it did report
# named a file that does not exist, one line off.
#
# SO THE POSITION IS TRACKED, NOT GUESSED. '+++ b/path' is a file header only
# when it comes straight after a '--- a/path' line with no hunk open. A hunk
# header says how many old and new lines the hunk holds, so the parser knows
# where the hunk ENDS instead of inferring it from the next line's first
# characters. Inside a hunk the first byte is the marker and the rest is content,
# whatever it looks like — so a file whose own text holds '---', '+++', '@@',
# 'diff --git' or two plus signs is read correctly, and its line numbers stay
# right.
#
# The count check after the parser asserts that the two outputs are still the
# same length. It is a second line of defence and no test reaches it — see the
# next paragraph, which is the honest statement and which an earlier version of
# this one contradicted by calling the same check "not a formality".
#
# What that earlier version was really evidence for is the REWRITE, not the
# check: a reviewer mutated the OLD two-rule index to disagree with the
# added-lines file and the suite of the day stayed green throughout, which is
# why the two outputs are now produced by one rule in one pass instead of being
# reconciled afterwards.
#
# THREE BRANCHES BELOW ARE A SECOND LINE OF DEFENCE, AND NO TEST REACHES THEM.
# Say so rather than let a reader take them for measured behaviour. They are the
# `saw_minus` guard on the '+++' header rule, the '+'-with-no-hunk-open rule, and
# the count check after the parser. Each was mutated on its own and the suite
# stayed green, because git's diff output — WITH --no-color, --no-ext-diff and
# --no-textconv in force, which is the only form this script reads — does not
# produce the input that would reach them. The qualifier is load-bearing: with
# colour forced on, git emits '+'-bearing lines while the parser never opens a
# hunk at all, and it is the escape sitting in front of the '+' rather than any
# property of the marker that keeps those branches unreached. In the pinned
# form, a '+' line only ever appears inside a hunk or as a paired
# '--- '/'+++ ' header, and `git diff --cached` prints "* Unmerged path" rather
# than a combined diff for a conflicted index, so the two-column combined format
# never arrives here either. They are kept because the FIRST line — the hunk
# counter — is what the suite pins, and if that ever loses its place these three
# make the result a refusal or a wrong location rather than a dropped line. A
# wrong location is a nuisance; a dropped line is a leak.
#
# The two filenames arrive through the environment rather than through `awk -v`,
# because -v processes backslash escapes in the value it is given and a path is
# not an escape sequence.
ADDED_FILE="$added_file" LOC_FILE="$loc_file" awk '
    function clean_path(p) {
        # git appends a TAB and a timestamp field when the path holds a space,
        # and wraps the whole thing in quotes when it holds a byte outside
        # ASCII. Both were in the first output this index produced.
        sub(/\t.*$/, "", p)
        if (p ~ /^".*"$/) { p = substr(p, 2, length(p) - 2) }
        return p
    }
    function emit(content, where) {
        print content > A
        print where > L
    }
    BEGIN {
        A = ENVIRON["ADDED_FILE"]
        L = ENVIRON["LOC_FILE"]
        printf "" > A
        printf "" > L
        path = ""; minus_path = ""; saw_minus = 0
        in_hunk = 0; oldrem = 0; newrem = 0; newline = 0
    }
    in_hunk == 1 {
        marker = substr($0, 1, 1)
        if (marker == "+") {
            emit(substr($0, 2), (path == "" ? "?" : path) ":" newline)
            newline++; newrem--
        }
        else if (marker == "-") { oldrem-- }
        else if (marker == "\\") { }   # "\ No newline at end of file"
        else { oldrem--; newrem--; newline++ }   # context, including an empty one
        if (oldrem <= 0 && newrem <= 0) { in_hunk = 0 }
        next
    }
    # Below here no hunk is open, so nothing that follows can be file content.
    substr($0, 1, 3) == "@@ " {
        body = substr($0, 4)
        at = index(body, " @@")
        if (at > 0) { body = substr(body, 1, at - 1) }
        parts = split(body, R, " ")
        oldrem = 1; newrem = 1; newline = 1
        for (i = 1; i <= parts; i++) {
            side = substr(R[i], 1, 1)
            span = substr(R[i], 2)
            comma = index(span, ",")
            if (side == "-") {
                oldrem = (comma > 0 ? substr(span, comma + 1) + 0 : 1)
            }
            else if (side == "+") {
                newline = (comma > 0 ? substr(span, 1, comma - 1) + 0 : span + 0)
                newrem  = (comma > 0 ? substr(span, comma + 1) + 0 : 1)
            }
        }
        if (oldrem > 0 || newrem > 0) { in_hunk = 1 }
        saw_minus = 0
        next
    }
    substr($0, 1, 11) == "diff --git " {
        path = ""; minus_path = ""; saw_minus = 0
        next
    }
    substr($0, 1, 4) == "--- " {
        minus_path = clean_path(substr($0, 5))
        sub(/^a\//, "", minus_path)
        saw_minus = 1
        next
    }
    substr($0, 1, 4) == "+++ " && saw_minus == 1 {
        path = clean_path(substr($0, 5))
        sub(/^b\//, "", path)
        # A deleted file has no added lines, but a mangled stanza might; name the
        # old path rather than /dev/null.
        if (path == "/dev/null" && minus_path != "") { path = minus_path }
        saw_minus = 0
        next
    }
    substr($0, 1, 1) == "+" {
        emit(substr($0, 2), (path == "" ? "?" : path) ":?")
        saw_minus = 0
        next
    }
    { saw_minus = 0 }
' "$scan_diff" \
    || fail_hard "the scan could not run — the diff could not be parsed."

# THE COUPLING, ASSERTED. Two files that must stay in step are still two files.
# If they ever differ in length the parser is broken, every reported location
# after the first divergence is wrong, and — worse — the divergence would mean
# lines went missing from the content scan. Refuse rather than report.
added_count=$(awk 'END { print NR + 0 }' "$added_file") \
    || fail_hard "the scan could not run — the added lines could not be counted."
loc_count=$(awk 'END { print NR + 0 }' "$loc_file") \
    || fail_hard "the scan could not run — the location index could not be counted."
if [ "$added_count" != "$loc_count" ]; then
    fail_hard "the scan could not run — the diff parser produced ${added_count} added line(s) but ${loc_count} location(s)."
fi

found=0

# `-e` is load-bearing, not tidiness. The private-key pattern starts with five
# dashes, so without `-e` grep reads it as a bundle of options, prints
# "unrecognized option", and exits 2 — and `if` reads 2 as no-match. That
# pattern therefore matched nothing for the whole life of this script, and the
# `2>/dev/null` that used to sit on this line hid the message that said so. A
# staged RSA private key went straight through at any size.
#
# The three exit statuses are told apart for the same reason. Folding 2 into "no
# secret found" is how the defect above stayed invisible, so an error blocks and
# says what it was.
for pattern in "${SECRET_PATTERNS[@]}"; do
    grep_err=$(grep -qE -e "$pattern" "$added_file" 2>&1)
    case "$?" in
        0)
            echo "secret-scan: BLOCKED — potential secret matches pattern: ${pattern}"
            echo "  scope: ${scope_note}"
            # THE MATCHING LINE IS NOT PRINTED. This output goes to a CI log,
            # and a CI log has a wider audience than the tree the value sits in,
            # so printing the line publishes the credential further than the
            # leak did. A path, a line number and the first few characters are
            # enough to find it and not enough to use it.
            grep -nE -e "$pattern" "$added_file" >"$work/hits" 2>/dev/null
            shown=0
            while IFS= read -r hit; do
                [ "$shown" -lt 5 ] || { echo "    … more of the same pattern, not listed"; break; }
                hit_line=${hit%%:*}
                sed -n "${hit_line}p" "$added_file" >"$work/one"
                frag=$(grep -oE -e "$pattern" "$work/one" 2>/dev/null)
                frag=${frag%%$'\n'*}
                where=$(sed -n "${hit_line}p" "$loc_file")
                [ -n "$where" ] || where="(location unknown)"
                echo "    ${where}: [REDACTED ${#frag} chars, starts '${frag:0:4}']"
                shown=$((shown + 1))
            done <"$work/hits"
            found=1
            ;;
        1) ;;
        *)
            echo "secret-scan: BLOCKED — the scan could not run for pattern: ${pattern}"
            echo "  grep said: ${grep_err}"
            echo "  This is not a clean result. Fix the scan before committing."
            found=1
            ;;
    esac
done

# Block if any .env-family file is in scope for addition or modification.
#
# The `|| true` this line used to carry folded grep's exit 2 — "I could not do
# the job" — into "no .env file in scope". That is a negative assertion that can
# never fail, which is the shape the comments above refuse for the content scan,
# so it is refused here too. 0 is a finding, 1 is a clean answer, and anything
# else is a broken scan and blocks.
ENV_FILES=$(grep -E '(^|\/)\.env($|\.|\-)' "$names_lines")
case "$?" in
    0)
        echo "secret-scan: BLOCKED — .env file(s) in scope (${scope_note}):"
        echo "$ENV_FILES" | sed 's/^/  /'
        echo "  Env files may contain secrets. Add them to .gitignore."
        found=1
        ;;
    1) ;;
    *)
        echo "secret-scan: BLOCKED — the scan could not run — the file list could not be searched for .env files."
        echo "  This is not a clean result. Fix the scan before committing."
        found=1
        ;;
esac

if [ "$found" -eq 1 ]; then
    exit 1
fi

# Say what was read. A gate that prints nothing on success cannot be told apart
# from a gate that was never reached, and being unable to tell those apart is
# the defect this file was changed to fix.
if [ "$MODE" != "staged" ]; then
    # The count is a LINE count, not `grep -c .`. An added line can legitimately
    # be empty now that the diff's own '+' marker is stripped from it, and a
    # count that skipped those would under-report what was read.
    echo "secret-scan: clean — ${added_count} added line(s) read over ${scope_note}"
fi
