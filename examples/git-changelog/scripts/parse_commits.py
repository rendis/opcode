#!/usr/bin/env python3
"""parse_commits.py - Reads git log --oneline output from stdin, parses
conventional commit messages, and outputs a structured summary.

Input (stdin):  git log --oneline output (e.g. "abc1234 feat: add login")
Output (stdout): {"commits": [...], "has_breaking_changes": bool, "features": N, "fixes": N, "total": N}
"""

import sys
import json
import re

def main():
    raw = sys.stdin.read().strip()
    if not raw:
        print(json.dumps({
            "commits": [],
            "has_breaking_changes": False,
            "features": 0,
            "fixes": 0,
            "total": 0
        }))
        sys.exit(0)

    lines = raw.split("\n")
    commits = []
    features = 0
    fixes = 0
    has_breaking = False

    for line in lines:
        line = line.strip()
        if not line:
            continue

        # Parse "hash message" format
        parts = line.split(" ", 1)
        if len(parts) < 2:
            continue

        sha = parts[0]
        message = parts[1]

        # Detect conventional commit type
        match = re.match(r'^(\w+)(\(.+?\))?(!)?:\s*(.+)$', message)
        commit_type = "other"
        breaking = False

        if match:
            commit_type = match.group(1)
            breaking = match.group(3) == "!" or "BREAKING CHANGE" in message
        elif "BREAKING CHANGE" in message:
            breaking = True

        if commit_type == "feat":
            features += 1
        elif commit_type == "fix":
            fixes += 1

        if breaking:
            has_breaking = True

        commits.append({
            "sha": sha,
            "message": message,
            "type": commit_type,
            "breaking": breaking
        })

    result = {
        "commits": commits,
        "has_breaking_changes": has_breaking,
        "features": features,
        "fixes": fixes,
        "total": len(commits)
    }

    print(json.dumps(result))

if __name__ == "__main__":
    main()
