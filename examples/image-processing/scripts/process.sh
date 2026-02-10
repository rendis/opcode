#!/usr/bin/env bash
# process.sh - Process an image (resize/optimize).
#
# Input (stdin):  JSON with source_path, output_path, max_width, quality, original_size
# Output (stdout): {"original_size": N, "processed_size": N, "format": "...", "dimensions": "...", "processed_data": "..."}
#
# In production, this would use ImageMagick or similar.
# For testing, it simulates processing by copying the file and reporting metadata.

set -euo pipefail

input=$(cat)

python3 -c "
import json, sys, os, shutil

data = json.loads(sys.argv[1])
source = data.get('source_path', '')
output = data.get('output_path', '')
max_width = int(data.get('max_width', 1200) or 1200)
quality = int(data.get('quality', 85) or 85)
original_size = int(data.get('original_size', 0) or 0)

# Detect format from extension
ext = os.path.splitext(source)[1].lower().lstrip('.')
format_map = {
    'jpg': 'jpeg', 'jpeg': 'jpeg', 'png': 'png',
    'gif': 'gif', 'webp': 'webp', 'bmp': 'bitmap'
}
fmt = format_map.get(ext, ext or 'unknown')

# Simulate processing: copy source to output
if source and output and os.path.exists(source):
    shutil.copy2(source, output)
    processed_size = os.path.getsize(output)
else:
    processed_size = int(original_size * 0.7)  # Simulated 30% reduction

# Simulated dimensions based on max_width
dimensions = f'{max_width}x{int(max_width * 0.75)}'

result = {
    'original_size': original_size,
    'processed_size': processed_size,
    'format': fmt,
    'dimensions': dimensions,
    'processed_data': f'[processed image data: {fmt}, {dimensions}, q={quality}]'
}
print(json.dumps(result))
" "$input"
