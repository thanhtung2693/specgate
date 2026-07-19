import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const TYPE_LABELS = new Set(["bug", "enhancement", "documentation", "question"]);
const SCOPE_LABELS = new Set([
  "agents",
  "cli",
  "doc-registry",
  "docs",
  "github-actions",
  "plugins",
  "release",
  "ui",
]);

const LABEL_DEFINITIONS = {
  bug: {
    color: "d73a4a",
    description: "Something is not working",
  },
  documentation: {
    color: "0075ca",
    description: "Documentation updates",
  },
  enhancement: {
    color: "a2eeef",
    description: "New feature or request",
  },
  question: {
    color: "d876e3",
    description: "Further information is requested",
  },
  "needs-triage": {
    color: "fbca04",
    description: "Needs maintainer triage",
  },
  agents: {
    color: "5319e7",
    description: "Governance-ops or agent runtime",
  },
  cli: {
    color: "1d76db",
    description: "SpecGate CLI",
  },
  "doc-registry": {
    color: "0e8a16",
    description: "Doc Registry service or API",
  },
  docs: {
    color: "0075ca",
    description: "Documentation",
  },
  "github-actions": {
    color: "bfdadc",
    description: "GitHub Actions or repository automation",
  },
  plugins: {
    color: "c5def5",
    description: "IDE plugins or agent packages",
  },
  release: {
    color: "d93f0b",
    description: "Release, installer, or published artifacts",
  },
  ui: {
    color: "a2eeef",
    description: "Web UI",
  },
};

const RULES = [
  {
    label: "bug",
    patterns: [
      /\bbug\b/i,
      /\bcrash(?:es|ing)?\b/i,
      /\berror\b/i,
      /\bfail(?:s|ed|ing|ure)?\b/i,
      /\bbroken\b/i,
      /\bnot working\b/i,
      /\bregression\b/i,
    ],
  },
  {
    label: "enhancement",
    patterns: [
      /\bfeature\b/i,
      /\badd\b/i,
      /\bsupport\b/i,
      /\bimprove(?:ment)?\b/i,
      /\brequest\b/i,
      /\bproposal\b/i,
    ],
  },
  {
    label: "documentation",
    patterns: [/\bdocs?\b/i, /\bdocumentation\b/i, /\breadme\b/i, /\bquickstart\b/i],
  },
  {
    label: "question",
    patterns: [/\bquestion\b/i, /\bhow do i\b/i, /\bhow to\b/i, /\bhelp\b/i],
  },
  {
    label: "cli",
    patterns: [
      /\bcli\b/i,
      /\bcommand\b/i,
      /\bdoctor\b/i,
      /\binit\b/i,
      /\buninstall\b/i,
      /\blocal-status\b/i,
      /\bspecgate\s+[a-z-]+\b/i,
    ],
  },
  {
    label: "ui",
    patterns: [/\bui\b/i, /\bweb ui\b/i, /\bfrontend\b/i, /\bbrowser\b/i, /\bdashboard\b/i],
  },
  {
    label: "doc-registry",
    patterns: [
      /\bdoc[- ]registry\b/i,
      /\bapi\b/i,
      /\bserver\b/i,
      /\bartifact\b/i,
      /\bworkboard\b/i,
      /\bpostgres\b/i,
    ],
  },
  {
    label: "agents",
    patterns: [/\bagents?\b/i, /\blanggraph\b/i, /\bmodel\b/i, /\bopenrouter\b/i, /\bgates?\b/i],
  },
  {
    label: "docs",
    patterns: [/\bdocs?\b/i, /\bdocumentation\b/i, /\breadme\b/i, /\bquickstart\b/i],
  },
  {
    label: "github-actions",
    patterns: [/\bgithub actions?\b/i, /\bworkflow\b/i, /\bci\b/i],
  },
  {
    label: "plugins",
    patterns: [/\bplugins?\b/i, /\bide\b/i, /\bcodex\b/i, /\bclaude\b/i, /\bcursor\b/i],
  },
  {
    label: "release",
    patterns: [/\brelease\b/i, /\binstaller\b/i, /\bimage\b/i, /\bghcr\b/i, /\bgoreleaser\b/i],
  },
];

export function classifyIssue({ title = "", body = "", labels = [] }) {
  const text = `${title}\n${body}`;
  const existing = new Set(labels.map((label) => label.toLowerCase()));
  const next = new Set();

  for (const rule of RULES) {
    if (!existing.has(rule.label) && rule.patterns.some((pattern) => pattern.test(text))) {
      next.add(rule.label);
    }
  }

  const hasType = [...existing, ...next].some((label) => TYPE_LABELS.has(label));
  const hasScope = [...existing, ...next].some((label) => SCOPE_LABELS.has(label));
  if (!hasType && !hasScope && !existing.has("needs-triage")) {
    next.add("needs-triage");
  }

  return [...next].sort();
}

async function githubRequest(path, { method = "GET", body } = {}) {
  const token = process.env.GITHUB_TOKEN;
  const repository = process.env.GITHUB_REPOSITORY;
  if (!token) throw new Error("GITHUB_TOKEN is required");
  if (!repository) throw new Error("GITHUB_REPOSITORY is required");

  const response = await fetch(`https://api.github.com/repos/${repository}${path}`, {
    method,
    headers: {
      accept: "application/vnd.github+json",
      authorization: `Bearer ${token}`,
      "content-type": "application/json",
      "x-github-api-version": "2022-11-28",
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });

  if (response.status === 204) return null;
  const text = await response.text();
  const data = text ? JSON.parse(text) : null;
  if (!response.ok) {
    const message = data?.message || `${method} ${path} failed with ${response.status}`;
    const error = new Error(message);
    error.status = response.status;
    throw error;
  }
  return data;
}

async function ensureLabel(label) {
  const definition = LABEL_DEFINITIONS[label];
  if (!definition) return;
  try {
    await githubRequest(`/labels/${encodeURIComponent(label)}`);
  } catch (error) {
    if (error.status !== 404) throw error;
    await githubRequest("/labels", {
      method: "POST",
      body: {
        name: label,
        color: definition.color,
        description: definition.description,
      },
    });
  }
}

export async function labelIssueFromEvent(event) {
  const issue = event.issue;
  if (!issue?.number || issue.pull_request) return [];

  const labels = classifyIssue({
    title: issue.title,
    body: issue.body || "",
    labels: (issue.labels || []).map((label) => label.name || label),
  });
  if (labels.length === 0) return [];

  for (const label of labels) {
    await ensureLabel(label);
  }
  await githubRequest(`/issues/${issue.number}/labels`, {
    method: "POST",
    body: { labels },
  });
  return labels;
}

async function main() {
  const eventPath = process.env.GITHUB_EVENT_PATH;
  if (!eventPath) throw new Error("GITHUB_EVENT_PATH is required");
  const event = JSON.parse(readFileSync(eventPath, "utf8"));
  const labels = await labelIssueFromEvent(event);
  console.log(labels.length ? `Added labels: ${labels.join(", ")}` : "No labels to add.");
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
}
