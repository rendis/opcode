# ETL with Validation

Extract data from a source API, transform it via a shell script, validate the output against a JSON schema, and load it to a destination endpoint. Includes retry with exponential backoff on the source fetch and a fallback step if transformation fails.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| source_url | string | yes | URL to fetch source data from |
| destination_url | string | yes | URL to POST transformed data to |
| expected_schema | object | no | JSON schema for validation (has default) |

## Steps

`extract` -> `transform` (fallback: `transform-fallback`) -> `validate` -> `load`

## Features

- HTTP GET with retry (max 3, exponential backoff, 15s cap)
- Shell script transformation with fallback on failure
- Schema assertion before loading
- HTTP POST to destination with retry
