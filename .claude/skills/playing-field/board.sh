#!/usr/bin/env bash
# playing-field: the fleet at a glance, GROUPED BY EPIC.
# Each epic = a kerf work (codename: label). Under it, its beads as bullets,
# each with a state glyph so you can scan status at a glance:
#   🔨 doing (in_progress)   ⛔ blocked   ○ next (open/ready)   ✅ done (closed)
# Epic header shows done/total. Fully-done+idle epics collapse to one line.
#
# NOTE: only kerf-codename epics group cleanly; flat beads with no codename
# land in "(ungrouped)". Counts mirror `kerf map`.
#
# Usage: board.sh [--recent-days N] [--all]   (default recent=3; --all shows every epic)
set -uo pipefail
cd "${HARMONIK_PROJECT:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}" || exit 1

RECENT=3; SHOWALL=0
while [ $# -gt 0 ]; do
  case "$1" in
    --recent-days) RECENT="${2:-3}"; shift 2;;
    --all) SHOWALL=1; shift;;
    *) shift;;
  esac
done

CREWS=$(harmonik crew list 2>/dev/null | awk 'NF{print $1}' | sort -u | paste -sd', ' -)

br list -a --limit 0 --json 2>/dev/null | RECENT="$RECENT" SHOWALL="$SHOWALL" CREWS="$CREWS" python3 -c '
import sys, json, os, datetime, re
from collections import defaultdict

recent_days = int(os.environ.get("RECENT","3"))
showall     = os.environ.get("SHOWALL","0") == "1"
crews       = os.environ.get("CREWS","")
cut = (datetime.date.today() - datetime.timedelta(days=recent_days)).isoformat()

try:
    d = json.load(sys.stdin)
except Exception:
    d = []
if isinstance(d, dict):
    d = d.get("issues") or next(iter(d.values()), [])

def codename(x):
    for l in (x.get("labels") or []):
        if str(l).startswith("codename:"):
            return str(l).split(":",1)[1]
    return "(ungrouped)"

groups = defaultdict(list)
for x in d:
    groups[codename(x)].append(x)

GLYPH = {"in_progress":"🔨","blocked":"⛔","open":"○","closed":"✅"}
ORDER = {"in_progress":0,"blocked":1,"open":2,"closed":3}

def is_recent_close(x):
    return x.get("status")=="closed" and (x.get("updated_at") or "")[:10] >= cut

def score(items):
    ip = sum(1 for x in items if x.get("status")=="in_progress")
    bl = sum(1 for x in items if x.get("status")=="blocked")
    rc = sum(1 for x in items if is_recent_close(x))
    op = sum(1 for x in items if x.get("status")=="open")
    return ip*1000 + bl*100 + rc*10 + op, (ip,bl,rc,op)

rows = []
for name, items in groups.items():
    s,(ip,bl,rc,op) = score(items)
    done = sum(1 for x in items if x.get("status")=="closed")
    active = ip>0 or bl>0 or rc>0
    rows.append((s, name, items, done, len(items), active, ip,bl,rc,op))
rows.sort(key=lambda r: (-r[0], r[1]))

print("═"*70)
print("  HARMONIK PLAYING FIELD — by epic   (recent = last %d days)" % recent_days)
if crews: print("  crews on shift: " + crews)
print("═"*70)
print("  🔨 doing   ⛔ blocked   ○ next   ✅ done      [done/total per epic]")

BULLET_CAP = 8
shown_idle = []
for s, name, items, done, total, active, ip,bl,rc,op in rows:
    if not active and not showall:
        shown_idle.append((name, done, total, op))
        continue
    print()
    tag = "  ● %-26s [%d/%d]" % (name[:26], done, total)
    print(tag)
    # order: doing, blocked, next(open), recent-done
    def bucket(x):
        st = x.get("status")
        if st=="closed": return 3 if is_recent_close(x) else 9
        return ORDER.get(st, 8)
    vis = [x for x in items if bucket(x) < 9]
    vis.sort(key=lambda x:(bucket(x), x.get("id","")))
    n=0
    for x in vis:
        if n>=BULLET_CAP:
            print("      … +%d more" % (len(vis)-n)); break
        st=x.get("status"); g=GLYPH.get(st,"·")
        who=x.get("assignee")
        wtag = (" @"+who) if (who and st=="in_progress") else ""
        print("      %s %-9s %s%s" % (g, x.get("id",""), str(x.get("title",""))[:52], wtag))
        n+=1

# collapsed idle/done epics
if shown_idle and not showall:
    print()
    print("  ── idle / no live work (collapsed) ─────────────────────────")
    shown_idle.sort(key=lambda r:(-(r[1]/r[2] if r[2] else 0), r[0]))
    line=[]
    for name,done,total,op in shown_idle:
        mark = "✅" if done==total and total>0 else "○"
        line.append("%s %s %d/%d" % (mark, name[:18], done, total))
    # print 2 per row
    for i in range(0,len(line),2):
        print("    " + "   ".join(line[i:i+2]))
print("═"*70)
print("  full list: --all   |   named initiatives: .harmonik/crew/admiral-initiatives.md")
'
