# Dependency Audit

Scan project dependencies for vulnerabilities, triage findings via agent reasoning (patch now vs defer), and send a notification webhook with the decision and vulnerability details.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| project_dir | string | no | Project directory to scan |
| notification_url | string | yes | Webhook URL for vulnerability notifications |

## Steps

`scan` -> `check-vulns` -> (if vulns) `triage` -> `notify-vulns` / (if clean) `log-clean`

## Features

- Shell script for dependency vulnerability scanning
- Condition branching on scan results
- Reasoning node for patch vs defer triage
- HTTP POST notification with retry
- Fallback to immediate patching on reasoning timeout
