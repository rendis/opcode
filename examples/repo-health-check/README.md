# Repo Health Check

Run three checks in parallel (git status, go test, go vet), evaluate results, and raise a reasoning decision if any checks fail.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| repo_dir | string | yes | Path to the repository to check |

## Steps

`run-checks` (parallel: `check-clean`, `run-tests`, `run-vet`) -> `evaluate-results` -> `log-healthy` / `issue-decision` -> `log-issues`

## Features

- Parallel execution of three independent checks
- shell.exec for `git status --porcelain`, `go test ./...`, `go vet ./...`
- on_error ignore on all checks so parallel completes even with failures
- Condition evaluating all three exit codes
- Reasoning node for triage with investigate/defer/ignore options
- Data injection of all check outputs for informed decision-making
