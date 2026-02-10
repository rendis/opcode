# Webhook Handler

Validate an incoming webhook payload against a JSON schema, transform and enrich it via a Node.js script, then forward the result to a destination API with retry.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| payload | object | yes | The incoming webhook payload to process |
| destination_url | string | yes | Destination API URL to forward the transformed payload to |
| expected_source | string | no | Expected source identifier for validation |

## Steps

`validate-payload` -> `transform` -> `forward`

## Features

- Schema validation using assert.schema before processing
- Node.js transform script for payload enrichment and normalization
- HTTP POST with exponential backoff retry for reliable delivery
- Structured error handling — validation failure halts the pipeline
