# Meeting Prep Brief

Gather calendar events, related documents, and past meeting notes from three APIs in parallel, then compile everything into a single briefing file.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| calendar_url | string | yes | API URL to fetch upcoming calendar events |
| documents_url | string | yes | API URL to fetch related documents |
| notes_url | string | yes | API URL to fetch past meeting notes |
| output_path | string | yes | File path to write the briefing document |

## Steps

`gather-sources` (parallel: `fetch-calendar`, `fetch-documents`, `fetch-notes`) -> `log-merge` -> `write-briefing`

## Features

- Parallel HTTP fetches for all three data sources
- Retry with exponential backoff on each fetch
- Workflow log for merge confirmation
- File write combining all sources into a structured briefing
