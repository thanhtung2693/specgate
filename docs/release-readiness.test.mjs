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
import { existsSync, readFileSync } from "node:fs";
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

test("Local distribution has no server owners and keeps plugin tooling at the root", () => {
  const roots = ["app/agents/", "app/doc-registry/", "app/ui/", "deploy/", "docker/"];
  const retired = workingFiles().filter((path) => roots.some((root) => path.startsWith(root)) && exists(path));
  assert.equal(retired.length, 0, `retired source still ships: ${retired.slice(0, 5).join(", ")}`);
  for (const path of ["scripts/generate-plugin-metadata.py", "scripts/sync-embedded-plugins.sh", "app/cli/internal/command/local_ui_assets/index.html"]) {
    assert.ok(exists(path), path);
  }
  assert.doesNotMatch(read("Makefile"), /\bdocker compose\b|app\/doc-registry/);
  assert.doesNotMatch(read(".github/workflows/release.yml"), /\bghcr\.io\b|specgate-compose|docker\/setup-buildx/);
});
const markdownFiles = () =>
  workingFiles().filter((path) => path.endsWith(".md") && !path.includes("/node_modules/"));

const uniqueMatches = (text, pattern) => [...new Set([...text.matchAll(pattern)].map(([, value]) => value))];
const wordCount = (text) => text.trim().split(/\s+/).length;

test("Local delivery-assurance vocabulary distinguishes binding and observation", () => {
  const contracts = read("docs/contributing/contracts.md");
  const vocabulary = uniqueMatches(
    contracts,
    /`(observed by the SpecGate CLI|reported by the coding agent, not re-run|bound deterministic check)`/g,
  );
  assert.equal(vocabulary.length, 3);
  const readback = ["assurance.go", "delivery_status.go"].map(name => read("app/cli/internal/command/" + name)).join("\n");
  for (const phrase of vocabulary.filter(value => value !== "bound deterministic check")) {
    assert.ok(readback.includes(phrase), phrase);
  }
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

const skillBudgets = {
  "plugins/skills/specgate/SKILL.md": 550,
  "plugins/skills/specgate-project-setup/SKILL.md": 750,
  "plugins/skills/specgate-work-preparation/SKILL.md": 1000,
  "plugins/skills/specgate-work-delivery/SKILL.md": 1000,
  "plugins/skills/specgate-human-handoff/SKILL.md": 350,
};

test("every installed skill stays inside a declared word budget", () => {
  const skills = workingFiles()
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

test("the router names exactly the companion and phase skills that exist", () => {
  // A router edit can silently advertise a phase that was renamed or drop one
  // that still ships. Either way the agent loses a phase it is told to select
  // from, and nothing else in the suite would notice.
  const router = read("plugins/skills/specgate/SKILL.md");
  const named = new Set(uniqueMatches(router, /`(specgate-[a-z-]+)`/g));
  const onDisk = new Set(
    workingFiles()
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
    workingFiles()
      .filter((path) => /^plugins\/skills\/[^/]+\/SKILL\.md$/.test(path))
      .filter((path) => !/^description: Use when /m.test(read(path))),
    [],
  );
});

test("every skill shipped to users is byte-identical across its embedded copies", () => {
  const canonical = workingFiles().filter((path) => path.startsWith("plugins/skills/"));
  assert.ok(canonical.length > 0, "no canonical plugin skills found");

  const drift = [];
  for (const source of canonical) {
    const copy = source.replace("plugins/", "app/cli/internal/command/local_plugin_assets/");
    if (!exists(copy)) drift.push(`${copy} is missing; run make sync-plugins`);
    else if (read(copy) !== read(source)) drift.push(`${copy} differs from ${source}; run make sync-plugins`);
  }

  assert.deepEqual(drift, []);
});

const handwrittenTestFile = /(?:^|\/)tests?(?:\/|$)|(?:_test\.go|[._]test\.[cm]?[jt]sx?|\.spec\.[cm]?[jt]sx?)$/;

test("handwritten production modules stay below 800 lines", () => {
  const oversized = workingFiles()
    .filter((path) => /\.(?:go|py|[cm]?[jt]sx?|sh)$/.test(path))
    .filter((path) => !/(?:^|\/)(?:tests?|fixtures|generated|node_modules|dist)(?:\/|$)/.test(path))
    .filter((path) => !handwrittenTestFile.test(path) && !path.endsWith(".d.ts"))
    .map((path) => ({ path, lines: read(path).split("\n").length - 1 }))
    .filter(({ lines }) => lines >= 800);

  assert.deepEqual(oversized, []);
});

test("handwritten test modules stay below 1000 lines", () => {
  const oversized = workingFiles()
    .filter((path) => /\.(?:go|py|[cm]?[jt]sx?)$/.test(path))
    .filter((path) => handwrittenTestFile.test(path))
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

  },
  {
    pattern: /specgate-router/,
    why: "the entry skill is named `specgate`",
    allow: ["app/cli/internal/command/plugins_internal_test.go"],
  },
  { pattern: /hooks-cursor\.json/, why: "the Cursor bootstrap hook was removed" },
  { pattern: /agent-handoff\.md/, why: "the handoff doc was retired" },
  { pattern: /ADMIN_SECRET/, why: "no admin secret shipped" },
  { pattern: /docker-compose\.dev\.yml/, why: "deployment packaging is retired" },
  { pattern: /multi-service Compose/i, why: "deployment packaging is retired" },
  {
    pattern: /\broute (?:suggestions|advice)\b/i,
    why: "prose route classification was removed from the governance model surface",
  },
  { pattern: /public Context Pack URL/i, why: "Context Packs are never published to an unauthenticated URL" },
  { pattern: /unstable_threadListAdapter/, why: "governance chat has no thread list; threads are ephemeral" },
  { pattern: /Offline Local CLI/, why: "Local mode is not positioned as an offline product" },
  { pattern: /<returned-data\.path>/, why: "examples capture the returned path in a shell variable" },
  {
    // Product messaging states capabilities and boundaries directly. The
    // deployment boundary in SECURITY.md and trust-and-security.md remains.
    pattern: /\b(?:experimental|early release|alpha release)\b/i,
    why: "SpecGate does not describe itself as provisional; state the capability and its boundary instead",

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

test("preparation teaches conditional preservation criteria without inventing a new workflow", () => {
  const skill = read("plugins/skills/specgate-work-preparation/SKILL.md");
  assert.match(skill, /when the change touches existing behavior/i);
  assert.match(skill, /must preserve/i);
  assert.match(skill, /existing tests/i);
  assert.match(skill, /rerun.*after/i);
  assert.match(skill, /ordinary acceptance criteri/i);
});

test("the delivery skill reads the authoritative actor before it acts", () => {
  const skill = read("plugins/skills/specgate-work-delivery/SKILL.md");
  const at = (needle) => {
    const index = skill.indexOf(needle);
    assert.notEqual(index, -1, `delivery skill no longer contains ${JSON.stringify(needle)}`);
    return index;
  };

  const status = at('specgate work resume "$WORK_REF" --json');
  assert.ok(at("Use `data.status`") > status);
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
test("Local human handoff gives the CLI next command before optional browser review", () => {
  const handoff = read("plugins/skills/specgate-human-handoff/SKILL.md").replace(/\s+/g, " ");
  const cli = handoff.indexOf("show `next_command` verbatim");
  const local = handoff.indexOf("In Local mode, look up a review link only when the user has chosen browser review");
  assert.ok(cli >= 0 && local > cli, "the authoritative command must precede optional Local review");
  assert.ok(handoff.indexOf("specgate open <work-ref> --print --json") > local);
  assert.match(handoff, /prefer the CLI or have expressed no preference, use the CLI handoff and skip the lookup/);
  assert.match(handoff, /Start the UI only when the user asks to start it/);
});

test("documented Local human decisions carry the explicit assertion flag", () => {
  const bare = [];

  for (const path of [
    "docs/using-specgate/guides/cli-workflow.md",
    "docs/using-specgate/guides/coding-agent-workflow.md",
    "docs/using-specgate/guides/respond-to-gate-failures.md",
    "docs/using-specgate/quickstart.md",
    "plugins/skills/specgate-work-preparation/SKILL.md",
  ]) {
    for (const line of read(path).split("\n")) {
      const decision = line.match(/specgate (?:--yes )?change (approve|accept|request-changes)\b/);
      if (decision && !line.includes("specgate --yes change")) bare.push(`${path}: ${decision[0]}`);
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

test("the public installer explains its flags without installing", () => {
  const help = execFileSync("sh", ["scripts/install-cli.sh", "--help"], { cwd: root }).toString("utf8");

  assert.match(help, /Usage:/);
  assert.match(help, /--version/);
  assert.match(help, /--install-dir/);
  assert.doesNotMatch(help, /--server|--no-config/);
});

test("the public installer explains missing flag values", () => {
  for (const flag of ["--version", "--install-dir"]) {
    for (const args of [[flag], [flag, "--help"]]) {
      assert.throws(
        () => execFileSync("sh", ["scripts/install-cli.sh", ...args], { cwd: root, stdio: "pipe" }),
        (error) => error.status === 1 && error.stderr.toString("utf8").includes(`${flag} requires a value`),
        args.join(" "),
      );
    }
  }
});

test("the public installer rejects retired remote setup flags before downloading", () => {
  for (const args of [["--server", "https://127.0.0.1"], ["--no-config"]]) {
    assert.throws(
      () => execFileSync("sh", ["scripts/install-cli.sh", ...args], { cwd: root, stdio: "pipe" }),
      error => error.status === 1 && /Unknown flag/.test(error.stderr.toString("utf8")),
    );
  }
});

test("public docs explain the Local trust boundary and legacy data recovery", () => {
  const readme = read("README.md");
  assert.match(readme, /loopback/);
  assert.match(readme, /local\s+processes and accounts that can reach it/);
  assert.match(readme, /release notes before updating/i);
  assert.match(readme, /CHANGELOG\.md/);
  const recovery = read("docs/using-specgate/guides/operate-specgate.md");
  assert.match(recovery, /preserv|in place/i);
  assert.match(recovery, /`SPECGATE_CONFIG_PATH`[^.]{0,100}separate/);
  assert.match(recovery, /unsigned bounded review request/);
  assert.match(read("docs/using-specgate/reference/feature-status.md"), /## Retired distribution features/);
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

test("re-running a version reuses its draft instead of adding a second", () => {
  // A retry must preserve one private asset set for the tag.
  const goreleaser = read(".goreleaser.yaml");
  const releaseBlock = goreleaser.slice(goreleaser.indexOf("\nrelease:"));
  assert.match(releaseBlock, /^\s*draft: true$/m, "assets must stay private until the smoke job passes");
  assert.match(
    releaseBlock,
    /^\s*use_existing_draft: true$/m,
    "goreleaser must reuse the existing draft; creating a second one splits a release's assets across two records",
  );
  assert.match(
    releaseBlock,
    /^\s*replace_existing_artifacts: true$/m,
    "a retried release must replace stale CLI assets in the reused draft",
  );

  const workflow = read(".github/workflows/release.yml");
  assert.match(
    workflow,
    /expected exactly one draft release/,
    "the smoke job's one-draft assertion is what catches a split release; keep it",
  );
});

test("release verification covers every module a release ships", () => {
  const verifyJob = read(".github/workflows/release.yml");

  for (const command of [
    "make test",
    "bash .github/scripts/check-legacy-cli.sh",
    "make check-plugins",
    "node --test docs/release-readiness.test.mjs",
    "scripts/install-cli.test.mjs",
    "app/landing/demo/demo.test.mjs",
  ]) {
    assert.ok(verifyJob.includes(command), `release verification does not run ${command}`);
  }
});

test("the canonical plugin version matches the most recent authored release notes", () => {
  const { version } = JSON.parse(read("plugins/package.json"));
  const latest = read("CHANGELOG.md").match(/^## \[([^\]]+)\]/m)?.[1];
  assert.equal(version, latest);
  const tag = `v${version}`;
  assert.ok(execFileSync(process.execPath, [".github/scripts/release-notes.mjs", tag], { cwd: root }).length > 0);
});

test("Local browser and pilot checks run in their Node 26 CI jobs", () => {
  const cli = read(".github/workflows/cli.yml");
  assert.match(cli, /actions\/setup-node@v\d+\s+with:\s+node-version: "26"(?:(?!\n  \w)[\s\S])*\bmake test\b/, "CLI tests require Node 26 before make test");
  const cliTests = read("app/cli/Makefile").match(/^\tnode --test (.+)$/m)?.[1]?.split(/\s+/) || [];
  for (const path of workingFiles().filter((path) => /^app\/cli\/internal\/command\/local_ui.*[._]test\.cjs$/.test(path))) {
    assert.ok(cliTests.includes(path.slice("app/cli/".length)), `make test does not run ${path}`);
  }
  for (const path of [".github/workflows/release-readiness.yml", ".github/workflows/release.yml"]) {
    const workflow = read(path);
    assert.match(workflow, /actions\/setup-node@v\d+\s+with:\s+node-version: "26"(?:(?!\n  \w)[\s\S])*node --test [^\n]*\bscripts\/local-review-notifications\.test\.mjs\b/, path);
  }
});

test("native Windows CLI and release jobs cover Local UI cache cleanup and transfer", () => {
  for (const path of [".github/workflows/cli.yml", ".github/workflows/release.yml"]) {
    const workflow = read(path);
    assert.match(workflow, /runs-on: windows-latest(?:(?!\n  \w)[\s\S])*go test .* -run '[^'\n]*\bTestLocalUIMaintenanceCacheDeletesOnlyConfirmedOwnedCache\b[^'\n]*'/, path);
    assert.match(workflow, /runs-on: windows-latest(?:(?!\n  \w)[\s\S])*go test \.\/internal\/command -run '[^'\n]*\bTestTransfer\b/, path);
    assert.match(workflow, /runs-on: windows-latest(?:(?!\n  \w)[\s\S])*go test \.\/internal\/local -run '[^'\n]*\bTestTransferGeneration\b[^'\n]*\bTestGenerationStore\b/, path);
  }
});

test("CLI and release CI execute the real legacy compatibility gate", () => {
  const gatePath = ".github/scripts/check-legacy-cli.sh";
  for (const path of [".github/workflows/cli.yml", ".github/workflows/release.yml"]) {
    const workflow = read(path);
    assert.ok(workflow.includes(`bash ${gatePath}`), `${path} does not run the compatibility gate`);
  }
  const gate = read(gatePath);
  assert.match(gate, /baseline=[a-f0-9]{40}\b/, "baseline must be an immutable pre-enhancement revision");
  assert.match(gate, /pre_transfer=[a-f0-9]{40}\b/, "prior enhanced writer must be pinned to an immutable revision");
  assert.match(gate, /git archive.*\bapp\/cli\b/, "build the baseline, not the implementation under test");
  assert.match(gate, /go build.*-o/, "gate must supply an actual executable");
  const gateCommand = gate.replaceAll(/\\\n\s*/g, " ");
  for (const env of ["SPECGATE_LEGACY_CLI", "SPECGATE_PRE_TRANSFER_CLI"]) {
    assert.match(gateCommand, new RegExp(`${env}=.*go test.*-count=1`), `${env} tests must run without caching or an optional skip`);
  }
  for (const path of workingFiles().filter(path => path.startsWith("app/cli/internal/local/") && path.endsWith("_test.go"))) {
    for (const [, name, body] of read(path).matchAll(/func (Test\w+)\(t \*testing\.T\) \{([\s\S]*?)(?=\nfunc |$)/g)) {
      if (/os\.Getenv\("SPECGATE_(?:LEGACY|PRE_TRANSFER)_CLI"\)/.test(body)) {
        assert.ok(gate.includes(name.replace(/^Test/, "")), `gate omits ${name}`);
      }
    }
  }
});

test("a private CLI release draft verifies the shipped platform archives before publish", () => {
  const workflow = read(".github/workflows/release.yml");
  const smoke = workflow.slice(workflow.indexOf("\n  release-cli-smoke:"), workflow.indexOf("\n  release-publish:"));
  const publish = workflow.slice(workflow.indexOf("\n  release-publish:"));
  const packaging = read(".goreleaser.yaml");
  for (const [os, architectures, format] of [
    ["linux", ["amd64", "arm64"], "tar.gz"],
    ["darwin", ["amd64", "arm64"], "tar.gz"],
    ["windows", ["amd64"], "zip"],
  ]) {
    assert.match(packaging, new RegExp(`^\\s+- ${os}$`, "m"));
    for (const arch of architectures) assert.ok(smoke.includes(`_${os}_${arch}.${format}`), `${os}/${arch}`);
  }
  assert.match(workflow, /concurrency:\n  group: release-/);
  assert.match(smoke, /permissions:[\s\S]*?contents: write/);
  assert.match(smoke, /sha256sum -c -/);
  assert.match(smoke, /cmp install-cli\.sh/);
  assert.match(smoke, /local-delivery-proof\.mjs/);
  assert.match(smoke, /Next actor: human_reviewer/);
  assert.match(publish, /needs: release-cli-smoke/);
});

test("the release guide matches the automation it drives", () => {
  const guide = read("docs/contributing/release.md");
  assert.match(guide, /git push origin "\$VERSION"/);
  assert.match(guide, /Do not create or publish a GitHub Release/i);
  assert.match(guide, /draft/);
  assert.match(guide, /native Windows/i);
});

test("Pages regenerates and tests the public demo before uploading the landing site", () => {
  const workflow = read(".github/workflows/pages.yml");
  const events = workflow.slice(workflow.indexOf("\non:"), workflow.indexOf("\npermissions:"));
  const push = events.slice(events.indexOf("  push:"), events.indexOf("  pull_request:"));
  const pullRequest = events.slice(events.indexOf("  pull_request:"), events.indexOf("  workflow_dispatch:"));
  const builder = "scripts/build-landing-demo.mjs";
  for (const event of [push, pullRequest]) {
    for (const path of ["app/landing/**", builder, "app/cli/internal/command/local_ui_assets/**"]) {
      assert.ok(event.includes(`"${path}"`), `changes to ${path} must rebuild the public demo`);
    }
  }
  const build = workflow.indexOf(`node ${builder}`);
  const tests = workflow.indexOf("app/landing/demo/demo.test.mjs");
  const upload = workflow.indexOf("actions/upload-pages-artifact@");
  assert.ok(build >= 0 && build < tests && tests < upload, "Pages builds and tests the demo before it uploads");

  assert.match(workflow, /push:\s*\n\s+branches: \["main"\]\s*\n\s+paths:/);
  assert.match(workflow, /pull_request:\s*\n\s+paths:/);
  assert.match(
    workflow,
    /deploy:\s*\n\s+if: github\.event_name != 'pull_request'[\s\S]*?concurrency:\s*\n\s+group: pages/,
  );
});
