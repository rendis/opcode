#!/usr/bin/env python3
"""parse_errors.py - Reads JSON log entries from stdin, groups by error type,
and outputs a summary with new error counts.

Input (stdin):  JSON array of log entries, each with at least {"type": "...", "message": "...", "timestamp": "..."}
Output (stdout): {"new_errors_count": N, "groups": [{"type": "...", "count": N, "sample": "..."}], "total_entries": N}
"""

import sys
import json
from collections import defaultdict

def main():
    try:
        data = json.load(sys.stdin)
    except json.JSONDecodeError:
        print(json.dumps({"new_errors_count": 0, "groups": [], "total_entries": 0}))
        sys.exit(0)

    entries = data if isinstance(data, list) else data.get("entries", [])
    total = len(entries)

    groups = defaultdict(lambda: {"count": 0, "sample": ""})
    for entry in entries:
        err_type = entry.get("type", "unknown")
        groups[err_type]["count"] += 1
        if not groups[err_type]["sample"]:
            groups[err_type]["sample"] = entry.get("message", "")[:200]

    result = {
        "new_errors_count": len(groups),
        "groups": [
            {"type": t, "count": g["count"], "sample": g["sample"]}
            for t, g in sorted(groups.items(), key=lambda x: -x[1]["count"])
        ],
        "total_entries": total
    }

    print(json.dumps(result))

if __name__ == "__main__":
    main()
