#!/usr/bin/env bash
# required-check-name-gate.sh — the CI job that gates a merge must keep the
# name branch protection asks for, and must still be able to report it.
#
# WHY THIS EXISTS. Branch protection on `main` requires one status context.
# A GitHub job reports under its `name:`, so the name is an identifier, not a
# label. Rename the job and nothing reports the required context again — and
# GitHub reads a required context that never arrives as PENDING, not as
# failing. Every later pull request to main then waits forever on a check that
# cannot come. The gate does not go red. It stops existing while it keeps
# blocking, which is the failure that is hardest to read.
#
# This already happened here. A commit renamed the job to `make full`, which
# describes what the job runs and was in every other way an improvement. It
# would have wedged main the moment the branch landed. Nothing caught it,
# because nothing anywhere said the name was load-bearing.
#
# THE NAME IS NOT THE ONLY WAY TO LOSE THE CONTEXT. A job keeps the right name
# and still never reports the bare context if it gains a build matrix (the
# context becomes `check (Tier 2) (1.22)`), if the workflow stops triggering on
# pull requests, or if a path filter means the run is skipped for most pull
# requests. And the context can arrive while meaning nothing: a decoy job can
# carry the name while the real work moved elsewhere. So this gate checks that
# ONE job, in some workflow, satisfies all of it at once. Each rule is listed
# under WHAT IT CHECKS below.
#
# WHAT IT CANNOT DO. It compares the workflows against the strings recorded
# below. It does not read the live protection setting — that needs network and
# a token, neither of which a static gate has. So the two can still drift if
# somebody edits protection and not this file. Change them together, and change
# these strings in the same commit. Read protection back with:
#   gh api repos/:owner/:repo/branches/main/protection --jq '.required_status_checks.contexts'
#
# WHAT THE PARSER UNDERSTANDS. `yq` is not installed here and is not going to
# become a dependency of a merge gate, so this file carries its own small YAML
# reader in Python 3. Say plainly what it does, because a parser that quietly
# mis-reads a workflow is worse than a grep that admits it is a grep.
#
#   It reads: block mappings and block sequences at any depth; plain, single-
#   quoted and double-quoted scalars; literal (`|`) and folded (`>`) block
#   scalars with `-`/`+` chomping and an explicit indent digit 1-9; flow
#   sequences `[a, b]` and flow mappings `{a: b}`; `#` comments; a single
#   leading `---`.
#   It resolves `true`/`false` to booleans and `null`/`~`/empty to nothing, and
#   leaves every other scalar a string. It deliberately does NOT resolve the
#   key `on` to the boolean true, which YAML 1.1 does and which would hide
#   every trigger in every workflow behind a key nobody would think to read.
#
#   It does NOT read: anchors, aliases, merge keys, explicit tags, more than
#   one document per file, quoted scalars that continue onto a second line, or
#   tabs used as structural indentation. Inside a block scalar a tab is CONTENT
#   once the scalar's content indent is fixed and the tab sits at or past it, so
#   a `run: |` step that carries a Makefile recipe or a heredoc body is read,
#   not refused. A tab LEFT of that column is indentation — and so is a tab on
#   the FIRST content line of a scalar whose indent is not fixed yet, because
#   the column that line sets is measured in spaces and a tab sets nothing.
#   libyaml refuses both ("found a tab character where an indentation space is
#   expected"), so GitHub refuses the whole workflow, no run starts, and the
#   required context is pending forever — the wedge itself, arriving through a
#   file this gate would otherwise have called fine. An explicit indicator
#   (`|2`) fixes the indent in the header before any content line is read, so a
#   tab on the first content line of one of THOSE is content and is read.
#
#   It REFUSES the file when it meets a construct on the list above. Failing
#   closed on a construct the reader does not know is the whole point: the old
#   gate passed a file that was not valid YAML at all. A folded scalar over
#   several lines is folded approximately; job names are one line, so this has
#   never mattered, but it is not a general YAML parser and must not be reused
#   as one.
#
#   It also refuses a duplicate key in one mapping, because GitHub does. Two
#   `jobs:` blocks in one file is what a badly resolved merge conflict looks
#   like, GitHub rejects the whole workflow, no run starts, and the required
#   context is then pending forever. Reading it as last-wins would make this
#   gate's answer depend on which copy came last.
#
#   THE READER MUST ALWAYS FINISH. This gate runs inside `make full`, which is
#   the merge decision, and `ci.yml` declares no `timeout-minutes`, so a hang
#   here stops every developer and sits in CI until GitHub's six-hour default.
#   The flow-collection scanner used to return with its index unchanged on
#   input like `[a:b]`, `[a}` or `run: [[ -n "$X" ]] && make full`, and spun
#   forever. Every one of those is a syntax error to a real YAML parser too, so
#   `scan_flow` now refuses any character it cannot get past, and treats an
#   iteration that consumed nothing as a parse error rather than a loop.
#
#   Finishing is about time, not only about loops. `scan_plain_key` is a scan
#   and not a regular expression for that reason: the pattern it replaced
#   (`^([^\s:#][^:#]*?)\s*:(?=\s|$)`) let two of its parts match the same run
#   of whitespace, so a line carrying a long run of it and no `key:` took time
#   quadratic in the length of that line — 20 KB of it ran for 0.9s, 40 KB for
#   3.7s, and 300 KB did not finish inside ten seconds. A scan reads each
#   character once. Depth is bounded by Python's own recursion limit, and a
#   file that nests past it is refused with a reason like every other refusal
#   here, not with a traceback.
#
# WHAT ONE UNREADABLE FILE DOES. It does not, on its own, redden the gate. A
# workflow the reader refuses is named on stderr and set aside; if some OTHER
# workflow still declares a job that satisfies every rule below, the required
# context arrives and the gate agrees. The refusal is scoped this way because
# an anchor in an unrelated dependabot-style workflow used to redden a gate
# whose subject is one job in `ci.yml`, and a gate that goes red for reasons
# unrelated to its subject is one people learn to route around. Two things keep
# it closed: an unreadable file is fatal when nothing else carries the context
# (it might have been the file meant to), and it is fatal even beside a healthy
# workflow when its raw text mentions REQUIRED_CONTEXT, because then it might
# be a second job under the same name and the gate cannot tell which check run
# GitHub would grade.
#
# WHAT IT CHECKS. Some job, in some workflow file under the scanned root, must
# satisfy every one of these:
#   1. Its reported context equals REQUIRED_CONTEXT. The context is the job's
#      `name:`, or the job's key when it declares no name — which is what
#      GitHub does.
#   2. Its workflow has a `pull_request` trigger. `push` alone is refused even
#      though it does produce a status for same-repo branches, because it
#      produces nothing for a pull request from a fork and it is not what
#      branch protection is built on.
#   3. That trigger carries no `paths:` / `paths-ignore:`. A skipped workflow
#      leaves a required context pending forever, which is the wedge itself.
#   4. That trigger's `branches:` / `branches-ignore:` still admit the
#      protected branch. On `pull_request` those filter the BASE branch.
#   5. That trigger's `types:`, if it declares one, still includes `opened` and
#      `synchronize`, or the check never runs on an open pull request.
#   6. The job declares no `strategy: matrix:`. A matrix job reports
#      `NAME (value)` per leg and never the bare NAME.
#   7. The job declares no job-level `if:`. The gate cannot evaluate an Actions
#      expression, and a conditional required job either fails to report or
#      reports without doing the work. Neither is what protection bought.
#   8. Neither the job nor the step that runs REQUIRED_COMMAND carries
#      `continue-on-error`, which reports success on every REST surface after a
#      real failure. The STEP is checked because that is where every instance
#      of this in this repo's history was written — on ci.yml's own step, and
#      on the scenario.yml step that reported 20 straight real failures as
#      green. A job-level-only rule would have passed all of them.
#   9. The job runs REQUIRED_COMMAND in one of its steps, and that step carries
#      no `if:` either. This is what tells a real gate from a decoy that
#      carries the name and runs `true`, and it is checked at the step level
#      as well because otherwise rule 7 is defeated by moving the `if:` down
#      one line. It is a substring match on the step's `run:`, so a workflow
#      that only MENTIONS the command in an echo satisfies it. The gate says
#      the command appears; it cannot say the command does anything.
#  10. Every job in that workflow declares `runs-on:` (or delegates with
#      `uses:`). This is the one INVALIDITY check. The nine rules above all
#      describe ways a VALID workflow loses the context; a workflow GitHub
#      refuses loses it just as completely, because no run starts and no check
#      run is ever created. One bad job kills the whole file, so a sibling job
#      counts.
#  11. No job the job `needs:`, at any depth, carries a job-level `if:`, and
#      every job it needs exists. A skipped dependency skips this job, GitHub
#      reports the required check as SKIPPED, and branch protection reads a
#      skipped required check as satisfied. That is rule 7's failure one hop
#      away, and it ends with a merge nobody decided on.
#
# A workflow file that holds no YAML mapping at all — empty, or nothing but
# comments — is refused with the rest, for the same reason: the gate cannot
# vouch for a file it cannot read jobs out of.
#
# Exit 0 = agree; exit 1 = drift.

set -uo pipefail

# REQUIRED_CONTEXT — the exact status context branch protection on `main`
# requires. REQUIRED_COMMAND — the merge decision the job carrying that context
# has to actually run. PROTECTED_BRANCH — the branch whose protection this
# pairing describes. Read the live setting back with the gh api command in the
# header above.
REQUIRED_CONTEXT='check (Tier 2)'
REQUIRED_COMMAND='make full'
PROTECTED_BRANCH='main'

# The workflow root is overridable so the self-test can point this gate at
# deliberately-broken copies and watch it go red. Without that seam the only
# way to prove the gate can fail is to break the real workflow files, and a
# gate nobody has ever seen fail is not evidence of anything. It takes a
# directory (every workflow in it is scanned — a second workflow could
# legitimately be the one reporting the context) or a single file.
WORKFLOW_ROOT="${REQUIRED_CHECK_WORKFLOW:-.github/workflows}"

fail() {
  echo "required-check-name-gate: FAIL — $*" >&2
  exit 1
}

files=()
if [ -d "$WORKFLOW_ROOT" ]; then
  while IFS= read -r f; do
    files+=("$f")
  done < <(find "$WORKFLOW_ROOT" -maxdepth 1 -type f \( -name '*.yml' -o -name '*.yaml' \) | sort)
  if [ "${#files[@]}" -eq 0 ]; then
    fail "$WORKFLOW_ROOT holds no .yml or .yaml workflow file, so nothing can report the required context '$REQUIRED_CONTEXT'"
  fi
elif [ -f "$WORKFLOW_ROOT" ]; then
  files=("$WORKFLOW_ROOT")
else
  fail "$WORKFLOW_ROOT is missing, so nothing can report the required context '$REQUIRED_CONTEXT'"
fi

# No `producer | grep -q PATTERN` anywhere in this file. grep -q exits at the
# first match, the writer takes SIGPIPE, and pipefail reports the writer's
# death as the pipeline's status — so a match reads as a failure. That shape
# kept this repo's merge gate red for days. scripts/pipefail-grepq-gate.sh
# owns that rule for the whole tree and scans this file, so it is not
# re-asserted in this gate's own self-test.
GATE_REQUIRED_CONTEXT="$REQUIRED_CONTEXT" \
GATE_REQUIRED_COMMAND="$REQUIRED_COMMAND" \
GATE_PROTECTED_BRANCH="$PROTECTED_BRANCH" \
python3 - "${files[@]}" <<'PYTHON_EOF'
import os
import re
import sys

REQUIRED_CONTEXT = os.environ["GATE_REQUIRED_CONTEXT"]
REQUIRED_COMMAND = os.environ["GATE_REQUIRED_COMMAND"]
PROTECTED_BRANCH = os.environ["GATE_PROTECTED_BRANCH"]

PREFIX = "required-check-name-gate"


class YamlError(Exception):
    pass


_UNSUPPORTED_START = re.compile(r'^[&*!]')
_COMMENT_SPLIT = re.compile(r'(?:^|\s)#')


def scan_plain_key(body):
    """The `key:` half of a plain `key: value` line, or (None, None).

    A key runs up to the first `:` that a space or the end of the line
    follows, carries no `:` or `#` of its own, and does not open with
    whitespace. That is exactly the language the regular expression here used
    to describe, and this reads it in one pass. The regular expression could
    not: `[^:#]*?` and `\\s*` both matched whitespace, so every space in a long
    run was another split point to try, and a line with no `key:` on it cost
    time quadratic in its length. This gate is inside the merge decision.
    """
    if not body or body[0].isspace() or body[0] in ":#":
        return None, None
    for i, c in enumerate(body):
        if c == "#":
            return None, None
        if c == ":":
            if i + 1 < len(body) and not body[i + 1].isspace():
                return None, None
            return body[:i], body[i + 1:]
    return None, None


def resolve_plain(text):
    """Scalar resolution for VALUES only. Keys keep their literal text, which
    is how `on:` stays the string "on" instead of YAML 1.1's boolean true."""
    low = text.lower()
    if text == "" or text == "~" or low == "null":
        return None
    if low == "true":
        return True
    if low == "false":
        return False
    return text


class Reader(object):
    def __init__(self, text, path):
        self.lines = text.split("\n")
        self.path = path
        self.i = 0

    def err(self, msg, line=None):
        n = self.i if line is None else line
        raise YamlError("line %d: %s" % (n + 1, msg))

    def at_end(self):
        return self.i >= len(self.lines)

    def skip_blank(self):
        while self.i < len(self.lines):
            s = self.lines[self.i]
            if s.strip() == "" or s.lstrip(" ").startswith("#"):
                self.i += 1
            else:
                return

    def indent_width(self, line):
        """Column of the first character that is not a space. No tab check:
        this is the measurement block-scalar CONTENT needs, where a tab past
        the content indent is ordinary text — a Makefile recipe, a heredoc
        body — and legal YAML. `indent_of` is the structural measurement."""
        s = self.lines[line]
        return len(s) - len(s.lstrip(" "))

    def indent_of(self, line):
        s = self.lines[line]
        width = self.indent_width(line)
        if s[width:width + 1] == "\t":
            self.err("a tab is used for indentation; YAML forbids it and this "
                     "reader will not guess what it meant", line)
        return width

    # ---- documents -----------------------------------------------------
    def parse_document(self):
        self.skip_blank()
        if not self.at_end() and self.lines[self.i].rstrip() == "---":
            self.i += 1
        for j in range(self.i, len(self.lines)):
            s = self.lines[j].rstrip()
            if s == "---" or s.startswith("--- ") or s == "...":
                self.err("more than one YAML document in one workflow file; "
                         "this reader handles one", j)
        self.skip_blank()
        if self.at_end():
            return None
        node = self.parse_block(self.indent_of(self.i))
        self.skip_blank()
        if not self.at_end():
            self.err("content continues after the end of the top-level block")
        return node

    def parse_block(self, indent):
        self.skip_blank()
        if self.at_end():
            return None
        cur = self.indent_of(self.i)
        if cur < indent:
            return None
        body = self.lines[self.i][cur:]
        if body == "-" or body.startswith("- "):
            return self.parse_seq(cur)
        return self.parse_map(cur)

    # ---- mappings ------------------------------------------------------
    def parse_map(self, indent):
        out = {}
        while True:
            self.skip_blank()
            if self.at_end():
                break
            cur = self.indent_of(self.i)
            if cur < indent:
                break
            if cur > indent:
                self.err("indentation increases where a mapping key was expected")
            body = self.lines[self.i][cur:]
            if body == "-" or body.startswith("- "):
                self.err("a sequence item appears where a mapping key was expected")
            key, rest, ok = self.split_key(body)
            if not ok:
                self.err("this line is not a mapping key and this reader does "
                         "not understand it")
            if key in out:
                # GitHub refuses a workflow with a duplicate key outright:
                # "'jobs' is already defined". No run starts, no check run is
                # created, and the required context is pending forever. Reading
                # it as last-wins would make this gate's answer depend on which
                # copy of a badly merged block happened to come second.
                self.err("the key '%s' is defined twice in one mapping; GitHub "
                         "refuses the whole workflow, so no run starts and no "
                         "check run is ever created" % key)
            self.i += 1
            out[key] = self.parse_value(rest, indent)
        return out

    def split_key(self, body):
        if body[0] in "\"'":
            value, idx = self.scan_quoted(body, 0)
            after = body[idx:]
            if not after.startswith(":"):
                return None, None, False
            return value, after[1:], True
        key, rest = scan_plain_key(body)
        if key is None:
            return None, None, False
        return key.strip(), rest, True

    def parse_value(self, rest, indent):
        s = rest.strip()
        if s == "" or s.startswith("#"):
            return self.parse_nested(indent)
        if s[0] in "|>":
            return self.parse_block_scalar(s, indent)
        return self.parse_inline_scalar(s, self.i - 1)

    def parse_nested(self, indent):
        self.skip_blank()
        if self.at_end():
            return None
        cur = self.indent_of(self.i)
        body = self.lines[self.i][cur:]
        if cur > indent:
            return self.parse_block(cur)
        # A block sequence may sit at the same indent as the key that owns it.
        if cur == indent and (body == "-" or body.startswith("- ")):
            return self.parse_seq(indent)
        return None

    # ---- sequences -----------------------------------------------------
    def parse_seq(self, indent):
        items = []
        while True:
            self.skip_blank()
            if self.at_end():
                break
            cur = self.indent_of(self.i)
            if cur < indent:
                break
            if cur > indent:
                self.err("indentation increases where a sequence item was expected")
            body = self.lines[self.i][cur:]
            if body == "-":
                self.i += 1
                items.append(self.parse_nested(indent))
                continue
            if not body.startswith("- "):
                break
            after = body[2:]
            if after.strip() == "" or after.lstrip().startswith("#"):
                self.i += 1
                items.append(self.parse_nested(indent))
                continue
            _key, _rest, ok = self.split_key(after)
            if ok:
                # Rewrite "- key: v" as "  key: v" and read it as a mapping
                # that starts two columns in. Later keys of the same item are
                # already written at that column.
                self.lines[self.i] = " " * (cur + 2) + after
                items.append(self.parse_map(cur + 2))
                continue
            line = self.i
            self.i += 1
            items.append(self.parse_inline_scalar(after.strip(), line))
        return items

    # ---- scalars -------------------------------------------------------
    def parse_inline_scalar(self, s, line):
        if _UNSUPPORTED_START.match(s):
            self.err("YAML anchors, aliases and tags are not understood by "
                     "this reader, so the file is refused rather than guessed at",
                     line)
        if s[0] in "[{":
            value, idx = self.scan_flow(s, 0, line)
            trailing = s[idx:].strip()
            if trailing and not trailing.startswith("#"):
                self.err("unexpected text after a flow collection", line)
            return value
        if s[0] in "\"'":
            value, idx = self.scan_quoted(s, 0, line)
            trailing = s[idx:].strip()
            if trailing and not trailing.startswith("#"):
                self.err("unexpected text after a quoted scalar", line)
            return value
        return resolve_plain(_COMMENT_SPLIT.split(s, 1)[0].strip())

    def scan_quoted(self, s, start, line=None):
        quote = s[start]
        i = start + 1
        out = []
        escapes = {"n": "\n", "t": "\t", "r": "\r", "\\": "\\", "\"": "\"",
                   "'": "'", "/": "/", "0": "\0", " ": " "}
        while i < len(s):
            c = s[i]
            if quote == "'":
                if c == "'":
                    if s[i + 1:i + 2] == "'":
                        out.append("'")
                        i += 2
                        continue
                    return "".join(out), i + 1
                out.append(c)
                i += 1
                continue
            if c == "\\":
                nxt = s[i + 1:i + 2]
                if nxt in escapes:
                    out.append(escapes[nxt])
                    i += 2
                    continue
                if nxt == "u":
                    try:
                        out.append(chr(int(s[i + 2:i + 6], 16)))
                    except ValueError:
                        self.err("a \\u escape is malformed", line)
                    i += 6
                    continue
                self.err("the escape sequence \\%s is not understood" % nxt, line)
            if c == "\"":
                return "".join(out), i + 1
            out.append(c)
            i += 1
        self.err("a quoted scalar is not closed on its line; this reader does "
                 "not follow one onto the next line", line)

    def scan_flow(self, s, i, line):
        opener = s[i]
        closer = "]" if opener == "[" else "}"
        i += 1
        seq = []
        mapping = {}
        while True:
            while i < len(s) and s[i] in " \t":
                i += 1
            if i >= len(s):
                self.err("a flow collection is not closed on its line", line)
            if s[i] == closer:
                i += 1
                break
            if s[i] == ",":
                i += 1
                continue
            start = i
            value, i = self.scan_flow_entry(s, i, line)
            while i < len(s) and s[i] in " \t":
                i += 1
            if opener == "{" and i < len(s) and s[i] == ":":
                i += 1
                while i < len(s) and s[i] in " \t":
                    i += 1
                inner, i = self.scan_flow_entry(s, i, line)
                mapping[value if isinstance(value, str) else str(value)] = inner
            else:
                seq.append(value)
            if i <= start:
                # Belt and braces. Every branch above is meant to consume at
                # least one character. This turns a future branch that does not
                # into a refusal instead of a hang: the gate runs inside `make
                # full`, and a merge decision that never finishes is worse than
                # one that says no.
                self.err("this reader cannot get past %r inside a flow "
                         "collection, so the file is refused rather than read "
                         "as something it is not" % s[start:start + 1], line)
        return (mapping if opener == "{" else seq), i

    def scan_flow_entry(self, s, i, line):
        """One value inside a flow collection, with the guarantee the caller
        needs: it consumes a character, or the file is refused. A value that
        reads as empty is legal only where the next character ends the entry
        (`[a, ]`). Anywhere else the character is one a real YAML parser also
        refuses — `[a:b]`, `{a: b: c}` and `run: [[ -n "$X" ]]` are all syntax
        errors — and returning with the index unchanged used to hang."""
        value, j = self.scan_flow_scalar(s, i, line)
        if j == i and (j >= len(s) or s[j] not in ",]}"):
            self.err("a flow collection contains %r where this reader expects "
                     "a value, a comma or a closing bracket"
                     % s[j:j + 1], line)
        return value, j

    def scan_flow_scalar(self, s, i, line):
        if _UNSUPPORTED_START.match(s[i:]):
            self.err("YAML anchors, aliases and tags are not understood by "
                     "this reader", line)
        if s[i] in "[{":
            return self.scan_flow(s, i, line)
        if s[i] in "\"'":
            return self.scan_quoted(s, i, line)
        j = i
        while j < len(s) and s[j] not in ",:]}":
            j += 1
        return resolve_plain(s[i:j].strip()), j

    def parse_block_scalar(self, header, indent):
        style = header[0]
        rest = _COMMENT_SPLIT.split(header[1:], 1)[0].strip()
        chomp = ""
        explicit = None
        # An indentation indicator is ONE digit, and that digit is 1 to 9.
        # Reading a wider set makes this gate fail OPEN, which is the one
        # direction that matters here:
        #   `|0`  libyaml refuses it — "found an indentation indicator equal
        #         to 0". This reader used to accept it and then discard it,
        #         because `0` is falsy and the line below tested truthiness.
        #   `|10` libyaml reads one digit and then wants a comment or a line
        #         break, so the second digit ends the file. This reader used to
        #         let each digit overwrite the last, so `|10` also arrived at 0
        #         and was discarded by the same falsy test.
        #   an Arabic-Indic two, and every other Unicode DECIMAL digit
        #         (category Nd: Devanagari, fullwidth, N'Ko, and the rest).
        #         str.isdigit() is true AND int() returns 2 — no error at all.
        #         The shipped reader took the 2, read the file and said ok,
        #         exit 0, while libyaml refuses the file. So this was a SILENT
        #         fail-open, the same direction as `|0` and the worse of the
        #         two Unicode halves.
        #   a superscript two, and the other NON-decimal digits (category No).
        #         isdigit() is true but int() raises ValueError, which is not a
        #         YamlError, so it escaped this reader as a traceback rather
        #         than as a refusal with a reason. That half failed loud.
        # libyaml reads ASCII digits only. GitHub refuses every one of these
        # files, so no run is created and the required context stays pending
        # for ever — the wedge this gate exists to catch, arriving through a
        # file the gate called fine.
        #
        # Testing the accepted set directly refuses all of them in one place, and
        # never calls int() on a character that cannot survive it. An
        # isdigit() test with the three cases repaired afterwards would need
        # three guards to say one rule, and would leave the isdigit()/int()
        # pair in the code for a later edit to widen again.
        #
        # This branch is TIGHTER than libyaml in one known place and LOOSER in
        # another. Tighter: `|2#x` starts a comment with no space in front of
        # it, libyaml takes it, and this reader refuses it. THAT is why the
        # message below does not say that everything it refuses is a file
        # GitHub would refuse — `|2#x` is refused here and accepted there. It
        # fails closed, and it refused this before the indicator rule was
        # tightened, so it is not a new divergence. Looser: `| 2` and `|--`
        # are refused by libyaml and still read here. That is a separate gap,
        # filed as hk-jkqir, deliberately not closed in this change.
        for ch in rest:
            if ch in "+-":
                chomp = ch
            elif ch in "123456789" and explicit is None:
                explicit = int(ch)
            else:
                # State the accepted set, not the rule the author probably
                # broke. `|2#x` reaches here and breaks no digit rule, so a
                # message about digits would send that reader the wrong way.
                self.err("the block scalar header '%s' is not understood; this "
                         "reader accepts a chomping indicator (+ or -) and at "
                         "most one indentation digit, 1 to 9" % header)
        raw = []
        # An explicit indicator fixes the content indent in the header, before
        # any content line is read. With no indicator it is not fixed until the
        # first non-empty line, and it is that line's SPACES that fix it. The
        # test is `is not None`, not truthiness: 0 is refused above so the two
        # agree today, but they stop agreeing the moment anybody widens the
        # accepted set, and then `indent + 0` is a real column rather than
        # "not fixed yet".
        content_indent = indent + explicit if explicit is not None else None
        while self.i < len(self.lines):
            line = self.lines[self.i]
            blank = line.strip() == ""
            width = self.indent_width(self.i)
            if not blank and width <= indent:
                # The line sits left of the key that owns the scalar, so it
                # ends it. The structural reader picks it up from here and
                # refuses a tab there in its own words.
                break
            # indent_width, not indent_of: a tab AT OR PAST the content indent
            # is ordinary text — a Makefile recipe, a heredoc body — and legal
            # YAML. A tab left of that column is indentation, and so is a tab on
            # the first content line of a scalar whose indent is not fixed yet,
            # because that line fixes the column by its spaces. libyaml refuses
            # both, GitHub then refuses the whole workflow, and a gate that said
            # ok has produced the pending-forever context it exists to prevent.
            # A whitespace-only line is checked too: libyaml reads its
            # indentation like any other line's.
            if line[width:width + 1] == "\t" and (
                    content_indent is None or width < content_indent):
                self.err("a tab appears in a block scalar's indentation; YAML "
                         "measures that indentation in spaces only, so GitHub "
                         "refuses the whole workflow file and no check run is "
                         "ever created", self.i)
            if blank:
                raw.append("")
                self.i += 1
                continue
            if content_indent is None:
                content_indent = width
            if width < content_indent:
                break
            raw.append(line[content_indent:])
            self.i += 1
        while raw and raw[-1] == "":
            raw.pop()
        if not raw:
            return ""
        if style == "|":
            text = "\n".join(raw)
        else:
            text = ""
            for k, ln in enumerate(raw):
                if k == 0:
                    text = ln
                elif ln == "":
                    text += "\n"
                elif text.endswith("\n"):
                    text += ln
                else:
                    text += " " + ln
        if chomp == "-":
            return text
        return text + "\n"


def read_workflow(path):
    """(contexts, candidates, reason, text). When reason is not None the file
    could not be read and nothing from it is usable."""
    try:
        with open(path, "r") as fh:
            text = fh.read()
    except OSError as exc:
        return [], [], "cannot be read: %s" % exc, ""
    try:
        doc = Reader(text, path).parse_document()
    except YamlError as exc:
        return [], [], "cannot be parsed: %s" % exc, text
    except RecursionError:
        # Fails closed either way, but every other refusal here names a reason
        # and a traceback names none. A reader that ran out of stack has not
        # read the file, which is all this gate needs to say.
        return [], [], ("cannot be parsed: it nests deeper than this reader "
                        "follows, so the reader ran out of stack before it "
                        "read any job"), text
    if not isinstance(doc, dict):
        return [], [], "does not parse as a YAML mapping, so it declares no jobs", text
    jobs = doc.get("jobs")
    if not isinstance(jobs, dict):
        return [], [], None, text
    contexts = []
    candidates = []
    for job_id, job in jobs.items():
        if not isinstance(job, dict):
            return [], [], "job '%s' does not parse as a mapping" % job_id, text
        context = reported_context(job_id, job)
        contexts.append((path, job_id, context))
        if context == REQUIRED_CONTEXT:
            candidates.append((path, job_id, job, doc))
    return contexts, candidates, None, text


# ---- what GitHub would do with the parsed workflow ---------------------
def pull_request_trigger(doc):
    """(present, config). config is the trigger's mapping, or None when the
    trigger is declared with no filters at all."""
    on = doc.get("on") if isinstance(doc, dict) else None
    if isinstance(on, str):
        return (on == "pull_request", None)
    if isinstance(on, list):
        return ("pull_request" in on, None)
    if isinstance(on, dict):
        if "pull_request" in on:
            cfg = on["pull_request"]
            return (True, cfg if isinstance(cfg, dict) else None)
        return (False, None)
    return (False, None)


def glob_to_re(pattern):
    out = ["^"]
    i = 0
    while i < len(pattern):
        c = pattern[i]
        if c == "*":
            if pattern[i + 1:i + 2] == "*":
                out.append(".*")
                i += 2
                continue
            out.append("[^/]*")
            i += 1
            continue
        if c == "?":
            out.append("[^/]")
            i += 1
            continue
        out.append(re.escape(c))
        i += 1
    out.append("$")
    return re.compile("".join(out))


def branch_admitted(spec, branch, is_ignore):
    patterns = spec if isinstance(spec, list) else [spec]
    patterns = [str(p) for p in patterns if p is not None]
    if is_ignore:
        return not any(glob_to_re(p).match(branch) for p in patterns)
    positive = [p for p in patterns if not p.startswith("!")]
    negative = [p[1:] for p in patterns if p.startswith("!")]
    if any(glob_to_re(p).match(branch) for p in negative):
        return False
    if not positive:
        return True
    return any(glob_to_re(p).match(branch) for p in positive)


def reported_context(job_id, job):
    """GitHub reports a job under its `name:`, and under its key when it
    declares none. A block scalar's trailing newline is ignored here; leading
    and internal whitespace is not."""
    if isinstance(job, dict) and isinstance(job.get("name"), str):
        return job["name"].rstrip("\n")
    return job_id


def declares_runner(job):
    """True when GitHub would accept the job. A job either names a runner or
    delegates with `uses:`; one that does neither is a workflow-level syntax
    error, so the run never starts and no check run is created."""
    if "uses" in job:
        return True
    runs_on = job.get("runs-on")
    return runs_on is not None and runs_on != "" and runs_on != [] and runs_on != {}


def needs_of(job):
    spec = job.get("needs")
    if spec is None:
        return []
    if isinstance(spec, list):
        return [str(n) for n in spec if n is not None]
    return [str(spec)]


def job_problems(doc, job, path):
    problems = []
    jobs = doc.get("jobs") if isinstance(doc, dict) else None
    jobs = jobs if isinstance(jobs, dict) else {}

    present, cfg = pull_request_trigger(doc)
    if not present:
        problems.append(
            "its workflow has no pull_request trigger, so it reports nothing "
            "on a pull request and the required context never arrives")
    elif cfg is not None:
        if "paths" in cfg or "paths-ignore" in cfg:
            problems.append(
                "its pull_request trigger carries path filters, so the run "
                "is skipped for any pull request that touches nothing on the "
                "list and the required context stays pending forever")
        for key, is_ignore in (("branches", False), ("branches-ignore", True)):
            if key in cfg and not branch_admitted(cfg[key], PROTECTED_BRANCH, is_ignore):
                problems.append(
                    "its pull_request trigger does not run for pull requests "
                    "targeting '%s' (%s: %r)" % (PROTECTED_BRANCH, key, cfg[key]))
        if "types" in cfg:
            types = cfg["types"] if isinstance(cfg["types"], list) else [cfg["types"]]
            missing = [t for t in ("opened", "synchronize") if t not in types]
            if missing:
                problems.append(
                    "its pull_request types list omits %s, so the check never "
                    "runs on an open pull request" % ", ".join(missing))

    strategy = job.get("strategy")
    if isinstance(strategy, dict) and "matrix" in strategy:
        problems.append(
            "it uses a build matrix, so every leg reports '%s (value)' and the "
            "bare context is never reported" % REQUIRED_CONTEXT)

    if "if" in job:
        problems.append(
            "it carries a job-level if:, which this gate cannot evaluate; a "
            "conditional required job either fails to report or reports "
            "without running the work")

    if "continue-on-error" in job and job["continue-on-error"] is not False:
        problems.append(
            "it carries continue-on-error, which reports success on every REST "
            "surface after a real failure")

    # The one invalidity check. Rules above describe ways a VALID workflow
    # loses the context; a workflow GitHub refuses loses it just as completely.
    # One bad job kills the whole file, so a sibling counts.
    if not declares_runner(job):
        problems.append(
            "it declares no runs-on:, so GitHub refuses the whole workflow "
            "file, no run starts and no check run is ever created")
    for other_id, other in jobs.items():
        if other is job or not isinstance(other, dict):
            continue
        if not declares_runner(other):
            problems.append(
                "job '%s' in the same workflow declares no runs-on:, so GitHub "
                "refuses the whole workflow file and this job never runs either"
                % other_id)

    # Rule 7 one hop away. A skipped dependency skips this job; GitHub reports
    # the required check as skipped; branch protection reads a skipped required
    # check as satisfied. `seen` also makes a needs: cycle terminate.
    seen = set()
    queue = needs_of(job)
    while queue:
        dep_id = queue.pop(0)
        if dep_id in seen:
            continue
        seen.add(dep_id)
        dep = jobs.get(dep_id)
        if not isinstance(dep, dict):
            problems.append(
                "it needs job '%s', which this workflow does not declare, so "
                "GitHub refuses the whole workflow file and no check run is "
                "ever created" % dep_id)
            continue
        if "if" in dep:
            problems.append(
                "it needs job '%s', which carries a job-level if:. When that "
                "job is skipped this one is skipped with it, GitHub reports "
                "the required context as skipped, and branch protection reads "
                "a skipped required check as satisfied — so the merge decision "
                "is never made" % dep_id)
        queue.extend(needs_of(dep))

    if "uses" in job:
        problems.append(
            "it delegates to a reusable workflow. A calling job reports its "
            "child jobs under composed names, '<job name> / <child job name>', "
            "so the bare context '%s' is never reported at all — and this gate "
            "does not follow the child, so it cannot confirm '%s' runs there "
            "either" % (REQUIRED_CONTEXT, REQUIRED_COMMAND))
    else:
        steps = job.get("steps")
        steps = steps if isinstance(steps, list) else []
        runs_it = False
        guarded = False
        masked = False
        delegated = []
        for step in steps:
            if not isinstance(step, dict):
                continue
            if isinstance(step.get("uses"), str):
                delegated.append(step["uses"])
            if not isinstance(step.get("run"), str):
                continue
            if REQUIRED_COMMAND not in step["run"]:
                continue
            if "if" in step:
                # Same hole as a job-level if:, moved down one line. Without
                # this the job-level rule is defeated by a one-line edit.
                guarded = True
                continue
            if "continue-on-error" in step and step["continue-on-error"] is not False:
                # Same masking as the job-level flag, moved down one line, and
                # this is the line every real instance of it in this repo was
                # written on. The step fails, the job still reports success,
                # and the required context arrives green.
                masked = True
                continue
            runs_it = True
            break
        if not runs_it and guarded:
            problems.append(
                "every step that invokes '%s' carries a step-level if:, which "
                "this gate cannot evaluate, so the job can report the required "
                "context without making the merge decision" % REQUIRED_COMMAND)
        if not runs_it and masked:
            problems.append(
                "every step that invokes '%s' carries continue-on-error, so "
                "the step can fail while the job reports success and the "
                "required context arrives green after a real failure"
                % REQUIRED_COMMAND)
        if not runs_it and not guarded and not masked:
            if delegated:
                # Say only what the gate can see. It cannot read a composite
                # action, so "it runs no step that invokes make full" would be
                # a stronger claim than it can support.
                problems.append(
                    "it runs no step that invokes '%s' directly, and it calls "
                    "%s, which this gate does not follow, so it cannot confirm "
                    "the job makes the merge decision"
                    % (REQUIRED_COMMAND,
                       ", ".join("'%s'" % u for u in delegated)))
            else:
                problems.append(
                    "it runs no step that invokes '%s', so it carries the "
                    "required context without making the merge decision"
                    % REQUIRED_COMMAND)

    return problems


def main(paths):
    contexts = []          # (path, job_id, context)
    candidates = []        # (path, job_id, job, doc)
    unreadable = []        # (path, reason, mentions_required_context)

    for path in paths:
        file_contexts, file_candidates, reason, text = read_workflow(path)
        if reason is not None:
            unreadable.append((path, reason, REQUIRED_CONTEXT in text))
            continue
        contexts.extend(file_contexts)
        candidates.extend(file_candidates)

    # An unreadable file whose text names the required context is fatal even
    # when a healthy workflow sits beside it: it may declare a second job under
    # the same name, and the gate cannot tell which check run GitHub grades.
    suspect = [(path, reason) for path, reason, mentions in unreadable if mentions]

    findings = []
    for path, job_id, job, doc in candidates:
        problems = job_problems(doc, job, path)
        if problems:
            findings.append((path, job_id, problems))
            continue
        if suspect:
            break
        for bad_path, bad_reason, _ in unreadable:
            print("%s: note — %s %s. It is set aside: it cannot deliver '%s' "
                  "and it does not mention it, so it does not change this "
                  "gate's answer."
                  % (PREFIX, bad_path, bad_reason, REQUIRED_CONTEXT),
                  file=sys.stderr)
        print("%s: ok — %s job '%s' reports the required context '%s' and "
              "runs '%s' (%d workflow file(s), %d job(s) checked)"
              % (PREFIX, path, job_id, REQUIRED_CONTEXT, REQUIRED_COMMAND,
                 len(paths), len(contexts)))
        return 0

    if findings:
        print("%s: FAIL — a job is named '%s' but cannot report it as a "
              "working required check." % (PREFIX, REQUIRED_CONTEXT),
              file=sys.stderr)
        for path, job_id, problems in findings:
            print("  %s job '%s':" % (path, job_id), file=sys.stderr)
            for problem in problems:
                print("    - %s" % problem, file=sys.stderr)
    elif suspect:
        print("%s: FAIL — a workflow this gate cannot read mentions '%s'."
              % (PREFIX, REQUIRED_CONTEXT), file=sys.stderr)
        print("  It may declare a second job under that name, and two check "
              "runs with one", file=sys.stderr)
        print("  name leave the gate unable to say which one branch protection "
              "grades.", file=sys.stderr)
    elif not candidates:
        print("%s: FAIL — no job in any scanned workflow is named '%s'."
              % (PREFIX, REQUIRED_CONTEXT), file=sys.stderr)
        print("  Branch protection on %s requires exactly that context. With "
              "no job" % PROTECTED_BRANCH, file=sys.stderr)
        print("  reporting it, every pull request to %s blocks forever on a "
              "check" % PROTECTED_BRANCH, file=sys.stderr)
        print("  that cannot arrive.", file=sys.stderr)
        print("  Contexts found (a job with no name: reports under its key):",
              file=sys.stderr)
        for path, job_id, context in contexts:
            print("    %s [%s] %s" % (path, job_id, context), file=sys.stderr)
        print("  Fix: restore the job name, or change branch protection AND the",
              file=sys.stderr)
        print("  REQUIRED_CONTEXT value in this script in the same commit.",
              file=sys.stderr)

    if unreadable:
        print("  Workflow files this gate could not read. Any one of them "
              "might have been", file=sys.stderr)
        print("  the file meant to carry the context, so with nothing else "
              "carrying it they", file=sys.stderr)
        print("  are part of this refusal:", file=sys.stderr)
        for path, reason, mentions in unreadable:
            print("    %s %s%s"
                  % (path, reason,
                     " [mentions '%s']" % REQUIRED_CONTEXT if mentions else ""),
                  file=sys.stderr)
        print("  A workflow this gate cannot read is a workflow it cannot "
              "vouch for.", file=sys.stderr)
    print("  Branch protection on %s requires exactly that context, and a "
          "context" % PROTECTED_BRANCH, file=sys.stderr)
    print("  that never arrives reads as PENDING, not as failing. Every pull "
          "request", file=sys.stderr)
    print("  to %s then blocks forever." % PROTECTED_BRANCH, file=sys.stderr)
    return 1


sys.exit(main(sys.argv[1:]))
PYTHON_EOF
exit $?
