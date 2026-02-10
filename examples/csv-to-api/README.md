# CSV to API

Read a CSV file from disk, parse it into JSON rows using a Python script, then POST each row to an API endpoint in a loop.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| csv_path | string | yes | Path to the CSV file to process |
| api_url | string | yes | API endpoint to POST each row to |

## Steps

`read-csv` -> `parse-csv` -> `upload-rows` (loop: `post-row`) -> `log-summary`

## Features

- fs.read to load CSV content from disk
- Shell script (Python) for CSV-to-JSON parsing
- for_each loop over parsed rows (up to 1000)
- HTTP POST with retry and exponential backoff per row
- workflow.log summary with total row count
