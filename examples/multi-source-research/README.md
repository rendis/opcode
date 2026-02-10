# Multi-Source Research

Fetch data from three sources in parallel, have an agent evaluate which source provides the best data, then log the selected source.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| source_a_url | string | yes | URL for source A |
| source_b_url | string | yes | URL for source B |
| source_c_url | string | yes | URL for source C |

## Steps

`fetch-all` (parallel: fetch-a, fetch-b, fetch-c) -> `pick-best` -> `route-selection` (condition: log-a / log-b / log-c)

## Features

- Parallel HTTP fetches across three data sources
- Reasoning node for agent-driven source evaluation
- Condition branching based on reasoning decision
- Retry with linear backoff on all fetches
