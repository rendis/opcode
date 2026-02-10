#!/usr/bin/env python3
"""Merge tickets, PRs, and logs into a markdown-formatted standup report."""

import json
import sys
from datetime import datetime, timezone


def main():
    data = json.load(sys.stdin)

    tickets = data.get("tickets", {}).get("body", [])
    prs = data.get("prs", {}).get("body", [])
    logs = data.get("logs", {}).get("body", [])

    today = datetime.now(timezone.utc).strftime("%Y-%m-%d")
    items_count = len(tickets) + len(prs) + len(logs)

    lines = [f"# Standup Report — {today}", ""]

    lines.append("## Tickets")
    if tickets:
        for t in tickets:
            title = t.get("title", "Untitled")
            status = t.get("status", "unknown")
            lines.append(f"- **{title}** ({status})")
    else:
        lines.append("- No open tickets")
    lines.append("")

    lines.append("## Pull Requests")
    if prs:
        for pr in prs:
            title = pr.get("title", "Untitled")
            state = pr.get("state", "unknown")
            lines.append(f"- **{title}** [{state}]")
    else:
        lines.append("- No recent PRs")
    lines.append("")

    lines.append("## Activity Logs")
    if logs:
        for log in logs:
            msg = log.get("message", log.get("text", str(log)))
            lines.append(f"- {msg}")
    else:
        lines.append("- No recent activity")
    lines.append("")

    report = "\n".join(lines)

    result = {
        "report": report,
        "date": today,
        "items_count": items_count,
    }

    json.dump(result, sys.stdout)


if __name__ == "__main__":
    main()
