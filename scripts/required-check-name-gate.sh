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
#   scalars with `-`/`+` chomping and an explicit indent digit; flow sequences
#   `[a, b]` and flow mappings `{a: b}`; `#` comments; a single leading `---`.
#   It resolves `true`/`false` to booleans and `null`/`~`/empty to nothing, and
#   leaves every other scalar a string. It deliberately does NOT resolve the
#   key `on` to the boolean true, which YAML 1.1 does and which would hide
#   every trigger in every workflow behind a key nobody would think to read.
#
#   It does NOT read: anchors, aliases, merge keys, explicit tags, more than
#   one document per file, quoted scalars that continue onto a second line, or
#   tabs used as indentation. It REFUSES the file when it meets one, and the
#   gate fails. Failing closed on a construct the reader does not know is the
#   whole point: the old gate passed a file that was not valid YAML at all.
#   A folded scalar over several lines is folded approximately; job names are
#   one line, so this has never mattered, but it is not a general YAML parser
#   and must not be reused as one.
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
#   8. The job is not `continue-on-error: true`, which reports success on every
#      REST surface after a real failure.
#   9. The job runs REQUIRED_COMMAND in one of its steps, and that step carries
#      no `if:` either. This is what tells a real gate from a decoy that
#      carries the name and runs `true`, and it is checked at the step level
#      as well because otherwise rule 7 is defeated by moving the `if:` down
#      one line. It is a substring match on the step's `run:`, so a workflow
#      that only MENTIONS the command in an echo satisfies it. The gate says
#      the command appears; it cannot say the command does anything.
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


_PLAIN_KEY = re.compile(r'^([^\s:#][^:#]*?)\s*:(?=\s|$)')
_UNSUPPORTED_START = re.compile(r'^[&*!]')
_COMMENT_SPLIT = re.compile(r'(?:^|\s)#')


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

    def indent_of(self, line):
        s = self.lines[line]
        width = len(s) - len(s.lstrip(" "))
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
        m = _PLAIN_KEY.match(body)
        if not m:
            return None, None, False
        return m.group(1).strip(), body[m.end():], True

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
            value, i = self.scan_flow_scalar(s, i, line)
            while i < len(s) and s[i] in " \t":
                i += 1
            if opener == "{" and i < len(s) and s[i] == ":":
                i += 1
                while i < len(s) and s[i] in " \t":
                    i += 1
                inner, i = self.scan_flow_scalar(s, i, line)
                mapping[value if isinstance(value, str) else str(value)] = inner
            else:
                seq.append(value)
        return (mapping if opener == "{" else seq), i

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
        for ch in rest:
            if ch in "+-":
                chomp = ch
            elif ch.isdigit():
                explicit = int(ch)
            else:
                self.err("the block scalar header '%s' is not understood" % header)
        raw = []
        content_indent = None
        while self.i < len(self.lines):
            line = self.lines[self.i]
            if line.strip() == "":
                raw.append("")
                self.i += 1
                continue
            cur = self.indent_of(self.i)
            if cur <= indent:
                break
            if content_indent is None:
                content_indent = indent + explicit if explicit else cur
            if cur < content_indent:
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


def parse_workflow(path):
    with open(path, "r") as fh:
        text = fh.read()
    return Reader(text, path).parse_document()


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


def job_problems(doc, job, path):
    problems = []

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

    if "uses" in job:
        problems.append(
            "it delegates to a reusable workflow, which this gate does not "
            "follow, so it cannot confirm the job runs '%s'" % REQUIRED_COMMAND)
    else:
        steps = job.get("steps")
        steps = steps if isinstance(steps, list) else []
        runs_it = False
        guarded = False
        for step in steps:
            if not isinstance(step, dict) or not isinstance(step.get("run"), str):
                continue
            if REQUIRED_COMMAND not in step["run"]:
                continue
            if "if" in step:
                # Same hole as a job-level if:, moved down one line. Without
                # this the job-level rule is defeated by a one-line edit.
                guarded = True
                continue
            runs_it = True
            break
        if not runs_it and guarded:
            problems.append(
                "every step that invokes '%s' carries a step-level if:, which "
                "this gate cannot evaluate, so the job can report the required "
                "context without making the merge decision" % REQUIRED_COMMAND)
        elif not runs_it:
            problems.append(
                "it runs no step that invokes '%s', so it carries the required "
                "context without making the merge decision" % REQUIRED_COMMAND)

    return problems


def main(paths):
    contexts = []          # (path, job_id, context)
    candidates = []        # (path, job_id, job, doc)

    for path in paths:
        try:
            doc = parse_workflow(path)
        except YamlError as exc:
            print("%s: FAIL — %s cannot be parsed: %s" % (PREFIX, path, exc),
                  file=sys.stderr)
            print("  A workflow this gate cannot read is a workflow it cannot "
                  "vouch for, so it refuses rather than passing on a guess.",
                  file=sys.stderr)
            return 1
        except OSError as exc:
            print("%s: FAIL — %s cannot be read: %s" % (PREFIX, path, exc),
                  file=sys.stderr)
            return 1

        if not isinstance(doc, dict):
            print("%s: FAIL — %s does not parse as a YAML mapping, so it "
                  "declares no jobs" % (PREFIX, path), file=sys.stderr)
            return 1

        jobs = doc.get("jobs")
        if not isinstance(jobs, dict):
            continue
        for job_id, job in jobs.items():
            if not isinstance(job, dict):
                print("%s: FAIL — job '%s' in %s does not parse as a mapping"
                      % (PREFIX, job_id, path), file=sys.stderr)
                return 1
            context = reported_context(job_id, job)
            contexts.append((path, job_id, context))
            if context == REQUIRED_CONTEXT:
                candidates.append((path, job_id, job, doc))

    if not candidates:
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
        return 1

    findings = []
    for path, job_id, job, doc in candidates:
        problems = job_problems(doc, job, path)
        if not problems:
            print("%s: ok — %s job '%s' reports the required context '%s' and "
                  "runs '%s' (%d workflow file(s), %d job(s) checked)"
                  % (PREFIX, path, job_id, REQUIRED_CONTEXT, REQUIRED_COMMAND,
                     len(paths), len(contexts)))
            return 0
        findings.append((path, job_id, problems))

    print("%s: FAIL — a job is named '%s' but cannot report it as a working "
          "required check." % (PREFIX, REQUIRED_CONTEXT), file=sys.stderr)
    for path, job_id, problems in findings:
        print("  %s job '%s':" % (path, job_id), file=sys.stderr)
        for problem in problems:
            print("    - %s" % problem, file=sys.stderr)
    print("  Branch protection on %s requires exactly that context, and a "
          "context" % PROTECTED_BRANCH, file=sys.stderr)
    print("  that never arrives reads as PENDING, not as failing. Every pull "
          "request", file=sys.stderr)
    print("  to %s then blocks forever." % PROTECTED_BRANCH, file=sys.stderr)
    return 1


sys.exit(main(sys.argv[1:]))
PYTHON_EOF
exit $?
