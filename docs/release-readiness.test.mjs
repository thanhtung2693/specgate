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
  preparingWorkSkill: read("plugins/skills/specgate-work-preparation/SKILL.md"),
  deliveringWorkSkill: read("plugins/skills/specgate-work-delivery/SKILL.md"),
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

  for (const skill of [
    "specgate-router",
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

test("user docs keep Local and Full capability boundaries explicit", () => {
  assert.match(docs.featureStatus, /Local and Full quick work item creation/);
  assert.match(docs.featureStatus, /Embedded Codex, Claude Code, and Cursor plugin install in Local mode/);
  assert.match(docs.cliReference, /Full mode only: list, inspect, add, and search[\s\S]*Governance Knowledge/);
  assert.match(docs.cliReference, /`artifact request-changes` in Full mode/);
  assert.match(docs.cliReference, /List and inspect governed features in either mode/);
  assert.match(docs.configReference, /## Full appliance/);
  assert.match(files.deliveringWorkSkill, /Delivery review remains available for\s+diagnosis in both modes/);
  assert.match(files.deliveringWorkSkill, /In Full mode[\s\S]{0,120}specgate gates run/);
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
  assert.match(files.deliveringWorkSkill, /specgate --yes change approve/);
  assert.match(docs.cliReference, /Local mode[\s\S]{0,240}requires explicit `--yes`/);
  assert.match(docs.cliReference, /Full-mode[\s\S]{0,240}`--yes` is optional/);
});

test("Full appliance custom-port setup selects Full mode explicitly", () => {
  assert.match(docs.operateSpecGate, /SPECGATE_PORT=13000 specgate init --mode full/);
  assert.doesNotMatch(docs.operateSpecGate, /SPECGATE_PORT=13000 specgate init\s*(?:\n|$)/);
});

test("work-preparation skill keeps comparison explicit and completes full-route handoff", () => {
  const skill = files.preparingWorkSkill;

  assert.match(skill, /artifact publish --file artifact\.json --preview --compare/);
  assert.match(skill, /artifact show/);
  assert.match(skill, /specgate --yes change approve[\s\S]{0,180}--title[\s\S]{0,180}--ac/);
  assert.match(skill, /work context/);
  assert.match(skill, /does not detect frameworks or infer roles/i);
  assert.doesNotMatch(skill, /auto-detect|detected source kind/i);
  assert.doesNotMatch(skill, /specgate work create --feature/);
  assert.ok(skill.indexOf("change approve") < skill.indexOf("work context"));
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
