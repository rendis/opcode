# OpenClaw Version Checker

Scheduled workflow that checks a remote git repository for new version tags, compares against the current installed version, installs updates if available, and sends a notification.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| repo_url | string | yes | Git repository URL to check for new tags |
| current_version | string | yes | Currently installed version (e.g. `v1.2.3`) |
| install_dir | string | yes | Directory where the tool is installed |
| notification_url | string | no | Webhook URL for update notifications |

## Steps

`fetch-tags` -> `check-version` -> `route-update` -> `install-update` -> `log-updated` -> `notify-update` / `log-up-to-date`

## Features

- shell.exec for `git ls-remote --tags` to fetch remote tags
- Bash script for semantic version comparison
- Condition branch on update availability
- shell.exec for `git fetch --tags && git checkout` to install update
- Optional HTTP POST notification (conditional on notification_url)
- Designed for periodic scheduled execution
