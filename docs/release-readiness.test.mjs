import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";

const root = new URL("../", import.meta.url);
const read = (path) => readFileSync(new URL(path, root), "utf8");

const trackedFiles = () =>
  execFileSync("git", ["ls-files", "-z"], { cwd: root })
    .toString("utf8")
    .split("\0")
    .filter((path) => path && existsSync(new URL(path, root)));

const docs = {
  readme: read("README.md"),
  security: read("SECURITY.md"),
  quickstart: read("docs/using-specgate/quickstart.md"),
  featureStatus: read("docs/using-specgate/reference/feature-status.md"),
  howSpecGateWorks: read("docs/using-specgate/concepts/how-specgate-works.md"),
  integrationGuide: read("docs/using-specgate/guides/connect-integrations.md"),
  dataModel: read("docs/contributing/data-model.md"),
  trustAndSecurity: read("docs/using-specgate/concepts/trust-and-security.md"),
  governanceAndGates: read("docs/using-specgate/concepts/governance-and-gates.md"),
  deliveryTrustADR: read("docs/contributing/adr/2026-07-07-delivery-trust-model.md"),
  teamIntegrationsADR: read("docs/contributing/adr/2026-07-20-minimal-team-integrations.md"),
  operateSpecGate: read("docs/using-specgate/guides/operate-specgate.md"),
  contracts: read("docs/contributing/contracts.md"),
  configReference: read("docs/using-specgate/reference/configuration.md"),
  configureModels: read("docs/using-specgate/guides/configure-models.md"),
  installIdePlugins: read("docs/using-specgate/guides/install-ide-plugins.md"),
  cliWorkflow: read("docs/using-specgate/guides/cli-workflow.md"),
  codingAgentWorkflow: read("docs/using-specgate/guides/coding-agent-workflow.md"),
  respondToGateFailures: read("docs/using-specgate/guides/respond-to-gate-failures.md"),
  cliReference: read("docs/using-specgate/reference/cli.md"),
  governanceReference: read("docs/using-specgate/reference/governance.md"),
  gatesReference: read("docs/using-specgate/reference/gates.md"),
  deployReadme: read("deploy/README.md"),
  docRegistrySpec: read("app/doc-registry/docs/spec.md"),
  docRegistryReadme: read("app/doc-registry/docs/README.md"),
  agentsReadme: read("app/agents/docs/README.md"),
  architecture: read("docs/contributing/architecture.md"),
  testing: read("docs/contributing/testing.md"),
  release: read("docs/contributing/release.md"),
};

const files = {
  rootIgnore: read(".gitignore"),
  rootMakefile: read("Makefile"),
  installCli: read("scripts/install-cli.sh"),
  releaseWorkflow: read(".github/workflows/release.yml"),
  goreleaser: read(".goreleaser.yaml"),
  pagesWorkflow: read(".github/workflows/pages.yml"),
  readinessWorkflow: read(".github/workflows/release-readiness.yml"),
  uiWorkflow: read(".github/workflows/ui.yml"),
  issueLabelerWorkflow: read(".github/workflows/issue-labeler.yml"),
  rootEnvExample: read(".env.example"),
  docRegistryEnvExample: read("app/doc-registry/.env.example"),
  docRegistryConfig: read("app/doc-registry/internal/config/config.go"),
  docRegistrySettings: read("app/doc-registry/internal/settings/model.go"),
  uiModelSettings: read("app/ui/src/data/model-settings.ts"),
  uiModelSettingsPanel: read("app/ui/src/components/layout/settings/model-settings-panel.tsx"),
  uiGovernanceAgent: read("app/ui/src/components/agent/governance-agent.tsx"),
  workboardModel: read("app/doc-registry/internal/workboard/model.go"),
  routerSkill: read("plugins/skills/specgate/SKILL.md"),
  setupSkill: read("plugins/skills/specgate-project-setup/SKILL.md"),
  preparingWorkSkill: read("plugins/skills/specgate-work-preparation/SKILL.md"),
  deliveringWorkSkill: read("plugins/skills/specgate-work-delivery/SKILL.md"),
  pluginPackage: read("plugins/package.json"),
  sessionStartHook: read("plugins/hooks/session-start"),
  cursorRule: read("plugins/rules/using-specgate.mdc"),
  cursorPlugin: read("plugins/.cursor-plugin/plugin.json"),
  localDockerfile: read("docker/Dockerfile.local"),
  uiDockerfile: read("docker/Dockerfile.ui"),
  localGateway: read("docker/local/nginx.conf"),
  fullGateway: read("app/ui/docker/nginx-default.conf"),
  landingPage: read("app/landing/index.html"),
  docRegistryPrd: read("app/doc-registry/docs/prd.md"),
};

const releaseFacingDocs = [
  "README.md",
  "docs/using-specgate/quickstart.md",
  "docs/README.md",
  "docs/contributing/contracts.md",
  "docs/using-specgate/reference/cli.md",
  "docs/using-specgate/reference/configuration.md",
  "docs/using-specgate/guides/cli-workflow.md",
  "docs/using-specgate/guides/coding-agent-workflow.md",
  "docs/using-specgate/guides/install-ide-plugins.md",
  "deploy/README.md",
  "app/doc-registry/README.md",
  "app/doc-registry/docs/README.md",
  "app/doc-registry/docs/spec.md",
  "app/agents/README.md",
  "app/agents/docs/README.md",
  "app/ui/README.md",
];

const releaseDocsText = () => releaseFacingDocs.map(read).join("\n");

const uniqueMatches = (text, pattern) => [...new Set([...text.matchAll(pattern)].map(([, value]) => value))];

const assertMentions = (text, value, label = value) => {
  assert.match(text, new RegExp(`\\b${value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\b`), `${label} is not documented`);
};

test("release docs keep the public install path CLI-first", () => {
  const publicDocs = releaseDocsText();

  assert.match(docs.readme, /scripts\/install-cli\.sh/);
  assert.match(files.installCli, /releases\/latest/);
  assert.match(files.installCli, /releases\.atom/);
  assert.match(files.installCli, /STABLE_VERSION/);
  assert.doesNotMatch(files.installCli, /api\.github\.com/);
  assert.match(files.installCli, /"\$\{DEST\}" config server "\$\{SERVER_URL\}"/);
  assert.match(files.installCli, /Run 'specgate init'/);
  assert.doesNotMatch(files.installCli, /config set server/);
  assert.match(files.installCli, /Cannot verify the download/);
  assert.doesNotMatch(files.installCli, /skipping verification/);

  for (const text of [docs.readme, docs.quickstart, docs.installIdePlugins, docs.contracts]) {
    assert.match(text, /specgate plugins install/);
  }
  assert.match(docs.readme, /specgate plugins doctor/);
  assert.match(docs.installIdePlugins, /specgate plugins doctor/);
  assert.match(docs.contracts, /normal coding-agent workflow publishes through `specgate artifact publish`/);

  assert.doesNotMatch(publicDocs, /plugins\/install\.sh/);
  assert.doesNotMatch(publicDocs, /agent-handoff\.md/);
  assert.doesNotMatch(publicDocs, /ADMIN_SECRET/);
});

test("public docs accurately position the current v0.1 release", () => {
  assert.match(docs.readme, /\*\*Status: v0\.1 early release\.\*\*/);
  assert.match(docs.security, /The v0\.1 release also includes a web UI\./);
  assert.doesNotMatch(docs.security, /The alpha release/i);
  assert.match(docs.featureStatus, /ready for cautious\s+v0\.1 evaluation/i);
  assert.match(docs.featureStatus, /## Core v0\.1 paths/);
  assert.match(docs.quickstart, /Governance-chat threads reset when the appliance restarts/i);
  assert.match(docs.operateSpecGate, /Governance-chat threads are ephemeral/i);
});

test("team integrations expose the graduated minimal workflow only", () => {
  assert.match(docs.integrationGuide, /Full mode only/i);
  assert.match(docs.integrationGuide, /Repositories[\s\S]{0,160}GitHub[\s\S]{0,160}GitLab/);
  assert.match(docs.integrationGuide, /Work tracking[\s\S]{0,160}Linear/);
  assert.match(docs.integrationGuide, /authorize[\s\S]{0,160}select[\s\S]{0,160}managed[\s\S]{0,80}webhook/i);
  assert.match(docs.integrationGuide, /<!-- specgate-work-ref: CR-123 -->/);
  assert.match(docs.integrationGuide, /head_sha[\s\S]{0,160}head_revision/);
  assert.match(docs.integrationGuide, /directly to a coding IDE agent/i);
  assert.doesNotMatch(docs.integrationGuide, /CI setup|CI ingestion|issue handoff|tracker authority|public Context Pack URL/i);
  assert.doesNotMatch(docs.integrationGuide, /manually configure an integration-level webhook secret/i);
  assert.doesNotMatch(docs.readme, /Graduate tracker and git integrations for team use/i);
  assert.doesNotMatch(files.landingPage, /\bexperimental\s+(?:connectors?|integrations?|trackers?)\b/i);
  assert.doesNotMatch(files.landingPage, /\b(?:connectors?|integrations?)\b[^.\n]{0,80}\bmirror(?:s|ed|ing)?\b[^.\n]{0,80}\btracker\b/i);
});

test("provider CI ingestion is absent from the graduated integration contract", () => {
  const noProviderCI = [
    files.docRegistryPrd,
    docs.dataModel,
    docs.trustAndSecurity,
    docs.governanceAndGates,
    docs.deliveryTrustADR,
    docs.teamIntegrationsADR,
  ].join("\n");

  assert.doesNotMatch(noProviderCI, /CI or webhook events|CI, PR\/MR|matched merge and CI corroboration|managed webhook can transport a CI result/i);
  assert.match(noProviderCI, /user-cited|user-reported|externally supplied|user's own/i);
  assert.match(docs.deliveryTrustADR, /historical|superseded/i);
});

test("release docs do not advertise removed prose classification routes", () => {
  const currentCapabilityDocs = [
    docs.readme,
    docs.agentsReadme,
    docs.architecture,
    docs.testing,
    docs.configureModels,
  ].join("\n");

  assert.doesNotMatch(currentCapabilityDocs, /route suggestions/i);
  assert.doesNotMatch(currentCapabilityDocs, /route advice/i);
  assert.doesNotMatch(currentCapabilityDocs, /delivery review, classification/i);
  assert.doesNotMatch(currentCapabilityDocs, /summaries, and classification run through/i);

  for (const [label, text] of [
    ["Doc Registry spec", docs.docRegistrySpec],
    ["model settings panel", files.uiModelSettingsPanel],
    ["governance agent setup", files.uiGovernanceAgent],
  ]) {
    assert.doesNotMatch(text, /\bclassification\b/i, `${label} advertises removed classification work`);
  }
});

test("release purge docs preserve the cached appliance image", () => {
  assert.match(docs.release, /downloaded appliance image stays in Docker's cache/i);
  assert.doesNotMatch(docs.release, /no SpecGate-managed[\s\S]{0,100}appliance image remains/i);
});

test("release distribution is the single local appliance", () => {
  assert.match(files.rootIgnore, /^CLAUDE\.specgate\.md$/m);
  assert.match(files.rootIgnore, /^AGENTS\.specgate\.md$/m);

  assert.ok(!existsSync(new URL("deploy/compose/compose.yml", root)), "legacy multi-service release bundle remains");
  assert.doesNotMatch(docs.deployReadme, /multi-service Compose/i);
  assert.match(files.rootMakefile, /Contributor source integration:/);
  assert.doesNotMatch(files.rootMakefile, /Onboarding \(self-host\):/);
});

test("local and full gateways accept the documented upload limits", () => {
  for (const gateway of [files.localGateway, files.fullGateway]) {
    assert.match(gateway, /client_max_body_size 32m;/);
  }
});

test("local and full gateways route provider callbacks and managed webhooks", () => {
  for (const gateway of [files.localGateway, files.fullGateway]) {
    assert.match(gateway, /location = \/integrations\/oauth-callback/);
    assert.match(gateway, /location ~ \^\/integrations\/\[\^\/\]\+\/resources\/\[\^\/\]\+\/\(github\|gitlab\|linear\)\/webhook\$/);
    assert.match(gateway, /proxy_set_header Host \$http_host;/);
  }
});

test("release automation rejects an already-published GitHub Release before verification", () => {
  const workflow = files.releaseWorkflow;
  const verifyJob = workflow.slice(
    workflow.indexOf("\n  verify:\n"),
    workflow.indexOf("\n  release-cli:\n"),
  );
  const preflight = verifyJob.indexOf("Reject pre-published GitHub Release");
  const checkout = verifyJob.indexOf("actions/checkout@v6");

  assert.notEqual(preflight, -1, "tag releases need a published-release preflight");
  assert.ok(preflight < checkout, "release-state validation must run before expensive verification");
  assert.match(verifyJob, /\.tag_name == \$version and \.draft == false/);
  assert.match(verifyJob, /push the tag directly/i);
});

test("release guide tells maintainers to push a tag without publishing a GitHub Release", () => {
  assert.match(docs.release, /git push origin "\$VERSION"/);
  assert.match(docs.release, /Do not create or publish a GitHub Release/i);
});

test("release workflow builds only the appliance and exercises doctor repair", () => {
  const workflow = files.releaseWorkflow;
  const manifestJob = workflow.slice(
    workflow.indexOf("\n  release-image-manifests:\n"),
    workflow.indexOf("\n  release-local-bundle:\n"),
  );
  const smokeJob = workflow.slice(
    workflow.indexOf("\n  release-doctor-fix-smoke:\n"),
    workflow.indexOf("\n  release-publish:\n"),
  );
  const publishJob = workflow.slice(workflow.indexOf("\n  release-publish:\n"));

  assert.match(workflow, /^permissions:\n  contents: read$/m);
  assert.doesNotMatch(workflow, /^permissions:\n  contents: write\n  packages: write$/m);
  assert.match(workflow, /release-cli:\n[\s\S]*?permissions:\n\s+contents: write\n[\s\S]*?steps:/);
  assert.match(workflow, /release-images:\n[\s\S]*?permissions:\n\s+contents: read\n\s+packages: write\n[\s\S]*?strategy:/);
  assert.match(workflow, /release-image-manifests:\n[\s\S]*?permissions:\n\s+actions: read\n\s+packages: write\n[\s\S]*?steps:/);
  assert.match(smokeJob, /^    permissions:\n      contents: write$/m);
  assert.match(workflow, /release-publish:\n[\s\S]*?permissions:\n\s+contents: write\n[\s\S]*?steps:/);
  assert.match(workflow, /--file docker\/Dockerfile\.local/);
  assert.doesNotMatch(workflow, /image: \[(?:doc-registry|agents|ui)/);
  for (const dockerfile of ["docker/Dockerfile.doc-registry", "docker/Dockerfile.agents", "docker/Dockerfile.ui"]) {
    assert.doesNotMatch(workflow, new RegExp(`dockerfile: ${dockerfile.replace("/", "\\/")}`));
  }
  assert.match(workflow, /push-by-digest=true/);
  assert.match(workflow, /docker buildx imagetools create/);
  assert.doesNotMatch(manifestJob, /--tag "\$\{image\}:latest"/);
  assert.match(workflow, /actions\/upload-artifact@v7/);
  assert.match(workflow, /actions\/download-artifact@v7/);
  assert.match(workflow, /release-doctor-fix-smoke:/);
  assert.match(workflow, /needs: release-image-manifests/);
  assert.match(workflow, /specgate init --mode full --dir "\$DEPLOY_DIR" --bundle-version "\$VERSION" --no-seed --no-input/);
  assert.match(workflow, /specgate-deployment-v1/);
  assert.match(workflow, /\$DEPLOY_DIR\/\.specgate-managed/);
  assert.match(workflow, /specgate artifact publish --file/);
  assert.doesNotMatch(workflow, /gates_profile/);
  assert.match(workflow, /"request_type": "bugfix"/);
  assert.doesNotMatch(workflow, /"request_type": "bug_fix"/);
  assert.match(workflow, /specgate doctor --fix --yes/);
  assert.match(workflow, /GO_RELEASER_VERSION=v2\.17\.0/);
  assert.match(workflow, /goreleaser_Linux_x86_64\.tar\.gz/);
  assert.doesNotMatch(workflow, /goreleaser\/goreleaser-action/);
  assert.doesNotMatch(workflow, /go install github\.com\/goreleaser\/goreleaser/);
  assert.match(workflow, /goreleaser release --clean/);
  assert.match(files.goreleaser, /release:\s+[\s\S]*draft: true/);
  assert.match(workflow, /--provenance=mode=max/);
  assert.match(workflow, /--sbom=true/);
  assert.match(workflow, /anchore\/scan-action@v7\.4\.0/);
  assert.match(workflow, /severity-cutoff: high/);
  assert.match(workflow, /only-fixed: true/);
  assert.match(workflow, /release-publish:/);
  assert.match(workflow, /needs: release-doctor-fix-smoke/);
  assert.match(publishJob, /packages: write/);
  assert.match(publishJob, /if \[\[ "\$VERSION" == \*-\* \]\]; then[\s\S]*else[\s\S]*--tag "\$\{image\}:latest"/);
  assert.match(publishJob, /--tag "\$\{image\}:latest"/);
  assert.match(publishJob, /"\$\{image\}:\$VERSION"/);
  assert.match(workflow, /GH_REPO: \$\{\{ github\.repository \}\}/);
  assert.doesNotMatch(workflow, /gh release download "\$VERSION"/);
  assert.match(workflow, /gh api --paginate --slurp "repos\/\$\{GH_REPO\}\/releases"/);
  assert.match(workflow, /gh api -H "Accept: application\/octet-stream"/);
  assert.match(workflow, /gh release edit "\$VERSION" --draft=false/);
});

test("release workflow verifies every release-facing module before publishing", () => {
  const workflow = files.releaseWorkflow;
  const verifyJob = workflow.slice(
    workflow.indexOf("\n  verify:\n"),
    workflow.indexOf("\n  release-cli:\n"),
  );

  assert.notEqual(verifyJob, "", "release workflow must define a verify job before release-cli");
  assert.match(verifyJob, /go run honnef\.co\/go\/tools\/cmd\/staticcheck@v0\.7\.0 \.\/\.\.\./);
  assert.match(verifyJob, /go test -race -count=1 -p=1 \.\/\.\.\./);
  assert.match(verifyJob, /make test/);
  assert.match(verifyJob, /uv run ruff check \.\/src \.\/tests/);
  assert.match(verifyJob, /uv run pytest -q/);
  assert.match(verifyJob, /npm run test -- --run/);
  assert.match(verifyJob, /npm run lint/);
  assert.match(verifyJob, /npm run build/);
  assert.match(verifyJob, /node --test docs\/release-readiness\.test\.mjs/);
  assert.match(workflow, /release-cli:\n\s+needs: verify\n/);
});

test("release env examples document runtime config without requiring model secrets", () => {
  const docRegistryOperatorDocs = [
    files.docRegistryEnvExample,
    docs.deployReadme,
    docs.configReference,
    docs.docRegistryReadme,
    docs.docRegistrySpec,
  ].join("\n");
  const configEnvVars = uniqueMatches(files.docRegistryConfig, /\bgetEnv(?:Bool|Int|Int64|Float)?\("([A-Z0-9_]+)"/g);

  assert.ok(configEnvVars.length > 0, "no doc-registry config env vars found");
  for (const name of configEnvVars) {
    assertMentions(docRegistryOperatorDocs, name);
  }

  assert.doesNotMatch(files.rootEnvExample, /STORAGE_DRIVER|KNOWLEDGE_DRIVER/);
  assert.doesNotMatch(files.rootEnvExample, /^QUEUE_DRIVER/m);
});

test("shared contracts are generated from current backend vocabulary", () => {
  const warningCodes = uniqueMatches(
    files.workboardModel,
    /\bWarning[A-Za-z0-9_]+\s+WarningCode\s*=\s*"([^"]+)"/g,
  );

  assert.ok(warningCodes.length > 0, "no WarningCode constants found");
  for (const code of warningCodes) {
    assert.match(docs.contracts, new RegExp(`\\\`${code}\\\``));
    assert.match(docs.docRegistrySpec, new RegExp(`\\\`${code}\\\``));
  }

  assert.match(docs.docRegistrySpec, /workspace_id[\s\S]*scopes all reads and writes/);
  assert.match(docs.contracts, /workspace_id/);
});

test("model defaults and docs agree on low governance thinking", () => {
  assert.match(files.docRegistrySettings, /KeyGovernanceDefaultThinkingLevel:\s+"low"/);
  assert.match(files.uiModelSettings, /"governance\.default_thinking_level": "low"/);
  assert.match(docs.configureModels, /`low` is the runtime default/);
});

test("model docs distinguish IDE assistance from server-only features", () => {
  assert.match(docs.configureModels, /Local[\s\S]*IDE-agent semantic readiness/);
  assert.match(docs.configureModels, /agent_attested/);
  assert.match(docs.configureModels, /different review-only agent/);
  assert.match(docs.configureModels, /repository context, not Governance Knowledge/);
  assert.match(docs.installIdePlugins, /Local[\s\S]*same\s+focused[\s\S]*without\s+contacting a registry/i);
  assert.match(docs.codingAgentWorkflow, /artifact coverage[\s\S]*exact[- ]version/i);
  assert.doesNotMatch(docs.quickstart, /Configure a model when you want server-side summaries, route suggestions/);
  assert.doesNotMatch(docs.quickstart + docs.installIdePlugins, /Offline Local CLI/);
  assert.match(docs.installIdePlugins, /Codex[\s\S]{0,160}`\.agents\/skills\/specgate-\*`/i);
  assert.match(docs.installIdePlugins, /Claude Code[\s\S]{0,160}`\.claude\/skills\/specgate-\*`/i);
  assert.doesNotMatch(docs.installIdePlugins, /project-local marketplace configuration/i);

  for (const skill of [
    "specgate",
    "specgate-project-setup",
    "specgate-work-preparation",
    "specgate-work-delivery",
  ]) {
    assert.match(docs.installIdePlugins, new RegExp(`\\\`${skill}\\\``));
  }

  for (const reference of [docs.cliReference, docs.governanceReference, docs.gatesReference]) {
    assert.match(reference, /Local[\s\S]*IDE[\s-]*gate tasks/i);
    assert.match(reference, /not_run[\s\S]*until[\s\S]*submitted/i);
  }
});

test("SpecGate uses one short bootstrap and one explicit lifecycle phase", () => {
  const routerWords = files.routerSkill.trim().split(/\s+/).length;

  assert.match(files.routerSkill, /^description: Use when the user explicitly mentions SpecGate/m);
  assert.ok(routerWords <= 550, `router is ${routerWords} words; expected at most 550`);
  assert.match(files.routerSkill, /For lifecycle work, choose exactly one phase/is);
  assert.match(files.routerSkill, /exactly one.*setup.*prepar.*deliver/is);
  assert.match(files.routerSkill, /framework.*owns.*paths.*Git/is);
  assert.match(files.routerSkill, /readiness.*not.*approval/is);
  assert.match(files.routerSkill, /read-only[\s\S]{0,160}specgate change status "\$WORK_REF" --json/i);
  assert.match(files.routerSkill, /only product-state read and write surface/i);
  assert.match(files.routerSkill, /never inspect\s+or edit[\s\S]{0,200}SQLite[\s\S]{0,200}object storage/i);
  assert.match(files.routerSkill, /bootstrap[\s-]*only[\s\S]*`specgate-project-setup`[\s\S]*unavailable/i);
  assert.match(files.routerSkill, /skills\.sh[\s\S]*instructions only[\s\S]*explicit approval/i);
  assert.match(files.routerSkill, /raw\.githubusercontent\.com\/thanhtung2693\/specgate\/main\/scripts\/install-cli\.sh/);
  assert.match(files.routerSkill, /npx skills remove specgate -y/);
  assert.match(files.routerSkill, /npx skills remove specgate -g -y/);
  assert.match(files.routerSkill, /never edit or delete[\s\S]*skills\.sh[\s\S]*directly/i);
  assert.match(files.routerSkill, /plugins install[\s\S]*--dry-run[\s\S]*plugins install[\s\S]*plugins doctor/i);
  assert.match(files.routerSkill, /restart[\s\S]*stop before[\s\S]*initializ/i);

  assert.match(files.sessionStartHook, /SpecGate skills are installed/);
  assert.match(files.sessionStartHook, /explicitly mentions SpecGate/);
  assert.match(files.sessionStartHook, /Read-only.*stay in the root skill/is);
  assert.doesNotMatch(files.sessionStartHook, /SpecGate is connected/);
  assert.doesNotMatch(files.sessionStartHook, /SKILL_CONTENT=.*cat/);

  assert.match(files.cursorRule, /explicitly mentions SpecGate/);
  assert.match(files.cursorRule, /Read-only.*stay in the root skill/is);
  assert.doesNotMatch(files.cursorPlugin, /hooks\/hooks-cursor\.json/);
  assert.doesNotMatch(files.pluginPackage, /hooks\/hooks-cursor\.json/);
  assert.ok(!existsSync(new URL("plugins/hooks/hooks-cursor.json", root)), "dead Cursor bootstrap hook remains");
});

test("SpecGate exposes one product-named entry skill", () => {
  assert.ok(existsSync(new URL("plugins/skills/specgate/SKILL.md", root)));
  assert.ok(!existsSync(new URL("plugins/skills/specgate-router/SKILL.md", root)));
  assert.match(files.pluginPackage, /"skills\/specgate\/SKILL\.md"/);
  assert.doesNotMatch(files.pluginPackage, /specgate-router/);
});

test("skills.sh bootstrap hands plugin ownership to the SpecGate CLI", () => {
  assert.match(docs.readme, /skills\.sh[\s\S]{0,240}bootstrap/i);
  assert.match(docs.readme, /ask your\s+agent[\s\S]{0,160}set up SpecGate/i);
  assert.match(docs.installIdePlugins, /## Start from skills\.sh/i);
  assert.match(docs.installIdePlugins, /instructions only[\s\S]{0,240}SpecGate CLI/i);
  assert.match(docs.installIdePlugins, /npx skills remove specgate -y/);
  assert.match(docs.installIdePlugins, /npx skills remove specgate -g -y/);
  assert.match(docs.installIdePlugins, /sole manager|one owner/i);
  assert.match(docs.installIdePlugins, /never edits[\s\S]{0,160}skills\.sh lock/i);
  assert.match(docs.cliReference, /skills\.sh[\s\S]{0,240}`conflict`[\s\S]{0,240}retry_command/i);
  assert.match(files.pluginPackage, /"version": "0\.2\.3"/);
});

test("SpecGate project setup performs and verifies the requested setup", () => {
  const skill = files.setupSkill;

  assert.match(skill, /^description: Use when SpecGate is being (?:initialized|configured)/m);
  assert.match(skill, /command -v specgate/);
  assert.match(skill, /Get-Command specgate/);
  assert.match(skill, /lookup succeeds[\s\S]*specgate --version/i);
  assert.match(skill, /skills\.sh[\s\S]*does not install[\s\S]*CLI/i);
  assert.match(
    skill,
    /curl -fsSL https:\/\/raw\.githubusercontent\.com\/thanhtung2693\/specgate\/main\/scripts\/install-cli\.sh \| sh/,
  );
  assert.match(skill, /show[\s\S]*installer command[\s\S]*explicit (?:approval|confirmation)/i);
  assert.match(skill, /Windows[\s\S]*WSL2[\s\S]*releases\/latest/i);
  for (const command of [
    "specgate --version",
    "specgate doctor --json",
    "specgate workspace bind",
    "specgate plugins install",
    "specgate plugins doctor",
    "specgate workspace current",
  ]) assert.match(skill, new RegExp(command.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")));

  assert.match(skill, /user.*chooses.*Local.*Full/is);
  assert.match(skill, /user.*chooses.*IDE.*scope/is);
  assert.match(skill, /--workspace-name/);
  assert.match(skill, /--display-name/);
  assert.match(skill, /--username/);
  assert.match(skill, /bind only when.*missing.*incorrect.*explicitly requested/is);
  assert.match(skill, /existing topology.*recovery action/is);
  assert.match(skill, /restart.*IDE/is);
  assert.doesNotMatch(skill, /Produce a project setup map|hard_stop.*required_practice.*local_convention/is);
});

test("user docs keep Local and Full capability boundaries explicit", () => {
  assert.match(docs.featureStatus, /Local and Full quick work item creation/);
  assert.match(docs.featureStatus, /Embedded Codex, Claude Code, and Cursor plugin install in Local mode/);
  assert.match(docs.cliReference, /Full mode only: list, inspect, add, and search[\s\S]*Governance Knowledge/);
  assert.match(docs.cliReference, /`artifact request-changes` in Full mode/);
  assert.match(docs.cliReference, /List and inspect governed features in either mode/);
  assert.match(docs.configReference, /## Full appliance/);
  assert.match(docs.howSpecGateWorks, /Local and Full modes[\s\S]{0,100}quick route/i);
  assert.match(docs.howSpecGateWorks, /artifact-backed route/i);
  assert.doesNotMatch(docs.howSpecGateWorks, /no Local quick-work parity/i);
  assert.match(docs.cliReference, /same in Local and Full mode/i);
  assert.doesNotMatch(files.deliveringWorkSkill, /specgate delivery review/i);
  assert.match(files.deliveringWorkSkill, /checks\[\]\.command[\s\S]{0,240}`sh -c`/i);
  assert.doesNotMatch(files.deliveringWorkSkill, /specgate gates run/);
  for (const text of [docs.quickstart, docs.installIdePlugins, docs.cliReference]) {
    assert.match(text, /removes\s+CLI\s+configuration\s+and\s+globally\s+installed\s+managed\s+plugin\s+files/);
    assert.match(text, /Project-local\s+plugin\s+files[\s\S]{0,120}preserved/i);
    assert.doesNotMatch(text, /removes CLI and plugin setup/);
  }
});

test("Change facade docs describe the actionable post-handoff path without inventing an entity", () => {
  const reference = docs.cliReference.split("## Change facade\n")[1]?.split("\n## ")[0] ?? "";
  const workflow = docs.cliWorkflow.split("## Complete an existing change\n")[1]?.split("\n## ")[0] ?? "";

  assert.notEqual(reference, "", "CLI Reference must have a dedicated Change facade section");
  assert.notEqual(workflow, "", "CLI workflow must have a complete Change recipe");
  for (const syntax of [
    "specgate change status <work-ref>",
    "specgate --yes change approve <artifact-id>",
    "specgate change submit <ref> [--file <completion.json>]",
    "specgate --yes change accept <ref>",
    "specgate change accept <ref>",
    "specgate --yes change request-changes <ref>",
    "specgate change request-changes <ref>",
  ]) assert.match(reference, new RegExp(syntax.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")));
  assert.match(reference, /change approve <artifact-id> --title <title> --ac <criterion>/);
  assert.match(reference, /\.specgate\/completion-<safe-ref>\.json/);
  assert.match(reference, /letters, digits, `-`, and `_`/);
  assert.match(reference, /must pass `--file`/);
  for (const field of ["mode", "ref", "title", "state", "evidence", "assurance", "decision", "receipt", "freshness", "next_actor", "missing", "stale", "stale_reason", "next_command"]) {
    assert.match(reference, new RegExp("`" + field + "`"));
  }
  for (const state of ["implementation", "awaiting_review", "review_pending", "awaiting_acceptance", "accepted", "rework_requested", "blocked"]) {
    assert.match(reference, new RegExp("`" + state + "`"));
  }
  assert.match(reference, /separate summary labels/);
  assert.match(reference, /`evidence_verdict`/);
  assert.match(reference, /current reviewed completion/);
  assert.match(reference, /`change prepare`[\s\S]*not available/i);
  for (const family of ["delivery", "work", "gates", "artifact", "audit", "verify"]) {
    assert.match(reference, new RegExp("`" + family + "`"));
  }

  const initialStatus = workflow.indexOf("specgate change status <work-ref>");
  const scaffold = workflow.indexOf("specgate delivery report <work-ref> --init");
  const submit = workflow.indexOf("specgate change submit <work-ref>");
  const followUpStatus = workflow.indexOf("specgate change status <work-ref>", initialStatus + 1);
  assert.ok(initialStatus >= 0 && initialStatus < scaffold && scaffold < submit && submit < followUpStatus,
    "Change How-to must order status, scaffold, submit, then status again");
  assert.match(workflow, /Run exactly one of the following human decisions/);
  assert.match(workflow, /State: awaiting_review[\s\S]*specgate delivery status <work-ref> --detail/);
  assert.match(workflow, /State: review_pending[\s\S]*specgate delivery review/);
  assert.match(workflow, /State: awaiting_acceptance[\s\S]*specgate --yes change accept <work-ref>/);
  assert.match(workflow, /not ready for acceptance[\s\S]*specgate --yes change request-changes <work-ref>/);
  assert.match(docs.howSpecGateWorks, /does not create a new durable Change entity/i);
  assert.match(docs.featureStatus, /Local and Full quick work item creation/i);
  assert.match(docs.featureStatus, /`change prepare`[\s\S]*not available/i);
});

test("coding-agent delivery ends with a truthful human-acceptance receipt", () => {
  const workflow = docs.codingAgentWorkflow;

  for (const phrase of [
    "SpecGate delivery receipt",
    "`awaiting_acceptance`",
    "Evidence",
    "Assurance",
    "Decision",
    "Receipt",
    "Freshness",
    "Next",
  ]) assert.match(workflow, new RegExp(phrase));
  assert.match(workflow, /fresh\s+`specgate change status <work-ref> --json`/);
  assert.match(workflow, /per-work\s+governance handoff/i);
  assert.match(workflow, /Evidence: Ready for human review/);
  assert.match(workflow, /Assurance: Agent-reported; locally reproduced; second agent affirmed/);
  assert.match(workflow, /Freshness: The stored receipt was not checked against the current checkout\./);
  assert.match(workflow, /stale warning does not rewrite the reported state/i);
  assert.match(workflow, /separate `Stale:` line/);
  assert.match(workflow, /does not prove that SpecGate prevented bugs or saved\s+time/i);
  assert.match(workflow, /not\s+accepted\s+or\s+delivered/i);
  assert.match(workflow, /returned `data\.path`/i);
  assert.doesNotMatch(workflow, /--file \.specgate\/completion-<work-ref>\.json/);
});

test("quickstart completes one Local CLI work item before linking to Full", () => {
  for (const command of [
    "specgate workspace bind",
    "specgate artifact publish",
    "specgate artifact show",
    "specgate gates check",
    "specgate gates results",
    "specgate --yes change approve",
    "specgate work context",
    "specgate delivery report",
    "specgate change submit",
    "specgate change status",
    "specgate --yes change accept",
    "specgate --yes change request-changes",
    "specgate audit",
    "specgate stats",
  ]) {
    assert.match(docs.quickstart, new RegExp(command.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")));
  }
  assert.match(docs.quickstart, /Your IDE agent prepares and\s+publishes/);
  assert.match(docs.quickstart, /You approve the exact artifact version/);
  assert.match(docs.quickstart, /change approve <artifact-id>[\s\S]{0,220}--title[\s\S]{0,220}--ac/);
  assert.match(docs.quickstart, /You make the final delivery decision/);
  assert.doesNotMatch(docs.quickstart, /^## \d+\. Full appliance:/m);
  assert.match(docs.quickstart, /For the Full appliance workflow, see \[Operate SpecGate\]/);
});

test("Local human-decision examples require an explicit human assertion", () => {
  assert.match(docs.cliWorkflow, /specgate --yes change approve/);
  assert.match(docs.codingAgentWorkflow, /specgate --yes change accept/);
  assert.match(docs.respondToGateFailures, /specgate --yes change approve/);
  assert.match(files.preparingWorkSkill, /specgate --yes change approve/);
  assert.doesNotMatch(files.deliveringWorkSkill, /specgate --yes change approve/);
  assert.match(docs.cliReference, /Local mode[\s\S]{0,240}requires explicit `--yes`/);
  assert.match(docs.cliReference, /Full-mode[\s\S]{0,240}`--yes` is optional/);
});

test("delivery skill follows the authoritative SpecGate actor without crossing phases", () => {
  const skill = files.deliveringWorkSkill;
  const words = skill.trim().split(/\s+/).length;
  const firstStatus = skill.indexOf('specgate change status "$WORK_REF" --json');
  const driftDispatch = skill.indexOf('specgate gates tasks dispatch "$ARTIFACT_ID" --json');
  const reworkRouting = skill.indexOf("`rework_requested`");
  const reportSection = skill.indexOf("## 5. Report criterion evidence");
  const reviewPendingRouting = skill.indexOf("`review_pending`");

  assert.match(skill, /^description: Use when .*approved SpecGate work item/m);
  assert.match(skill, /specgate work show "\$WORK_REF" --json[\s\S]*specgate work context "\$WORK_REF" --json/);
  assert.ok(firstStatus > 0 && firstStatus < driftDispatch, "authoritative actor must be checked before drift or implementation");
  assert.ok(reworkRouting > 0 && reworkRouting < reportSection, "rework guidance must be read before evidence is submitted");
  assert.ok(reviewPendingRouting > 0 && reviewPendingRouting < driftDispatch, "review_pending must route before drift or implementation");
  assert.match(skill.slice(reviewPendingRouting, driftDispatch), /next_command[\s\S]*immediately[\s\S]*status/i);
  assert.match(skill, /data\.next_actor/);
  assert.match(skill, /next_command.*verbatim/is);
  assert.match(skill, /awaiting_review.*human reviewer/is);
  assert.match(skill, /peer review.*only when.*human.*explicitly requests/is);
  assert.match(skill, /new artifact version.*specgate-work-preparation/is);
  assert.match(skill, /stop on\s+any mismatch/i);
  assert.match(skill, /existing regular scaffold[\s\S]*reuse/is);
  assert.match(skill, /`data\.path`.*verbatim/is);
  assert.match(skill, /rework_requested[\s\S]*guidance[\s\S]*missing[\s\S]*focused fix[\s\S]*affected checks[\s\S]*submit[\s\S]*fresh status/is);
  assert.doesNotMatch(skill, /specgate --yes change approve|specgate artifact publish|specgate work list|specgate status --json/);
  assert.doesNotMatch(skill, /completion-\$WORK_REF|peer-review-\$WORK_REF|--force/);
  assert.doesNotMatch(skill, /coding_agent\.blocked_ambiguity|coding_agent\.docs_updated/);
  assert.ok(words <= 950, `delivery skill is ${words} words; expected at most 950`);
});

test("delivery examples capture the scaffold path before submitting", () => {
  assert.match(docs.cliWorkflow, /delivery report <work-ref> --init --json[\s\S]*COMPLETION_PATH[\s\S]*change submit <work-ref> --file "\$COMPLETION_PATH"/);
  assert.match(docs.configureModels, /delivery report <work-ref> --init --json[\s\S]*COMPLETION_PATH[\s\S]*change submit <work-ref> --file "\$COMPLETION_PATH"/);
  assert.match(docs.codingAgentWorkflow, /delivery report <work-ref> --init --json[\s\S]*COMPLETION_PATH[\s\S]*change submit <work-ref>[\s\S]*--file "\$COMPLETION_PATH"/);
  assert.match(docs.codingAgentWorkflow, /delivery peer-review <work-ref> --init --json[\s\S]*PEER_REVIEW_PATH[\s\S]*delivery peer-review <work-ref>[\s\S]*--file "\$PEER_REVIEW_PATH"/);
  assert.match(docs.quickstart, /delivery report <work-ref> --init --json[\s\S]*COMPLETION_PATH[\s\S]*change submit <work-ref>[\s\S]*--file "\$COMPLETION_PATH"/);
  assert.doesNotMatch(`${docs.cliWorkflow}\n${docs.configureModels}\n${docs.codingAgentWorkflow}\n${docs.quickstart}\n${docs.cliReference}\n${docs.respondToGateFailures}`, /<returned-data\.path>/);
});

test("update docs distinguish global refresh from project-local refresh", () => {
  assert.match(docs.cliWorkflow, /update[\s\S]{0,220}already-installed global IDE plugin\s+files/i);
  assert.match(docs.cliWorkflow, /project-local[\s\S]{0,180}specgate plugins install --project-local/i);
});

test("Full appliance custom-port setup selects Full mode explicitly", () => {
  assert.match(docs.operateSpecGate, /SPECGATE_PORT=13000 specgate init --mode full/);
  assert.doesNotMatch(docs.operateSpecGate, /SPECGATE_PORT=13000 specgate init\s*(?:\n|$)/);
});

test("work-preparation skill keeps comparison explicit and completes artifact-backed handoff", () => {
  const skill = files.preparingWorkSkill;
  const artifactRoute = skill.slice(skill.indexOf("## 2B."));

  assert.match(skill, /artifact publish --file \.specgate\/work\/artifact\.json[\s\S]{0,80}--preview --compare/);
  assert.match(skill, /artifact show/);
  assert.match(skill, /specgate --yes change approve[\s\S]{0,180}--title[\s\S]{0,180}--ac/);
  assert.match(skill, /work context/);
  assert.match(skill, /does not detect frameworks or infer roles/i);
  assert.doesNotMatch(skill, /auto-detect|detected source kind/i);
  assert.doesNotMatch(skill, /specgate work create --feature/);
  assert.ok(artifactRoute.indexOf("change approve") < artifactRoute.indexOf("work context"));
});

test("work preparation preserves framework sources and separates its two routes", () => {
  const skill = files.preparingWorkSkill;
  const words = skill.trim().split(/\s+/).length;

  assert.match(skill, /^description: Use when preparing .*SpecGate/m);
  assert.match(skill, /quick work[\s\S]*artifact-backed work/i);
  assert.match(skill, /\.specgate\/work\/artifact\.json/);
  assert.match(skill, /`path`.*repository-relative POSIX path/is);
  assert.match(skill, /`source_file`.*contained.*manifest directory/is);
  assert.match(skill, /`file_url`.*outside.*manifest directory/is);
  assert.match(skill, /"feature_key"[\s\S]*"request_type"[\s\S]*"documents"/);
  assert.match(skill, /new_feature.*change_request.*bugfix.*unknown/is);
  assert.match(skill, /publication succeeded[\s\S]*On failure, stop[\s\S]*do not run readiness/is);
  assert.match(skill, /never (?:relocate|move).*copy.*rename.*delete.*commit.*ignore/is);
  assert.match(skill, /every selected source appears exactly once/i);
  assert.match(skill, /explicit human confirmation[\s\S]*artifact publish/is);
  assert.match(skill, /Quick work ends here/i);
  assert.doesNotMatch(skill, /source path.*may differ|Fix each gap in the document that owns it/i);
  assert.ok(words <= 1000, `preparation skill is ${words} words; expected at most 1000`);
});

test("public gateways strip the internal governance settings header", () => {
  for (const gateway of [files.localGateway, files.fullGateway]) {
    assert.match(gateway, /proxy_set_header X-SpecGate-Internal-Agent "";/);
  }
});

test("Node workflows use the current setup-node action", () => {
  const workflows = [files.pagesWorkflow, files.readinessWorkflow, files.uiWorkflow, files.issueLabelerWorkflow];

  for (const workflow of workflows) {
    assert.match(workflow, /actions\/setup-node@v6/);
    assert.doesNotMatch(workflow, /actions\/setup-node@v[1-5]/);
  }
});

test("release UI images build with the supported Node major", () => {
  for (const dockerfile of [files.localDockerfile, files.uiDockerfile]) {
    assert.match(dockerfile, /^FROM node:26-alpine AS /m);
    assert.doesNotMatch(dockerfile, /^FROM node:(?!26-alpine\b)/m);
  }
});

test("tracked Markdown local links resolve", () => {
  const missing = [];

  for (const path of trackedFiles()) {
    if (!path.endsWith(".md") || path.includes("/node_modules/")) continue;
    const markdown = read(path);
    const base = new URL(path, root);

    for (const match of markdown.matchAll(/!?\[[^\]\n]+\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g)) {
      let target = match[1];
      if (
        target.startsWith("#") ||
        target.startsWith("http://") ||
        target.startsWith("https://") ||
        target.startsWith("mailto:") ||
        target.startsWith("app://")
      ) {
        continue;
      }
      if (target.startsWith("<") && target.endsWith(">")) {
        target = target.slice(1, -1);
      }
      const localPath = target.split("#")[0];
      if (!localPath) continue;

      let resolvedTarget = localPath;
      try {
        resolvedTarget = decodeURIComponent(localPath);
      } catch {
        // Keep the raw target; the existence check below reports it.
      }

      const resolved = new URL(resolvedTarget, base);
      if (resolved.protocol === "file:" && !existsSync(resolved)) {
        missing.push(`${path}: ${target}`);
      }
    }
  }

  assert.deepEqual(missing, []);
});
