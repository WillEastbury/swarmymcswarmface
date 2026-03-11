#!/usr/bin/env node
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import process from "node:process";

const execFileAsync = promisify(execFile);

const SPECIALIZATIONS = {
  backend: {
    objective:
      "Implement robust backend changes with explicit validation, clear error handling, and safe migrations.",
    focus: [
      "API contract stability",
      "Correctness and edge cases",
      "Performance and data integrity"
    ]
  },
  frontend: {
    objective:
      "Implement user-facing changes with predictable UX, accessible interaction patterns, and resilient state handling.",
    focus: ["Accessibility", "Rendering states", "Error/retry UX"]
  },
  security: {
    objective:
      "Deliver secure changes with threat-aware design and no silent risk acceptance.",
    focus: [
      "AuthN/AuthZ checks",
      "Input/output validation",
      "Secrets and dependency safety"
    ]
  },
  testing: {
    objective:
      "Generate strong tests that cover primary flows, edge cases, and regressions implied by the issue.",
    focus: ["Deterministic tests", "Coverage gaps", "Failure diagnostics"]
  }
};

function parseArgs(argv) {
  const map = new Map();
  for (let i = 2; i < argv.length; i += 1) {
    const key = argv[i];
    const value = argv[i + 1];
    if (key.startsWith("--")) map.set(key.slice(2), value);
  }
  return {
    repo: map.get("repo"),
    issue: map.get("issue"),
    specialization: map.get("specialization") || "backend",
    model: map.get("model") || "gpt-5.3-codex"
  };
}

async function ghJson(args) {
  const { stdout } = await execFileAsync("gh", args, { maxBuffer: 10 * 1024 * 1024 });
  return JSON.parse(stdout);
}

async function fetchIssueContext(repo, issueNumber) {
  const [issue, comments] = await Promise.all([
    ghJson(["api", `repos/${repo}/issues/${issueNumber}`]),
    ghJson(["api", `repos/${repo}/issues/${issueNumber}/comments`])
  ]);

  const normalizedComments = comments.map((c) => ({
    author: c.user?.login,
    created_at: c.created_at,
    body: c.body
  }));

  return {
    number: issue.number,
    title: issue.title,
    body: issue.body,
    labels: issue.labels?.map((l) => l.name) || [],
    author: issue.user?.login,
    comments: normalizedComments
  };
}

function buildPrompt({ issue, specialization }) {
  const profile = SPECIALIZATIONS[specialization] || SPECIALIZATIONS.backend;
  return `
You are an autonomous issue worker in a production SDLC swarm.

SPECIALIZATION: ${specialization}
PRIMARY OBJECTIVE: ${profile.objective}
FOCUS AREAS:
${profile.focus.map((x) => `- ${x}`).join("\n")}

OPERATING CONSTRAINTS:
- Work only on this issue scope; no speculative unrelated changes.
- Use explicit, non-silent error handling.
- Preserve existing behavior unless issue acceptance criteria require changes.
- Favor secure defaults and minimal privilege assumptions.

ISSUE:
#${issue.number} ${issue.title}
Labels: ${issue.labels.join(", ") || "(none)"}
Author: ${issue.author || "unknown"}

Issue Body:
${issue.body || "(empty)"}

Recent Comments:
${issue.comments
  .slice(-8)
  .map((c) => `- ${c.author}: ${c.body?.slice(0, 1200) || "(empty)"}`)
  .join("\n") || "(none)"}

TASK:
1) Produce a concise execution plan.
2) Implement required code changes.
3) Run available tests/lint/build for touched surfaces.
4) Summarize result with files changed + risk notes.
`;
}

async function loadCopilotSdk() {
  const mod = await import("@github/copilot-sdk");
  const clientFactory =
    mod.createCopilotClient ||
    mod.createClient ||
    mod.CopilotClient ||
    mod.default;

  if (!clientFactory) {
    throw new Error(
      "Copilot SDK loaded but no known client factory was found. Update loadCopilotSdk() for your installed SDK version."
    );
  }
  return { mod, clientFactory };
}

async function runWorker({ repo, issue, specialization, model }) {
  const issueContext = await fetchIssueContext(repo, issue);
  const prompt = buildPrompt({ issue: issueContext, specialization });

  const { clientFactory } = await loadCopilotSdk();
  const client =
    typeof clientFactory === "function"
      ? await clientFactory({
          model,
          sandbox: { mode: "workspace-only" }
        })
      : clientFactory;

  if (!client || typeof client.run !== "function") {
    throw new Error(
      "Unsupported Copilot SDK shape: expected a client with .run(...). Adapt runWorker() to your SDK API."
    );
  }

  const result = await client.run({
    task: prompt,
    metadata: {
      repo,
      issue: String(issue),
      specialization
    }
  });

  return result;
}

async function main() {
  const args = parseArgs(process.argv);
  if (!args.repo || !args.issue) {
    console.error(
      "Usage: node src/issue-worker.js --repo owner/name --issue 123 --specialization backend|frontend|security|testing [--model gpt-5.3-codex]"
    );
    process.exit(1);
  }

  const issueNumber = Number(args.issue);
  if (Number.isNaN(issueNumber)) {
    console.error("--issue must be a number");
    process.exit(1);
  }

  console.log(
    `[worker] starting issue #${issueNumber} (${args.specialization}) for ${args.repo} with model ${args.model}`
  );

  try {
    const result = await runWorker({
      repo: args.repo,
      issue: issueNumber,
      specialization: args.specialization,
      model: args.model
    });
    console.log("[worker] completed");
    console.log(JSON.stringify(result, null, 2));
  } catch (error) {
    console.error("[worker] failed:", error.message);
    process.exit(1);
  }
}

main();
