# Secret Rotation

Generate a new UUID secret, push it to a service endpoint, verify the response status, and log an audit trail. Uses `on_complete` and `on_error` hooks for audit logging.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| service_url | string | yes | Service endpoint to update the secret on |
| service_name | string | yes | Name of the service for audit logging |

## Steps

`generate-secret` -> `update-service` -> `verify-update` -> `log-rotation`

## Features

- UUID generation for new secrets
- HTTP POST with retry and exponential backoff
- Response status assertion for verification
- `on_complete` hook for success audit logging
- `on_error` hook for failure audit logging
