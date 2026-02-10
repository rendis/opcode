#!/usr/bin/env bash
# check_version.sh - Reads git ls-remote --tags output from stdin,
# compares the latest tag with the current version argument,
# and outputs whether an update is available.
#
# Usage: check_version.sh <current_version>
# Input (stdin): git ls-remote --tags output
# Output (stdout): {"update_available": bool, "current_version": "...", "latest_version": "...", "tag_ref": "..."}

set -euo pipefail

CURRENT_VERSION="${1:-v0.0.0}"

# Read tags from stdin, extract version tags, sort by version, pick latest
TAGS_INPUT=$(cat)

if [ -z "$TAGS_INPUT" ]; then
  python3 -c "
import json, sys
print(json.dumps({
    'update_available': False,
    'current_version': sys.argv[1],
    'latest_version': sys.argv[1],
    'tag_ref': ''
}))
" "$CURRENT_VERSION"
  exit 0
fi

# Parse tags: extract refs/tags/vX.Y.Z lines, strip ^{} derefs, get unique versions
LATEST_TAG=$(echo "$TAGS_INPUT" \
  | grep -oP 'refs/tags/v[\d.]+' \
  | sed 's|refs/tags/||' \
  | sort -t. -k1,1n -k2,2n -k3,3n \
  | tail -1)

if [ -z "$LATEST_TAG" ]; then
  LATEST_TAG="$CURRENT_VERSION"
fi

# Compare versions
python3 -c "
import json, sys, re

def parse_version(v):
    v = re.sub(r'^v', '', v)
    parts = v.split('.')
    return tuple(int(p) for p in parts if p.isdigit())

current = sys.argv[1]
latest = sys.argv[2]

current_tuple = parse_version(current)
latest_tuple = parse_version(latest)

update_available = latest_tuple > current_tuple

print(json.dumps({
    'update_available': update_available,
    'current_version': current,
    'latest_version': latest,
    'tag_ref': latest if update_available else ''
}))
" "$CURRENT_VERSION" "$LATEST_TAG"
