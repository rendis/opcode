# Digest Builder

Loop over a list of source URLs, fetch each one (tolerating failures), format the results into a markdown digest, and send it via an email/notification API.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| sources | array | yes | Array of URLs to fetch content from |
| send_api_url | string | yes | Email/notification API endpoint |
| recipient | string | yes | Recipient email or channel ID |

## Steps

`fetch-sources` (loop for_each) -> `format-digest` -> `send-digest`

## Features

- Loop iterates over arbitrary list of source URLs
- on_error ignore ensures one failed fetch does not block the digest
- Shell script formats all results into a markdown document
- HTTP retry with exponential backoff on send
