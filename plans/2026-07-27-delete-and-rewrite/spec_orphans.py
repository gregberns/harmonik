#!/usr/bin/env python3
"""Measure how many requirement IDs in specs/ are cited by no Go file.

Run it with no arguments from anywhere in the repo:

    python3 plans/2026-07-27-delete-and-rewrite/spec_orphans.py

WHAT IT COUNTS
  The universe is every distinct base requirement ID that occurs in any .md
  file under specs/. A base ID has the shape PREFIX-NNN with an optional
  lowercase letter suffix, for example EM-031a. The PREFIX must be registered
  in specs/_registry.yaml. An ID belongs to the spec that owns its prefix, not
  to the file the occurrence sits in.

WHAT IT DELIBERATELY EXCLUDES
  Open questions      OQ-PREFIX-NNN
  Invariants          PREFIX-INV-NNN
  Environment vars    PREFIX-ENV-NNN
  Other sub-forms     PREFIX-EX-NNN, PREFIX-MIG-NNN, PREFIX-RIA-NNN
  Unregistered prefixes.
  These are questions, invariants and sub-forms, not base requirements.

HOW IT CLASSIFIES
  The Go corpus is `git ls-files '*.go'`. Tracked files only. This keeps
  untracked scratch files and nested agent worktrees out of the count. An
  earlier hand-run sweep counted .claude/worktrees/ and read about ten times
  too high.
  A file is a TEST file when its name ends in _test.go or its path contains a
  testdata directory. Every other Go file is production.
  An ID is CITED by a file when the ID string occurs in that file.
    CITED-IN-PRODUCTION  at least one production file cites it
    CITED-ONLY-IN-TESTS  no production file cites it, at least one test does
    UNCITED              no Go file cites it. This is an orphan.

The output prints these definitions, so the number always travels with its
method. The script is deterministic and makes no change to the repo.
"""

import os
import re
import subprocess
import sys
from collections import defaultdict

# A registry line looks like:  AR: {spec-id: architecture, reserved: ..., ...}
REGISTRY_LINE = re.compile(r"^\s+([A-Z][A-Z0-9]{1,3}):\s*\{[^}]*spec-id:\s*([A-Za-z0-9_./-]+)")

# Base requirement ID. The prefix alternation is filled in from the registry.
# (?<![A-Za-z0-9-]) rejects a match that continues a longer token, which is what
# keeps OQ-EM-001 from contributing EM-001.
# -\d{3} then rejects EM-INV-008, EM-ENV-003, EM-EX-01 and the rest, because
# those have letters where the digits must be.
ID_TEMPLATE = r"(?<![A-Za-z0-9-])(%s)-(\d{3})([a-z]?)(?![A-Za-z0-9])"


def repo_root(argv):
    if len(argv) > 1:
        return os.path.abspath(argv[1])
    here = os.path.dirname(os.path.abspath(__file__))
    out = subprocess.run(
        ["git", "-C", here, "rev-parse", "--show-toplevel"],
        capture_output=True, text=True, check=True,
    )
    return out.stdout.strip()


def load_prefixes(root):
    """Map each registered prefix to the spec-id that owns it."""
    path = os.path.join(root, "specs", "_registry.yaml")
    owner = {}
    with open(path, encoding="utf-8") as fh:
        for line in fh:
            m = REGISTRY_LINE.match(line)
            if m:
                owner[m.group(1)] = m.group(2)
    if not owner:
        sys.exit("no prefixes found in specs/_registry.yaml")
    return owner


def spec_files(root):
    out = []
    for dirpath, _dirnames, filenames in os.walk(os.path.join(root, "specs")):
        for name in sorted(filenames):
            if name.endswith(".md"):
                out.append(os.path.join(dirpath, name))
    return sorted(out)


def go_files(root):
    """Tracked Go files, split into production and test."""
    out = subprocess.run(
        ["git", "-C", root, "ls-files", "*.go"],
        capture_output=True, text=True, check=True,
    )
    prod, test = [], []
    for rel in out.stdout.splitlines():
        if not rel:
            continue
        parts = rel.split("/")
        if rel.endswith("_test.go") or "testdata" in parts:
            test.append(os.path.join(root, rel))
        else:
            prod.append(os.path.join(root, rel))
    return prod, test


def read(path):
    with open(path, encoding="utf-8", errors="replace") as fh:
        return fh.read()


def collect_ids(paths, pattern):
    """Return {id: set(paths that hold it)}."""
    found = defaultdict(set)
    for path in paths:
        for m in pattern.finditer(read(path)):
            found[m.group(0)].add(path)
    return found


def cite_counts(paths, ids):
    """Return {id: number of files in paths that name it}."""
    counts = defaultdict(int)
    for path in paths:
        text = read(path)
        for rid in ids:
            if rid in text:
                counts[rid] += 1
    return counts


def main():
    root = repo_root(sys.argv)
    owner = load_prefixes(root)
    pattern = re.compile(ID_TEMPLATE % "|".join(sorted(owner, key=len, reverse=True)))

    specs = spec_files(root)
    declared = collect_ids(specs, pattern)
    ids = sorted(declared)

    prod, test = go_files(root)
    prod_hits = cite_counts(prod, ids)
    test_hits = cite_counts(test, ids)

    verdict = {}
    for rid in ids:
        if prod_hits.get(rid, 0):
            verdict[rid] = "PROD"
        elif test_hits.get(rid, 0):
            verdict[rid] = "TEST"
        else:
            verdict[rid] = "UNCITED"

    n_prod = sum(1 for v in verdict.values() if v == "PROD")
    n_test = sum(1 for v in verdict.values() if v == "TEST")
    n_orph = sum(1 for v in verdict.values() if v == "UNCITED")
    total = len(ids)

    print(__doc__.strip())
    print()
    print("=" * 72)
    rev = subprocess.run(
        ["git", "-C", root, "rev-parse", "--short", "HEAD"],
        capture_output=True, text=True, check=True,
    ).stdout.strip()
    print(f"repo {root} @ {rev}")
    print(f"spec files scanned      {len(specs)}")
    print(f"registered prefixes     {len(owner)}")
    print(f"tracked Go production   {len(prod)}")
    print(f"tracked Go test         {len(test)}")
    print()
    print(f"requirement IDs         {total}")
    print(f"  CITED-IN-PRODUCTION   {n_prod:5d}  {pct(n_prod, total)}")
    print(f"  CITED-ONLY-IN-TESTS   {n_test:5d}  {pct(n_test, total)}")
    print(f"  UNCITED (orphans)     {n_orph:5d}  {pct(n_orph, total)}")
    print()

    per = defaultdict(lambda: [0, 0, 0, 0])
    for rid in ids:
        prefix = rid.rsplit("-", 1)[0]
        row = per[owner[prefix]]
        row[0] += 1
        row[{"PROD": 1, "TEST": 2, "UNCITED": 3}[verdict[rid]]] += 1

    print("per spec, sorted by orphan rate")
    print(f"{'spec':32s} {'IDs':>5s} {'PROD':>5s} {'TEST':>5s} {'ORPH':>5s} {'orph%':>7s}")
    for spec, (n, p, t, o) in sorted(
        per.items(), key=lambda kv: (-kv[1][3] / kv[1][0], kv[0])
    ):
        print(f"{spec:32s} {n:5d} {p:5d} {t:5d} {o:5d} {pct(o, n):>7s}")
    print(f"{'TOTAL':32s} {total:5d} {n_prod:5d} {n_test:5d} {n_orph:5d} {pct(n_orph, total):>7s}")
    print()

    print("orphan IDs, grouped by owning spec")
    by_spec = defaultdict(list)
    for rid in ids:
        if verdict[rid] == "UNCITED":
            by_spec[owner[rid.rsplit("-", 1)[0]]].append(rid)
    for spec in sorted(by_spec, key=lambda s: (-len(by_spec[s]), s)):
        print(f"  {spec} ({len(by_spec[spec])}): {' '.join(sorted(by_spec[spec]))}")


def pct(part, whole):
    return f"{100.0 * part / whole:.1f}%" if whole else "n/a"


if __name__ == "__main__":
    main()
