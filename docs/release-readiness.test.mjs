import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";

const root = new URL("../", import.meta.url);
const read = (path) => readFileSync(new URL(path, root), "utf8");

const readme = read("README.md");
const quickstart = read("docs/quickstart.md");
const docRegistrySpec = read("app/doc-registry/docs/spec.md");
const contracts = read("docs/contracts.md");
const dataModel = read("docs/data-model.md");
const mcpCatalog = read("app/doc-registry/internal/mcp/catalog.go");
const uiIndex = read("app/ui/index.html");
const landingIndex = read("app/landing/index.html");
const rootIgnore = read(".gitignore");
const docRegistryDockerfile = read("docker/Dockerfile.doc-registry");
const releaseWorkflow = read(".github/workflows/release.yml");
const pagesWorkflow = read(".github/workflows/pages.yml");
const readinessWorkflow = read(".github/workflows/release-readiness.yml");
const uiWorkflow = read(".github/workflows/ui.yml");
const releaseCompose = read("deploy/compose/compose.yml");
const rootEnvExample = read(".env.example");
const releaseAgentsEnvExample = read("deploy/compose/agents.env.example");
const appAgentsReadme = read("app/agents/docs/README.md");
const uiDockerfile = read("docker/Dockerfile.ui");
const uiReadme = read("app/ui/README.md");
const landingReadme = read("app/landing/README.md");
const deployReadme = read("deploy/README.md");
const installCli = read("scripts/install-cli.sh");

const trackedFiles = () =>
  execFileSync("git", ["ls-files", "-z"], { cwd: root })
    .toString("utf8")
    .split("\0")
    .filter(Boolean);

const staleScanExtensions = new Set([
  ".css",
  ".env",
  ".example",
  ".go",
  ".html",
  ".js",
  ".json",
  ".md",
  ".mdc",
  ".migration",
  ".mjs",
  ".py",
  ".sh",
  ".tmpl",
  ".toml",
  ".ts",
  ".tsx",
  ".txt",
  ".yaml",
  ".yml",
]);

const staleScanBasenames = new Set([
  ".env.example",
  ".gitignore",
  "AGENTS",
  "AGENTS.md",
  "CLAUDE.md",
  "Dockerfile",
  "Makefile",
]);

const shouldScanForStaleTerms = (path) => {
  if (
    path === "docs/release-readiness.test.mjs" ||
    path.startsWith("app/agents/.venv/") ||
    path.startsWith("app/ui/node_modules/") ||
    path.startsWith("node_modules/") ||
    path.startsWith("dist/") ||
    path.startsWith("build/")
  ) {
    return false;
  }
  const name = path.split("/").at(-1);
  if (staleScanBasenames.has(name)) return true;
  const dot = name.lastIndexOf(".");
  const ext = dot >= 0 ? name.slice(dot) : "";
  return staleScanExtensions.has(ext);
};

const staleReleaseTerms = [
  /elements[-_ ]?ai/i,
  /@ai-elements/i,
  /ai-elements/i,
  /AI Elements/,
  /shadcn AI/,
  /aset_feature_mock_ui/,
  /feature_mock_ui/,
  /mock[-_ ]?UI/i,
  /mock_ui/,
  /\/mock-ui\b/,
  /Figma MCP/,
  /External MCP/,
  /GET \/mcp\/servers/,
  /app's single model/,
  /app's one model/,
  /run_fake_judge_contract/,
  /Phase 1A/,
  /Phase 1B/,
  /Phase 2/,
  /Phase 3/,
  /phase1a/,
  /phase1b/,
  /phase3/,
  /legacy cards/,
  /drafting node/,
  /governance_ui_delta/,
];

const staleTermAllowlist = new Map([
  [
    "app/doc-registry/internal/api/schemas_artifacts.go",
    [/enum:"phase1,phase2"/],
  ],
]);

test("README positions the release as alpha and CLI-first", () => {
  assert.match(readme, /Status: alpha/i);
  assert.match(readme, /specgate init/);
  assert.match(readme, /CLI-first/i);
  assert.match(readme, /APIs and UI may change/i);
});

test("installation instructions point at the release installer and IDE installer", () => {
  assert.match(readme, /raw\.githubusercontent\.com\/thanhtung2693\/specgate\/main\/scripts\/install-cli\.sh/);
  assert.match(readme, /raw\.githubusercontent\.com\/thanhtung2693\/specgate\/main\/plugins\/install\.sh/);
  assert.match(quickstart, /specgate init --seed --no-input/);
});

test("CLI installer resolves alpha prereleases from GitHub releases", () => {
  assert.match(installCli, /api\.github\.com\/repos\/\$\{GITHUB_REPO\}\/releases"/);
  assert.doesNotMatch(installCli, /releases\/latest/);
});

test("machine-local SpecGate artifacts stay out of release sources", () => {
  assert.match(rootIgnore, /^\.mcp\.json$/m);
  assert.match(rootIgnore, /^CLAUDE\.specgate\.md$/m);
  assert.match(rootIgnore, /^AGENTS\.specgate\.md$/m);
  assert.match(rootIgnore, /^deploy\/compose\/doc-registry\.env$/m);
});

test("release images use production-safe runtime defaults", () => {
  assert.match(docRegistryDockerfile, /mkdir -p \/data/);
  assert.match(docRegistryDockerfile, /chown app:app \/data/);
  assert.match(releaseWorkflow, /dockerfile: docker\/Dockerfile\.ui/);
  assert.match(releaseWorkflow, /runner: ubuntu-24\.04-arm/);
  assert.match(releaseWorkflow, /--platform "\$\{\{ matrix\.platform \}\}"/);
  assert.match(releaseWorkflow, /push-by-digest=true/);
  assert.match(releaseWorkflow, /actions\/upload-artifact@v7/);
  assert.match(releaseWorkflow, /actions\/download-artifact@v7/);
  assert.match(releaseWorkflow, /scope=\$\{\{ matrix\.image \}\}-\$\{\{ matrix\.arch \}\}/);
  assert.match(releaseWorkflow, /--provenance=false/);
  assert.match(releaseWorkflow, /docker buildx imagetools create/);
  assert.match(releaseWorkflow, /--tag "\$\{image\}:\$\{\{ github\.ref_name \}\}"/);
  assert.match(releaseCompose, /ghcr\.io\/thanhtung2693\/ui:\$\{SPECGATE_VERSION\}/);
  assert.match(releaseCompose, /\$\{UI_PORT:-3000\}:80/);
  assert.match(releaseCompose, /wget -qO- http:\/\/127\.0\.0\.1\/healthz \|\| exit 1/);
  assert.match(uiDockerfile, /ARG VITE_DOC_REGISTRY_URL=\/api\/doc-registry/);
  assert.match(uiDockerfile, /ARG VITE_LANGGRAPH_API_URL=\/api\/agents/);
});

test("release compose defaults to file storage without Redis", () => {
  assert.doesNotMatch(releaseCompose, /^\s{2}redis:/m);
  assert.doesNotMatch(releaseCompose, /REDIS_URL:/);
  assert.doesNotMatch(releaseCompose, /redis-data:/);
});

test("release compose allows side-by-side local stacks", () => {
  assert.match(releaseCompose, /name: \$\{SPECGATE_COMPOSE_PROJECT:-specgate\}/);
  assert.match(releaseCompose, /127\.0\.0\.1:\$\{POSTGRES_PORT:-5432\}:5432/);
  assert.match(releaseCompose, /\$\{DOC_REGISTRY_PORT:-8080\}:8080/);
  assert.match(releaseCompose, /\$\{AGENTS_PORT:-2024\}:8000/);
});

test("env examples stay lean and settings-first", () => {
  assert.doesNotMatch(rootEnvExample, /QUEUE_DRIVER|STORAGE_DRIVER|KNOWLEDGE_DRIVER/);
  assert.doesNotMatch(releaseAgentsEnvExample, /OPENAI_API_KEY|GOOGLE_API_KEY|ANTHROPIC_API_KEY|OPENROUTER_API_KEY/);
  assert.match(releaseAgentsEnvExample, /LANGSMITH_API_KEY=/);
  assert.doesNotMatch(releaseAgentsEnvExample, /required/i);
  assert.doesNotMatch(deployReadme, /LangSmith API key \(required/i);
});

test("published docs do not contain corrupted tracker placeholder substitutions", () => {
  const corrupted = /(?:\{fixes SPECGATE|[A-Za-z_]+_fixes|webhook-fixes|SPECGATE-\{key\|SPECGATE-\{key\|id\})/;
  assert.doesNotMatch(docRegistrySpec, corrupted);
  assert.doesNotMatch(contracts, corrupted);
});

test("work discovery contract documents workspace filtering", () => {
  assert.match(docRegistrySpec, /list_work_items\(ready\?, handed_off\?, work_type\?, workspace_id\?, mine\?, limit\?\)/);
  assert.match(contracts, /list_work_items\(ready\?, handed_off\?, work_type\?, workspace_id\?, mine\?, limit\?\)/);
  assert.match(dataModel, /workspace_id` for attribution and selection scoping/);
  assert.match(mcpCatalog, /"workspace_id": "string\?"/);
});

test("release-facing docs have no pending social or live-verification placeholders", () => {
  assert.doesNotMatch(uiIndex, /Replace with a 1200×630 social card/);
  assert.doesNotMatch(contracts, /Live verification PENDING/);
});

test("UI shell exposes experimental browser metadata", () => {
  assert.match(uiIndex, /<title>SpecGate \(Experimental\)<\/title>/);
  assert.match(uiIndex, /rel="icon"[^>]+href="\/logo\.svg"/);
  assert.match(uiReadme, /SpecGate \(Experimental\)/);
});

test("landing metadata points at shipped logo assets", () => {
  assert.match(landingIndex, /rel="icon"[^>]+href="\.\/logo\.svg"/);
  assert.match(landingIndex, /property="og:image" content="https:\/\/thanhtung2693\.github\.io\/specgate\/logo\.svg"/);
  assert.match(landingIndex, /name="twitter:image" content="https:\/\/thanhtung2693\.github\.io\/specgate\/logo\.svg"/);
  assert.doesNotMatch(landingIndex, /\/images\/specgate-black\.svg/);
  assert.doesNotMatch(landingIndex, /specgate\.io/);
});

test("GitHub Pages deploys only the static landing site", () => {
  assert.match(pagesWorkflow, /name: pages/);
  assert.match(pagesWorkflow, /push:\n\s+branches: \["main"\]/);
  assert.doesNotMatch(pagesWorkflow, /\n\s+paths:/);
  assert.match(pagesWorkflow, /actions\/upload-pages-artifact@v5/);
  assert.match(pagesWorkflow, /actions\/deploy-pages@v5/);
  assert.match(pagesWorkflow, /path: app\/landing/);
  assert.doesNotMatch(pagesWorkflow, /npm ci|npm run build|app\/ui/);
  assert.match(landingReadme, /GitHub Pages/);
  assert.match(landingReadme, /app\/landing/);
  assert.match(landingReadme, /avoids path filters/);
});

test("Node-backed workflows use Node 24 compatible setup-node actions", () => {
  const nodeWorkflows = [pagesWorkflow, readinessWorkflow, uiWorkflow].join("\n");

  assert.doesNotMatch(nodeWorkflows, /actions\/setup-node@v4/);
  assert.match(pagesWorkflow, /actions\/setup-node@v6/);
  assert.match(readinessWorkflow, /actions\/setup-node@v6/);
  assert.match(uiWorkflow, /actions\/setup-node@v6/);
});

test("user docs present CLI-first handoff and optional model setup consistently", () => {
  const docsReadme = read("docs/README.md");
  assert.match(docsReadme, /optional model setup/);
  assert.doesNotMatch(docsReadme, /configured model/);
  assert.match(readme, /raw\.githubusercontent\.com\/thanhtung2693\/specgate\/main\/plugins\/install\.sh/);
  assert.match(quickstart, /Coding IDE agents use the CLI/);
});

test("release-facing docs use governance-ops terminology", () => {
  const combined = [
    readme,
    quickstart,
    read("docs/README.md"),
    contracts,
    docRegistrySpec,
    appAgentsReadme,
    uiReadme,
    uiIndex,
    deployReadme,
  ].join("\n");

  assert.doesNotMatch(combined, /planner chat/i);
  assert.match(combined, /governance-ops/);
});

test("tracked release sources do not reintroduce retired terminology", () => {
  const violations = [];

  for (const path of trackedFiles()) {
    if (!shouldScanForStaleTerms(path)) continue;
    if (!existsSync(new URL(path, root))) continue;
    const content = read(path);
    const allowlist = staleTermAllowlist.get(path) ?? [];

    for (const pattern of staleReleaseTerms) {
      pattern.lastIndex = 0;
      if (!pattern.test(content)) continue;
      if (allowlist.some((allowed) => allowed.test(content))) continue;
      violations.push(`${path}: ${pattern}`);
    }
  }

  assert.deepEqual(violations, []);
});
