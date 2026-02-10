# Image Processing

Read source image metadata, process it through a shell script for resizing and optimization, write the processed output to disk, and upload to a CDN.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| source_path | string | yes | File path to the source image |
| output_path | string | yes | File path to write the processed image |
| cdn_upload_url | string | yes | CDN upload API endpoint |
| max_width | integer | no | Maximum width in pixels (default: 1200) |
| quality | integer | no | Compression quality 1-100 (default: 85) |

## Steps

`read-source` -> `process-image` -> `write-output` -> `upload-cdn`

## Features

- fs.stat reads source image metadata before processing
- Shell script handles resize/optimize (simulated in test, production uses ImageMagick)
- fs.write persists the processed image locally
- HTTP upload to CDN with retry and exponential backoff
