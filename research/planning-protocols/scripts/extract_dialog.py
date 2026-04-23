#!/usr/bin/env python3
"""Extract dialog-only markdown from a Claude Code JSONL session transcript.

Filters:
- tool_result "user" events (content is array, not string) -> excluded
- isSidechain == true events (sub-agent activity) -> excluded
- All non-user/assistant metadata events -> excluded

Preserves:
- Human text turns verbatim
- Agent text responses (text content blocks)
- Tool activity as one-line summaries (name x count) per agent turn
- Timestamps (with caveat: long gaps may be user stepping away)

usage: extract_dialog.py <jsonl_path> [out_path]
"""
import json
import sys
from datetime import datetime
from collections import Counter
from pathlib import Path


def parse_ts(s):
    if not s:
        return None
    try:
        return datetime.fromisoformat(s.replace('Z', '+00:00'))
    except Exception:
        return None


def format_duration(seconds):
    if seconds is None or seconds < 1:
        return "<1s"
    if seconds < 60:
        return f"{int(seconds)}s"
    if seconds < 3600:
        return f"{int(seconds // 60)}m"
    h = int(seconds // 3600)
    m = int((seconds % 3600) // 60)
    return f"{h}h {m}m"


def content_text(content):
    """Concatenate all text blocks from assistant content (list or str)."""
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts = []
        for b in content:
            if isinstance(b, dict) and b.get('type') == 'text':
                parts.append(b.get('text', ''))
        return '\n'.join(p for p in parts if p)
    return ''


def content_tools(content):
    """Return list of tool names from tool_use blocks."""
    tools = []
    if isinstance(content, list):
        for b in content:
            if isinstance(b, dict) and b.get('type') == 'tool_use':
                tools.append(b.get('name', '?'))
    return tools


def is_human_text(ev):
    if ev.get('type') != 'user':
        return False
    if ev.get('isSidechain') is True:
        return False
    msg = ev.get('message') or {}
    content = msg.get('content')
    return isinstance(content, str) and content.strip()


def is_main_assistant(ev):
    if ev.get('type') != 'assistant':
        return False
    if ev.get('isSidechain') is True:
        return False
    return True


def extract(jsonl_path):
    events = []
    with open(jsonl_path) as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                events.append(json.loads(line))
            except json.JSONDecodeError:
                continue

    turns = []
    buf_text = []
    buf_tools = Counter()
    buf_start = None
    buf_end = None

    def flush():
        nonlocal buf_text, buf_tools, buf_start, buf_end
        if buf_text or buf_tools:
            dur = None
            if buf_start and buf_end:
                dur = (buf_end - buf_start).total_seconds()
            turns.append({
                'role': 'agent',
                'text': '\n\n'.join(t for t in buf_text if t.strip()),
                'tools': dict(buf_tools),
                'ts_start': buf_start,
                'ts_end': buf_end,
                'duration': dur,
            })
            buf_text = []
            buf_tools = Counter()
            buf_start = None
            buf_end = None

    for ev in events:
        ts = parse_ts(ev.get('timestamp'))
        if is_human_text(ev):
            flush()
            turns.append({
                'role': 'human',
                'text': ev['message']['content'],
                'ts': ts,
            })
        elif is_main_assistant(ev):
            content = (ev.get('message') or {}).get('content', [])
            text = content_text(content)
            tools = content_tools(content)
            if text:
                buf_text.append(text)
            for t in tools:
                buf_tools[t] += 1
            if ts:
                if buf_start is None:
                    buf_start = ts
                buf_end = ts
    flush()

    # Build markdown
    path = Path(jsonl_path)
    session_id = path.stem
    project = path.parent.name

    first_ts = next((t.get('ts') or t.get('ts_start') for t in turns
                     if t.get('ts') or t.get('ts_start')), None)
    last_ts = None
    for t in reversed(turns):
        last_ts = t.get('ts') or t.get('ts_end')
        if last_ts:
            break

    human_turns = sum(1 for t in turns if t['role'] == 'human')
    agent_turns = sum(1 for t in turns if t['role'] == 'agent')

    lines = []
    lines.append(f"# Session: {session_id}")
    lines.append("")
    lines.append(f"- Project dir: `{project}`")
    lines.append(f"- Source: `{jsonl_path}`")
    if first_ts:
        lines.append(f"- First event: {first_ts.isoformat()}")
    if last_ts:
        lines.append(f"- Last event: {last_ts.isoformat()}")
    lines.append(f"- Human turns: {human_turns}")
    lines.append(f"- Agent turns: {agent_turns}")
    lines.append("")
    lines.append("> Extracted dialog only. Tool activity elided to one-line summaries per agent turn. "
                 "`[AUTONOMOUS RUN]` markers flag agent turns longer than 5 min or with more than 20 tool calls. "
                 "Sub-agent sidechain events and tool_result events are filtered out.")
    lines.append("> Timestamps preserved but may include long pauses where the user stepped away. "
                 "Treat as rough signal, not truth.")
    lines.append("")
    lines.append("---")
    lines.append("")

    prev_ts = None
    human_idx = 0
    agent_idx = 0
    for t in turns:
        if t['role'] == 'human':
            human_idx += 1
            ts = t.get('ts')
            ts_str = ts.strftime('%Y-%m-%d %H:%M') if ts else '?'
            gap = ''
            if prev_ts and ts:
                gap_sec = (ts - prev_ts).total_seconds()
                if gap_sec > 60:
                    gap = f" — gap {format_duration(gap_sec)} from prior"
            lines.append(f"## Human #{human_idx} — {ts_str}{gap}")
            lines.append("")
            lines.append(t['text'].rstrip())
            lines.append("")
            if ts:
                prev_ts = ts
        else:
            agent_idx += 1
            dur = t.get('duration') or 0
            tools = t.get('tools') or {}
            n_tools = sum(tools.values())
            ts = t.get('ts_start')
            ts_str = ts.strftime('%Y-%m-%d %H:%M') if ts else '?'
            dur_str = format_duration(dur) if dur else '~'
            tools_str = ', '.join(f"{n}×{c}" for n, c in sorted(tools.items(), key=lambda x: -x[1]))
            autonomous = dur > 300 or n_tools > 20
            marker = " [AUTONOMOUS RUN]" if autonomous else ""
            lines.append(f"## Agent #{agent_idx} — {ts_str} ({dur_str}, {n_tools} tool calls){marker}")
            lines.append("")
            if tools:
                lines.append(f"_Tool activity: {tools_str}_")
                lines.append("")
            if t['text'].strip():
                lines.append(t['text'].rstrip())
                lines.append("")
            else:
                lines.append("_(no agent text; tool execution only)_")
                lines.append("")
            if t.get('ts_end'):
                prev_ts = t['ts_end']

    return '\n'.join(lines)


if __name__ == '__main__':
    if len(sys.argv) < 2:
        print("usage: extract_dialog.py <jsonl_path> [out_path]", file=sys.stderr)
        sys.exit(1)
    result = extract(sys.argv[1])
    if len(sys.argv) >= 3:
        Path(sys.argv[2]).write_text(result)
        print(f"wrote {sys.argv[2]} ({len(result)} chars)", file=sys.stderr)
    else:
        print(result)
