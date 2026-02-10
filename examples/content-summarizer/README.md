# Content Summarizer

Fetch a URL, extract and summarize its text content via a shell script, get agent approval on summary quality, then write the result to a file.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| url | string | yes | URL to fetch and summarize |
| output_path | string | yes | File path to write the final summary |

## Steps

`fetch` -> `extract` -> `review-quality` -> `write-result`

## Features

- HTTP fetch with retry and exponential backoff
- Shell script for text extraction and summarization
- Reasoning node for human/agent quality review
- File system write for persisting results
