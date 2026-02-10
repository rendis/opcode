# Knowledge Base Updater

Fetch source content, compute its SHA-256 hash for change detection, and conditionally update a knowledge base file. An agent reviews new content for relevance before appending.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| source_url | string | yes | URL to fetch the source content from |
| last_hash | string | yes | SHA-256 hash of the last known content version |
| kb_path | string | yes | File path to the knowledge base file to append to |

## Steps

`fetch-source` -> `hash-content` -> `check-changed` -> (if changed) `review-relevance` -> `check-relevant` -> `append-kb` / `log-skip`

## Features

- HTTP fetch with retry for source retrieval
- Cryptographic hashing for change detection
- Nested condition branching (changed? then relevant?)
- Reasoning node for agent-driven relevance review
- File append to incrementally build the knowledge base
