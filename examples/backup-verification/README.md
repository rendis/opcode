# Backup Verification

Verify that backups exist and are fresh using a shell script. If the backup is stale (older than the configured threshold), raise a reasoning decision for manual backup or investigation.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| backup_dir | string | yes | Directory path where backups are stored |
| max_age_hours | integer | yes | Maximum acceptable backup age in hours |

## Steps

`check-backup` -> `verify-exists` -> `check-freshness` -> `log-ok` / `stale-decision`

## Features

- Shell script for backup file discovery and age calculation
- Nested condition branches (exists, then freshness)
- Reasoning node with 30m timeout and fallback to investigate
- workflow.log for healthy and missing backup states
