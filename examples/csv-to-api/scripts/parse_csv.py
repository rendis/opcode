#!/usr/bin/env python3
"""parse_csv.py - Reads CSV text from a file path (argv[1]) or stdin and outputs a JSON array of row objects.

Input:  CSV file path as first argument, or CSV text on stdin
Output (stdout): {"rows": [{"field1": "val", "field2": "val"}, ...], "count": N}
"""

import sys
import json
import csv
import io

def main():
    if len(sys.argv) > 1:
        with open(sys.argv[1], 'r') as f:
            raw = f.read().strip()
    else:
        raw = sys.stdin.read().strip()

    if not raw:
        print(json.dumps({"rows": [], "count": 0}))
        sys.exit(0)

    reader = csv.DictReader(io.StringIO(raw))
    rows = [dict(row) for row in reader]

    result = {
        "rows": rows,
        "count": len(rows)
    }
    print(json.dumps(result))

if __name__ == "__main__":
    main()
