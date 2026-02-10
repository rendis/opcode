# Batch File Processor

List files in an input directory, loop over each entry to read, compute a SHA-256 hash, and write to an output directory with the hash prepended to the filename.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| input_dir | string | yes | Directory containing files to process |
| output_dir | string | yes | Directory to write processed files to |

## Steps

`list-files` -> `process-each` (for_each: `read-file` -> `hash-content` -> `write-output`)

## Features

- File system listing and iteration
- For-each loop over directory entries
- SHA-256 content hashing per file
- Content-addressable output filenames (hash prefix)
