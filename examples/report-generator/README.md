# Report Generator

Fetch sales, user, and system metrics data from three APIs in parallel, merge them into a unified report using a Go script, write to disk, and distribute via email API.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| sales_api_url | string | yes | API endpoint for sales data |
| users_api_url | string | yes | API endpoint for user data |
| metrics_api_url | string | yes | API endpoint for system metrics |
| report_path | string | yes | File path to write the generated report |
| email_api_url | string | yes | API endpoint for sending email distribution |

## Steps

`fetch-data` (parallel: `fetch-sales`, `fetch-users`, `fetch-metrics`) -> `merge-report` -> `write-report` -> `distribute-report`

## Features

- Parallel HTTP GET for three data sources with retry
- Go script for merging data into structured report format
- fs.write to persist the report
- HTTP POST to distribute the report via email API
