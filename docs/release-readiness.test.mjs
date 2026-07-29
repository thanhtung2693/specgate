// Repo-wide release gate.
//
// Every check here must be able to fail for a reason other than "someone edited
// this test". Three kinds qualify:
//
//   Derived    — read a fact out of the code (env vars, constants, a file glob)
//                and assert the documentation covers it. These fail when the
//                product changes and the docs do not.
//   Structural — assert a property no single file can restate: links resolve,
//                paths exist, files stay under their size and word budgets,
//                published metadata fits the directory's limits.
//   Policy     — assert that a retired term or an unshipped capability claim is
//                absent. There is no code to derive these from; absence is the
//                only place the rule can live.
//
// An assertion that pins a literal already present in the file under test is
// none of those. `assert.match(releaseWorkflow, /anchore\/scan-action@v7\.4\.0/)`
// only says release.yml says what release.yml says: bumping the action means
// editing two files and catches nothing. Those belong in the file's own review.
//
// CLI command names, `change status` fields, and change states are derived in
// app/cli/internal/command/docs_coverage_test.go and
// app/cli/internal/command/docs_contract_internal_test.go, where the Cobra tree
// and the structs are in memory. Do not re-pin them as literals here.

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, statSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";

const root = new URL("../", import.meta.url);
const read = (path) => readFileSync(new URL(path, root), "utf8");
const exists = (path) => existsSync(new URL(path, root));

const gitFiles = (...args) =>
  execFileSync("git", ["ls-files", ...args, "-z"], { cwd: root })
    .toString("utf8")
    .split("\0")
    .filter((path) => path && exists(path));

const trackedFiles = () => gitFiles();
const workingFiles = () => gitFiles("--cached", "--others", "--exclude-standard");
const markdownFiles = () =>
  trackedFiles().filter((path) => path.endsWith(".md") && !path.includes("/node_modules/"));

const uniqueMatches = (text, pattern) => [...new Set([...text.matchAll(pattern)].map(([, value]) => value))];
const wordCount = (text) => text.trim().split(/\s+/).length;

// ---------------------------------------------------------------------------
// Derived: the code is the source of truth and the docs must keep up.
// ---------------------------------------------------------------------------

test("every doc-registry config env var reaches the operator docs", () => {
  const operatorDocs = [
    "app/doc-registry/.env.example",
    "deploy/README.md",
    "app/doc-registry/docs/README.md",
    "app/doc-registry/docs/spec.md",
  ]
    .map(read)
    .join("\n");
  const configEnvVars = uniqueMatches(
    read("app/doc-registry/internal/config/config.go"),
    /\bgetEnv(?:Bool|Int|Int64|Float)?\("([A-Z0-9_]+)"/g,
  );

  assert.ok(configEnvVars.length > 0, "no doc-registry config env vars found; the accessor pattern changed");
  assert.deepEqual(
    configEnvVars.filter((name) => !new RegExp(`\\b${name}\\b`).test(operatorDocs)),
    [],
  );
});

test("every workboard warning code reaches the shared contract and the registry docs", () => {
  const contracts = read("docs/contributing/contracts.md");
  const registryContract = `${read("app/doc-registry/docs/spec.md")}\n${read("app/doc-registry/docs/api.md")}`;
  const warningCodes = uniqueMatches(
    read("app/doc-registry/internal/workboard/model.go"),
    /\bWarning[A-Za-z0-9_]+\s+WarningCode\s*=\s*"([^"]+)"/g,
  );

  assert.ok(warningCodes.length > 0, "no WarningCode constants found; the declaration pattern changed");
  assert.deepEqual(
    warningCodes.filter(
      (code) => !contracts.includes(`\`${code}\``) || !registryContract.includes(`\`${code}\``),
    ),
    [],
  );
});

test("every CLI exit code is documented as stable API", () => {
  const reference = read("docs/using-specgate/reference/cli.md");
  const codes = uniqueMatches(read("app/cli/internal/output/output.go"), /\bExit[A-Za-z]+\s+=\s+(\d)\b/g);

  assert.ok(codes.length > 0, "no exit-code constants found; the declaration pattern changed");
  assert.deepEqual(
    codes.filter((code) => !reference.includes(`| \`${code}\` |`)),
    [],
  );
});

// Paths named in prose precisely because they must NOT exist.
const deliberatelyAbsentPaths = new Set(["plugins/specgate/"]);

test("every artifact manifest source kind reaches the user docs and the skill", () => {
  // #33 added `repo_file` as a fourth source kind. Nothing derived checked that a
  // new manifest field reaches the people who author manifests, and the field
  // vocabulary is what an IDE agent has to get exactly right.
  const kinds = uniqueMatches(
    read("app/cli/internal/command/artifact_sources.go"),
    /"(content|repo_file|source_file|file_url)"/g,
  );
  assert.ok(kinds.length >= 3, "no manifest source kinds found; the declaration pattern changed");

  const authoringDocs = [
    "plugins/skills/specgate-work-preparation/SKILL.md",
    "docs/using-specgate/concepts/artifacts-and-context-packs.md",
  ].map(read);

  const undocumented = kinds.filter((kind) => !authoringDocs.some((text) => text.includes(`\`${kind}\``)));
  assert.deepEqual(undocumented, []);
});

test("every gate key the CLI emits is in the gate catalog", () => {
  // The catalog documented the policy field name `required_roles` while the gate
  // users actually see is `required_roles_present`, and `has_documents` was
  // missing entirely. A gate an author cannot look up is a gate they cannot fix.
  const catalog = read("docs/using-specgate/reference/gates.md");
  const emitted = uniqueMatches(
    read("app/cli/internal/local/gate_tasks.go"),
    /"gate":\s*"([a-z_]+)"|"([a-z_]+)":\s*requiredRoleCheck/g,
  ).filter(Boolean);
  const gateKeys = new Set([
    ...emitted,
    ...uniqueMatches(read("app/cli/internal/local/gate_tasks.go"), /"([a-z_]+)":\s*map\[string\]any\{"gate"/g),
  ]);
  assert.ok(gateKeys.size > 0, "no gate keys found; the declaration pattern changed");

  const undocumented = [...gateKeys].filter((key) => !catalog.includes(`\`${key}\``));
  assert.deepEqual(undocumented, []);
});

test("every backticked repository path in tracked Markdown resolves", () => {
  // Prefixes that are unambiguously repository-root-anchored. A bare `docs/...`
  // is excluded because every module has its own `docs/`, so `docs/spec.md` in
  // app/agents names a different file than the same string in the root rules.
  // Module-relative paths still resolve against the file's own directory below,
  // and `<placeholder>` segments never match the character class.
  const repoPath = /`((?:app|deploy|docker|plugins|scripts|media|\.github)\/[A-Za-z0-9._\-/]+)`/g;
  const missing = [];

  for (const path of markdownFiles()) {
    const dir = new URL(path, root);
    for (const target of uniqueMatches(read(path), repoPath)) {
      if (deliberatelyAbsentPaths.has(target)) continue;
      if (existsSync(new URL(target, dir)) || exists(target)) continue;
      missing.push(`${path}: ${target}`);
    }
  }

  assert.deepEqual(missing, []);
});

test("tracked Markdown local links resolve", () => {
  const missing = [];

  for (const path of markdownFiles()) {
    const base = new URL(path, root);

    for (const match of read(path).matchAll(/!?\[[^\]\n]+\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g)) {
      let target = match[1];
      if (/^(?:#|https?:|mailto:|app:)/.test(target)) continue;
      if (target.startsWith("<") && target.endsWith(">")) target = target.slice(1, -1);

      const localPath = target.split("#")[0];
      if (!localPath) continue;

      let decoded = localPath;
      try {
        decoded = decodeURIComponent(localPath);
      } catch {
        // Keep the raw target; the existence check below reports it.
      }

      const resolved = new URL(decoded, base);
      if (resolved.protocol === "file:" && !existsSync(resolved)) {
        missing.push(`${path}: ${target}`);
      }
    }
  }

  assert.deepEqual(missing, []);
});

test("every GitHub workflow uses supported action majors", () => {
  const workflows = trackedFiles().filter((path) => /^\.github\/workflows\/.+\.ya?ml$/.test(path));
  assert.ok(workflows.length > 0, "no workflows found");

  const stale = [];
  for (const path of workflows) {
    const text = read(path);
    for (const [action, minimum] of [
      ["actions/setup-node", 6],
      ["actions/checkout", 6],
      ["actions/upload-artifact", 7],
      ["actions/download-artifact", 7],
    ]) {
      for (const version of uniqueMatches(text, new RegExp(`${action}@v(\\d+)`, "g"))) {
        if (Number(version) < minimum) stale.push(`${path}: ${action}@v${version} < v${minimum}`);
      }
    }
  }

  assert.deepEqual(stale, []);
});

test("every image that builds the UI uses the supported Node major", () => {
  const dockerfiles = trackedFiles().filter((path) => /(?:^|\/)Dockerfile[.\w-]*$/.test(path));
  assert.ok(dockerfiles.length > 0, "no Dockerfiles found");

  const stale = [];
  for (const path of dockerfiles) {
    for (const major of uniqueMatches(read(path), /^FROM node:(\d+)/gm)) {
      if (Number(major) !== 26) stale.push(`${path}: node:${major}`);
    }
  }

  assert.deepEqual(stale, []);
});

// Every word in a skill is paid on each IDE agent session. Raise a budget only
// for a capability the agent cannot otherwise discover.
const skillBudgets = {
  "plugins/skills/specgate/SKILL.md": 550,
  "plugins/skills/specgate-project-setup/SKILL.md": 750,
  "plugins/skills/specgate-work-preparation/SKILL.md": 1000,
  "plugins/skills/specgate-work-delivery/SKILL.md": 1000,
};

test("every installed skill stays inside a declared word budget", () => {
  const skills = trackedFiles()
    .filter((path) => /^plugins\/skills\/[^/]+\/SKILL\.md$/.test(path))
    .sort();
  assert.deepEqual(skills, Object.keys(skillBudgets).sort(), "a skill has no declared word budget");

  assert.deepEqual(
    skills
      .map((path) => ({ path, words: wordCount(read(path)), budget: skillBudgets[path] }))
      .filter(({ words, budget }) => words > budget),
    [],
  );
});

test("the router names exactly the phase skills that exist", () => {
  // A router edit can silently advertise a phase that was renamed or drop one
  // that still ships. Either way the agent loses a phase it is told to select
  // from, and nothing else in the suite would notice.
  const router = read("plugins/skills/specgate/SKILL.md");
  const named = new Set(uniqueMatches(router, /`(specgate-[a-z-]+)`/g));
  const onDisk = new Set(
    trackedFiles()
      .filter((path) => /^plugins\/skills\/[^/]+\/SKILL\.md$/.test(path))
      .map((path) => path.split("/")[2])
      .filter((name) => name !== "specgate"),
  );

  assert.ok(onDisk.size > 0, "no phase skills found");
  assert.deepEqual([...named].filter((name) => !onDisk.has(name)).sort(), [], "router names a skill that does not exist");
  assert.deepEqual([...onDisk].filter((name) => !named.has(name)).sort(), [], "a shipped skill the router never names");
});

test("every installed skill declares when it applies", () => {
  assert.deepEqual(
    trackedFiles()
      .filter((path) => /^plugins\/skills\/[^/]+\/SKILL\.md$/.test(path))
      .filter((path) => !/^description: Use when /m.test(read(path))),
    [],
  );
});

test("every skill shipped to users is byte-identical across its embedded copies", () => {
  const canonical = trackedFiles().filter((path) => path.startsWith("plugins/skills/"));
  assert.ok(canonical.length > 0, "no canonical plugin skills found");

  const drift = [];
  for (const source of canonical) {
    for (const prefix of [
      "app/doc-registry/internal/agentpackages/plugins/",
      "app/cli/internal/command/local_plugin_assets/",
    ]) {
      const copy = source.replace("plugins/", prefix);
      if (!exists(copy)) drift.push(`${copy} is missing; run make sync-plugins`);
      else if (read(copy) !== read(source)) drift.push(`${copy} differs from ${source}; run make sync-plugins`);
    }
  }

  assert.deepEqual(drift, []);
});

test("handwritten production modules stay below 800 lines", () => {
  const oversized = workingFiles()
    .filter((path) => /\.(?:go|py|[cm]?[jt]sx?|sh)$/.test(path))
    .filter((path) => !/(?:^|\/)(?:tests?|fixtures|generated|node_modules|dist)(?:\/|$)/.test(path))
    .filter((path) => !/(?:_test\.go|\.test\.[cm]?[jt]sx?|\.spec\.[cm]?[jt]sx?|\.d\.ts)$/.test(path))
    .map((path) => ({ path, lines: read(path).split("\n").length - 1 }))
    .filter(({ lines }) => lines >= 800);

  assert.deepEqual(oversized, []);
});

test("handwritten test modules stay below 1000 lines", () => {
  const oversized = workingFiles()
    .filter((path) => /\.(?:go|py|[cm]?[jt]sx?)$/.test(path))
    .filter((path) => /(?:^|\/)tests?(?:\/|$)|(?:_test\.go|\.test\.[cm]?[jt]sx?|\.spec\.[cm]?[jt]sx?)$/.test(path))
    .filter((path) => !/(?:^|\/)(?:fixtures|generated|node_modules|dist)(?:\/|$)/.test(path))
    .map((path) => ({ path, lines: read(path).split("\n").length - 1 }))
    .filter(({ lines }) => lines >= 1000);

  assert.deepEqual(oversized, []);
});

test("every plugin manifest publishes the same version", () => {
  const manifests = {
    "plugins/package.json": (json) => json.version,
    "plugins/.cursor-plugin/plugin.json": (json) => json.version,
    ".claude-plugin/marketplace.json": (json) => json.plugins?.[0]?.version,
  };
  const versions = Object.entries(manifests).map(([path, pick]) => `${path}=${pick(JSON.parse(read(path)))}`);

  assert.equal(
    new Set(versions.map((entry) => entry.split("=")[1])).size,
    1,
    `plugin manifests disagree: ${versions.join(", ")}`,
  );
});

test("Codex plugin metadata stays within public-directory limits", () => {
  const plugin = JSON.parse(read("plugins/package.json"));
  const prompts = plugin.starter_prompts ?? [];

  assert.match(plugin.name, /^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/);
  assert.match(plugin.version, /^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/);
  assert.ok(plugin.display_name.length <= 30, "plugin display name exceeds 30 characters");
  assert.ok(plugin.short_description.length <= 30, "plugin short description exceeds 30 characters");
  assert.ok(plugin.long_description.length <= 4000, "plugin long description exceeds 4,000 characters");
  assert.ok(plugin.developer_name.length <= 80, "plugin developer name exceeds 80 characters");
  assert.ok(prompts.length <= 3, "plugin has more than three starter prompts");
  assert.equal(new Set(prompts.map((prompt) => prompt.trim().normalize())).size, prompts.length);
  for (const prompt of prompts) {
    assert.ok(prompt.length <= 128, `starter prompt exceeds 128 characters: ${prompt}`);
    assert.doesNotMatch(prompt, /@\w/, `starter prompt must not @-mention: ${prompt}`);
  }
});

// ---------------------------------------------------------------------------
// Policy: names and claims that must not reappear anywhere in the repository.
// A capability that does not exist has no code to derive from, so absence is
// the only encoding.
// ---------------------------------------------------------------------------

// `allow` lists the files that may name a retired thing: a test proving it is
// gone, or an ADR recording the decision that retired it. Both are the record
// of the retirement, not a live claim.
const retired = [
  {
    pattern: /plugins\/install\.sh/,
    why: "the SpecGate CLI owns plugin installation; the standalone installer was removed",
    allow: ["app/doc-registry/internal/api/handlers_agent_packages_test.go"],
  },
  {
    pattern: /specgate-router/,
    why: "the entry skill is named `specgate`",
    allow: ["app/cli/internal/command/plugins_internal_test.go"],
  },
  { pattern: /hooks-cursor\.json/, why: "the Cursor bootstrap hook was removed" },
  { pattern: /agent-handoff\.md/, why: "the handoff doc was retired" },
  { pattern: /ADMIN_SECRET/, why: "no admin secret ever shipped; the appliance has no HTTP auth layer" },
  { pattern: /docker-compose\.dev\.yml/, why: "the release is a single local appliance" },
  { pattern: /multi-service Compose/i, why: "the release is a single local appliance" },
  {
    pattern: /\broute (?:suggestions|advice)\b/i,
    why: "prose route classification was removed from the governance model surface",
  },
  { pattern: /public Context Pack URL/i, why: "Context Packs are never published to an unauthenticated URL" },
  { pattern: /unstable_threadListAdapter/, why: "governance chat has no thread list; threads are ephemeral" },
  { pattern: /Offline Local CLI/, why: "Local mode is not positioned as an offline product" },
  { pattern: /<returned-data\.path>/, why: "examples capture the returned path in a shell variable" },
  {
    // Provisional self-description stopped people trying the product. The
    // deployment boundary in SECURITY.md and trust-and-security.md is a
    // different thing and stays.
    pattern: /\b(?:experimental|early release|alpha release)\b/i,
    why: "SpecGate does not describe itself as provisional; state the capability and its boundary instead",
    allow: ["docs/contributing/adr/", "CHANGELOG.md", "app/ui/src/App.integrations.test.tsx"],
  },
  {
    pattern: /\b(?:CI ingestion|tracker authority)\b/i,
    why: "provider CI ingestion and tracker authority are not in the graduated integration contract",
    allow: ["docs/contributing/adr/"],
  },
];

test("retired terminology and unshipped capability claims are absent repo-wide", () => {
  // Text the repository actually ships or renders. Generated plugin copies are
  // included on purpose: a retired term that survives `make sync-plugins` still
  // reaches users.
  const scannable = trackedFiles().filter(
    (path) =>
      /\.(?:md|mdc|mjs|cjs|jsx?|tsx?|go|py|sh|ya?ml|json|toml|conf|sql|migration|html|css|example)$/.test(path) &&
      !path.includes("/node_modules/") &&
      path !== "docs/release-readiness.test.mjs",
  );
  assert.ok(scannable.length > 0, "no scannable files found");

  const found = [];
  for (const path of scannable) {
    const text = read(path);
    for (const { pattern, why, allow = [] } of retired) {
      if (allow.some((prefix) => path.startsWith(prefix))) continue;
      const match = text.match(pattern);
      if (match) found.push(`${path}: ${JSON.stringify(match[0])} — ${why}`);
    }
  }

  assert.deepEqual(found, []);
});

// Each lifecycle skill owns one phase. A command that belongs to another phase
// is not a typo the agent recovers from — it silently crosses a governance
// boundary, so the skill that must not name it is the only place to say so.
// There is no code to derive this from: every command below exists and is
// correct somewhere else.
const skillPhaseBoundaries = [
  {
    skill: "plugins/skills/specgate-work-delivery/SKILL.md",
    forbidden: [
      { pattern: /specgate delivery review/i, why: "`change submit` runs the review; a separate call double-runs it" },
      { pattern: /specgate gates run/, why: "delivery uses artifact gate tasks, not work-item model gates" },
      { pattern: /specgate --yes change approve/, why: "approval is the preparation phase's human decision" },
      { pattern: /specgate artifact publish/, why: "publishing a new version belongs to specgate-work-preparation" },
      { pattern: /specgate work list/, why: "the delivery phase already has its work ref" },
      { pattern: /specgate status --json/, why: "delivery reads change status for one ref, not the board" },
      { pattern: /completion-\$WORK_REF|peer-review-\$WORK_REF/, why: "the scaffold path comes from `data.path`" },
      { pattern: /--force/, why: "no delivery command takes --force; suggesting it invites a wrong retry" },
    ],
  },
  {
    skill: "plugins/skills/specgate-work-preparation/SKILL.md",
    forbidden: [
      { pattern: /specgate work create --feature/, why: "the flag does not exist; `work create` resolves the feature" },
      { pattern: /auto-detect|detected source kind/i, why: "SpecGate does not infer frameworks or source kinds" },
    ],
  },
];

test("each lifecycle skill stays inside its phase", () => {
  const crossings = [];

  for (const { skill, forbidden } of skillPhaseBoundaries) {
    const text = read(skill);
    for (const { pattern, why } of forbidden) {
      const match = text.match(pattern);
      if (match) crossings.push(`${skill}: ${JSON.stringify(match[0])} — ${why}`);
    }
  }

  assert.deepEqual(crossings, []);
});

test("the delivery skill reads the authoritative actor before it acts", () => {
  const skill = read("plugins/skills/specgate-work-delivery/SKILL.md");
  const at = (needle) => {
    const index = skill.indexOf(needle);
    assert.notEqual(index, -1, `delivery skill no longer contains ${JSON.stringify(needle)}`);
    return index;
  };

  const status = at('specgate change status "$WORK_REF" --json');
  const reviewPending = at("`review_pending`");
  const rework = at("`rework_requested`");
  const dispatch = at('specgate gates tasks dispatch "$ARTIFACT_ID" --json');
  const report = at("## 5. Report criterion evidence");

  assert.ok(status < dispatch, "the authoritative actor must be read before drift or implementation");
  assert.ok(reviewPending < dispatch, "a review already pending must route before more work starts");
  assert.ok(rework < report, "rework guidance must be read before evidence is submitted");
});

// In Local mode a human decision has no second party to confirm it, so the CLI
// requires an explicit `--yes`. An example without it teaches a command that
// fails, and the agent learns to add `--yes` by trial instead of by contract.
test("documented Local human decisions carry the explicit assertion flag", () => {
  const bare = [];

  for (const path of [
    "docs/using-specgate/guides/cli-workflow.md",
    "docs/using-specgate/guides/coding-agent-workflow.md",
    "docs/using-specgate/guides/respond-to-gate-failures.md",
    "docs/using-specgate/quickstart.md",
    "plugins/skills/specgate-work-preparation/SKILL.md",
  ]) {
    let full = false;
    for (const line of read(path).split("\n")) {
      // A `# Full` comment opens an example that is correct without --yes,
      // because Full mode confirms interactively.
      if (/^\s*#\s*(Local|Full)\b/.test(line)) full = /Full/.test(line);
      const decision = line.match(/specgate (?:--yes )?change (approve|accept|request-changes)\b/);
      if (decision && !full && !line.includes("specgate --yes change")) bare.push(`${path}: ${decision[0]}`);
    }
  }

  assert.deepEqual(bare, []);
});

test("files retired with their features stay deleted", () => {
  assert.deepEqual(
    [
      "deploy/compose/compose.yml",
      "docker-compose.dev.yml",
      "app/doc-registry/docker-compose.yml",
      "plugins/hooks/hooks-cursor.json",
      "plugins/skills/specgate-router/SKILL.md",
    ].filter(exists),
    [],
  );
});

test("the installer never trades away download verification or rate-limited discovery", () => {
  const installer = read("scripts/install-cli.sh");

  assert.doesNotMatch(installer, /api\.github\.com/, "the unauthenticated API is rate-limited behind shared NAT");
  assert.doesNotMatch(installer, /skipping verification/i, "a failed checksum must stop the install, not warn");
  assert.match(installer, /Cannot verify the download/, "the installer must say why it stopped");
});

test("both gateways strip the internal header and accept the documented limits", () => {
  for (const gateway of ["docker/local/nginx.conf", "app/ui/docker/nginx-default.conf"]) {
    const text = read(gateway);
    assert.match(text, /proxy_set_header X-SpecGate-Internal-Agent "";/, `${gateway} leaks the internal header`);
    assert.match(text, /client_max_body_size 32m;/);
    assert.match(text, /location = \/integrations\/oauth-callback/);
    assert.match(
      text,
      /location ~ \^\/integrations\/\[\^\/\]\+\/resources\/\[\^\/\]\+\/\(github\|gitlab\|linear\)\/webhook\$/,
    );
  }
});

// ---------------------------------------------------------------------------
// Positioning and release automation: the public claims a release may make, and
// the workflow invariants that a doc alone cannot express.
// ---------------------------------------------------------------------------

test("public docs lead with the deployment boundary, not with hedging", () => {
  // The boundary is a safety fact and has to stay stated. Provisional framing —
  // "early release", "experimental", "alpha" — is separate: it deterred people
  // from trying the product and is enforced absent by the retired-terminology
  // scan below.
  const readme = read("README.md");
  assert.match(readme, /trusted machine or private network/i);
  assert.match(readme, /no HTTP[\s>]+authentication layer/i);
  assert.match(readme, /pre-1\.0/);

  const featureStatus = read("docs/using-specgate/reference/feature-status.md");
  assert.match(featureStatus, /## Established paths/);
  assert.match(featureStatus, /## Newer surfaces/);
});

test("the repository ignores generated per-project agent rule files", () => {
  const ignore = read(".gitignore");
  assert.match(ignore, /^CLAUDE\.specgate\.md$/m);
  assert.match(ignore, /^AGENTS\.specgate\.md$/m);
});

test("release automation rejects an already-published GitHub Release before verifying", () => {
  const workflow = read(".github/workflows/release.yml");
  const verifyJob = workflow.slice(workflow.indexOf("\n  verify:\n"), workflow.indexOf("\n  release-cli:\n"));
  const preflight = verifyJob.indexOf("Reject pre-published GitHub Release");
  const checkout = verifyJob.indexOf("actions/checkout@");

  assert.notEqual(verifyJob, "", "release workflow must define a verify job before release-cli");
  assert.notEqual(preflight, -1, "tag releases need a published-release preflight");
  assert.ok(preflight < checkout, "release-state validation must run before expensive verification");
});

test("release verification covers every module a release ships", () => {
  const verifyJob = read(".github/workflows/release.yml");

  for (const command of [
    "make test",
    "uv run pytest -q",
    "npm run test -- --run",
    "npm run build",
    "node --test docs/release-readiness.test.mjs",
  ]) {
    assert.ok(verifyJob.includes(command), `release verification does not run ${command}`);
  }
});

test("release workflow builds only the single local appliance, with provenance and a blocking scan", () => {
  const workflow = read(".github/workflows/release.yml");

  assert.match(workflow, /--file docker\/Dockerfile\.local/);
  assert.doesNotMatch(workflow, /dockerfile: docker\/Dockerfile\.(?:doc-registry|agents|ui)/);
  assert.match(workflow, /--provenance=mode=max/);
  assert.match(workflow, /--sbom=true/);
  assert.match(workflow, /severity-cutoff: high/);
  assert.match(workflow, /only-fixed: true/);
});

test("the release guide matches the automation it drives", () => {
  const guide = read("docs/contributing/release.md");

  assert.match(guide, /git push origin "\$VERSION"/, "the tag push is what triggers the release");
  assert.match(guide, /Do not create or publish a GitHub Release/i, "a pre-published release fails the preflight");
  assert.match(
    guide,
    /downloaded appliance image stays in Docker's cache/i,
    "purge guidance must not claim the image is removed",
  );
});

test("Pages validates landing changes without redeploying unrelated main pushes", () => {
  const workflow = read(".github/workflows/pages.yml");

  assert.match(workflow, /push:\s*\n\s+branches: \["main"\]\s*\n\s+paths:/);
  assert.match(workflow, /pull_request:\s*\n\s+paths:/);
  assert.match(
    workflow,
    /deploy:\s*\n\s+if: github\.event_name != 'pull_request'[\s\S]*?concurrency:\s*\n\s+group: pages/,
  );
});

test("the local appliance deployment directory is the only Compose entry point", () => {
  const makefile = read("Makefile");

  assert.match(makefile, /LOCAL_DEPLOY_DIR := deploy\/local/);
  assert.match(makefile, /-f \$\(LOCAL_DEPLOY_DIR\)\/compose\.yml/);
  assert.ok(statSync(new URL("deploy/local/compose.yml", root)).isFile());
});
