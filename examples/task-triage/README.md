# Task Triage

Fetch new tickets from an API, loop through each one for agent-driven priority classification, and update each ticket with the assigned priority.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| tickets_url | string | yes | API URL to fetch new tickets (returns JSON array) |
| update_url | string | yes | API URL to POST ticket updates to |

## Steps

`fetch-tickets` -> `triage-loop` (for each ticket: `prioritize` -> `update-ticket`)

## Features

- HTTP fetch for ticket retrieval with retry
- For-each loop over ticket array
- Reasoning node with four priority levels per ticket
- Data injection of current ticket into reasoning context
- HTTP POST to update each ticket with assigned priority
- Workflow timeout suspends (allows resume) instead of failing
