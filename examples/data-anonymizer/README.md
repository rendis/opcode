# Data Anonymizer

Read a JSON dataset, hash specified PII fields using SHA-256 via crypto.hash, then write the anonymized result to a new file.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| input_path | string | yes | Path to the input JSON dataset file |
| output_path | string | yes | Path to write the anonymized output |
| pii_fields | array | yes | List of field names to anonymize (e.g. `["email", "name", "phone"]`) |

## Steps

`read-data` -> `hash-fields` (loop: `hash-field`) -> `build-anonymized` -> `write-output`

## Features

- fs.read to load the dataset
- for_each loop over PII field names
- crypto.hash (SHA-256) for each PII field
- Inline Python to merge hashes into dataset records
- fs.write for the anonymized output
