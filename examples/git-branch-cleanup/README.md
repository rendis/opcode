# Git Branch Cleanup

List branches that have been merged into main, loop over each one, skip protected branches (main, develop, master), and delete the rest.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| repo_dir | string | yes | Path to the git repository |
| protected_branches | string | no | Comma-separated list of branches to never delete (default: main,develop) |

## Steps

`list-merged` -> `parse-branches` -> `cleanup-loop` (loop: `check-protected` -> `delete-branch` -> `log-deleted` / `log-skipped`)

## Features

- shell.exec for `git branch --merged main`
- Inline Python to parse branch list into JSON array
- for_each loop over branches
- Condition to skip protected branches (main, develop, master)
- shell.exec for `git branch -d` to delete each safe branch
- workflow.log for deleted and skipped branches
