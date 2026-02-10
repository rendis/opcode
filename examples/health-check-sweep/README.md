# Health Check Sweep

Check multiple service endpoints in parallel, then run a validation loop across service types using crypto hashing as a dummy check, and log results.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| endpoint_a | string | yes | Health check URL for endpoint A |
| endpoint_b | string | yes | Health check URL for endpoint B |
| endpoint_c | string | yes | Health check URL for endpoint C |

## Steps

`check-endpoints` (parallel: check-a, check-b, check-c) -> `validate-services` (for_each: hash-check) -> `log-results`

## Features

- Parallel health checks across three endpoints
- Error strategy `ignore` so one failure does not block others
- For-each loop over service types with crypto hash validation
- Retry with linear backoff on all HTTP calls
