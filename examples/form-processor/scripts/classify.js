#!/usr/bin/env node
// classify.js - Classify form submissions by content.
//
// Input (stdin):  JSON object with name, email, subject, message fields
// Output (stdout): {"type": "support|sales|general", "priority": "low|medium|high", "summary": "..."}

const chunks = [];
process.stdin.on("data", (chunk) => chunks.push(chunk));
process.stdin.on("end", () => {
  const input = JSON.parse(Buffer.concat(chunks).toString());
  const text = `${input.subject} ${input.message}`.toLowerCase();

  let type = "general";
  let priority = "low";

  // Support keywords
  const supportKeywords = [
    "bug",
    "error",
    "broken",
    "issue",
    "help",
    "not working",
    "crash",
    "fix",
    "problem",
    "support",
    "ticket",
  ];
  // Sales keywords
  const salesKeywords = [
    "pricing",
    "demo",
    "trial",
    "enterprise",
    "quote",
    "buy",
    "purchase",
    "plan",
    "upgrade",
    "contract",
    "proposal",
  ];

  const supportScore = supportKeywords.filter((kw) =>
    text.includes(kw)
  ).length;
  const salesScore = salesKeywords.filter((kw) => text.includes(kw)).length;

  if (supportScore > salesScore && supportScore > 0) {
    type = "support";
  } else if (salesScore > supportScore && salesScore > 0) {
    type = "sales";
  }

  // Priority based on urgency signals
  const urgentWords = [
    "urgent",
    "critical",
    "asap",
    "immediately",
    "emergency",
    "down",
    "outage",
  ];
  const mediumWords = ["important", "soon", "needed", "waiting", "blocked"];

  if (urgentWords.some((w) => text.includes(w))) {
    priority = "high";
  } else if (mediumWords.some((w) => text.includes(w))) {
    priority = "medium";
  }

  // Generate summary (first 100 chars of message)
  const summary =
    input.message.length > 100
      ? input.message.substring(0, 100) + "..."
      : input.message;

  console.log(JSON.stringify({ type, priority, summary }));
});
