#!/usr/bin/env node
/**
 * Transform an incoming webhook payload into a normalized format.
 * Reads JSON from stdin, enriches and transforms it, outputs JSON to stdout.
 */

const chunks = [];

process.stdin.setEncoding("utf8");

process.stdin.on("data", (chunk) => {
  chunks.push(chunk);
});

process.stdin.on("end", () => {
  try {
    const input = JSON.parse(chunks.join(""));

    const transformed = {
      event_type: (input.event || "unknown").toLowerCase().replace(/\s+/g, "_"),
      payload: input.data || {},
      metadata: {
        original_source: input.source || "unknown",
        original_timestamp: input.timestamp || new Date().toISOString(),
        processed_at: new Date().toISOString(),
        processor: "opcode-webhook-handler",
      },
    };

    // Flatten nested data keys for easier downstream consumption
    if (typeof transformed.payload === "object") {
      transformed.payload_keys = Object.keys(transformed.payload);
    }

    process.stdout.write(JSON.stringify(transformed));
  } catch (err) {
    process.stderr.write(`Transform error: ${err.message}`);
    process.exit(1);
  }
});
