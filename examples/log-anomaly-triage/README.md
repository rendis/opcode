# Log Anomaly Triage

Read a log file, run a Python analysis script to detect anomalies, triage any findings via agent reasoning, and send a notification webhook.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| log_path | string | yes | Path to the log file to analyze |
| notification_url | string | yes | Webhook URL for anomaly notifications |

## Steps

`read-logs` -> `analyze` -> `check-anomaly` -> (if anomaly) `triage` -> `notify` / (if clean) `log-clean`

## Features

- File system read for log ingestion
- Python script for anomaly detection
- Condition branching on analysis results
- Reasoning node for escalation triage
- HTTP POST notification with retry
