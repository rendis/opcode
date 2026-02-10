#!/usr/bin/env bash
# extract.sh - Reads JSON from stdin (HTTP response), extracts text content,
# and outputs a summary JSON object.
#
# Input (stdin):  JSON with a "body" field containing HTML/text content
# Output (stdout): {"summary": "...", "word_count": N}

set -euo pipefail

input=$(cat)

# Extract body field from JSON input
body=$(echo "$input" | python3 -c "
import sys, json, re, html
data = json.load(sys.stdin)
text = data.get('body', '')
# Strip HTML tags
text = re.sub(r'<[^>]+>', ' ', text)
# Decode HTML entities
text = html.unescape(text)
# Collapse whitespace
text = re.sub(r'\s+', ' ', text).strip()
print(text)
")

# Count words
word_count=$(echo "$body" | wc -w | tr -d ' ')

# Create a truncated summary (first 200 words)
summary=$(echo "$body" | awk '{
  for (i=1; i<=NF && i<=200; i++) printf "%s ", $i
}' | sed 's/ $//')

# Output as JSON
python3 -c "
import json, sys
summary = sys.argv[1]
word_count = int(sys.argv[2])
print(json.dumps({'summary': summary, 'word_count': word_count}))
" "$summary" "$word_count"
