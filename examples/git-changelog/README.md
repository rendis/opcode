# Git Changelog

Extract recent git commits, parse them for conventional commit types, detect breaking changes, and prompt for release type via reasoning before writing a CHANGELOG entry.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| repo_dir | string | yes | Path to the git repository |
| changelog_path | string | yes | Path to the CHANGELOG file to update |

## Steps

`git-log` -> `parse-commits` -> `check-breaking` -> `release-decision` / `log-no-breaking` -> `write-changelog`

## Features

- shell.exec for git log extraction
- Python script for conventional commit parsing
- Condition on breaking changes detection
- Reasoning node for release version type decision (major/minor/patch)
- fs.append to update the CHANGELOG file
