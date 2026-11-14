const { test } = require("node:test");
const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { runInNewContext } = require("node:vm");
const ui = require("./local_ui_assets/core.js");

test("HTML rendering escapes original content without translating it", () => {
  assert.equal(
    ui.escape("<script>\"&' Your decision"),
    "&lt;script&gt;&quot;&amp;&#39; Your decision",
  );
  const title = "Your decisions · Chưa dịch <i>hello</i>";
  const rows = ui.criteria(
    {
      criteria: [
        {
          criterion_id: "local-1",
          text: title,
          verdict: "met",
          why: "Original agent explanation",
        },
      ],
      criterion_evidence: [
        {
          id: "local-1",
          claim: "satisfied",
          check_provenance: "agent_attested",
        },
      ],
    },
    [],
  );
  assert.equal(rows[0].text, title);
  assert.equal(rows[0].why, "Original agent explanation");
  assert.equal(ui.provenance(rows[0], "en").kind, "claim");
});
test("only selected passing testcase observations produce selected-test summary", () => {
  assert.equal(
    ui.provenance(
      {
        claim: "satisfied",
        check_source: "specgate_cli",
        check_status: "pass",
        check_provenance: "command_only",
      },
      "en",
    ).kind,
    "command",
  );
  assert.equal(
    ui.provenance(
      {
        claim: "satisfied",
        check_provenance: "selected_test_observed",
        check_status: "pass",
        selected_cases: [],
      },
      "en",
    ).kind,
    "missing",
  );
  assert.equal(
    ui.provenance(
      {
        claim: "satisfied",
        check_provenance: "selected_test_observed",
        check_status: "pass",
        selected_cases: [{ outcome: "passed" }],
      },
      "en",
    ).kind,
    "test",
  );
  assert.equal(
    ui.provenance(
      {
        check_provenance: "selected_test_observed",
        check_status: "pass",
        selected_cases: [{ outcome: "failed" }],
      },
      "vi",
    ).kind,
    "missing",
  );
});
test("decision pins need current server authorization, independent of gaps or freshness", () => {
  const status = {
    ref: "LOCAL-real",
    review_id: "r",
    completion_id: "c",
    basis_digest: "d",
    next_actor: "human_reviewer",
    freshness: "unavailable",
    missing: ["gap"],
  };
  assert.deepEqual(ui.decisionPins({ can_decide: true, status }), {
    ref: "LOCAL-real",
    review_id: "r",
    completion_id: "c",
    basis_digest: "d",
  });
  assert.equal(ui.decisionPins({ can_decide: false, status }), null);
  assert.deepEqual(
    ui.decisionPins({
      can_decide: true,
      status: { ...status, basis_digest: "" },
    }),
    { ref: "LOCAL-real", review_id: "r", completion_id: "c" },
  );
  assert.equal(
    ui.decisionPins({
      can_decide: true,
      status: { ...status, completion_id: "" },
    }),
    null,
  );
});
test("request sequence ignores stale responses after navigating", () => {
  const gate = ui.requestGate();
  const first = gate.start(),
    second = gate.start();
  assert.equal(gate.current(first), false);
  assert.equal(gate.current(second), true);
});
test("human queue is based on next actor, not a passing test or stale flag", () => {
  assert.equal(
    ui.needsHuman({ next_actor: "human_reviewer", stale: true }),
    true,
  );
  assert.equal(
    ui.needsHuman({
      next_actor: "implementing_agent",
      state: "awaiting_acceptance",
    }),
    false,
  );
});

test("stale wording remains neutral and only appears for a stale review", () => {
  assert.equal(
    ui.staleNotice({ stale: true, stale_reason: "Approved context changed" }),
    "Review freshness · Approved context changed",
  );
  assert.equal(ui.staleNotice({ stale: true }), "Review freshness");
  assert.equal(
    ui.staleNotice({ stale: false, stale_reason: "old observation" }),
    "",
  );
});

test("expanded evidence omits enum claims and empty fields without losing source or original notes", () => {
  assert.deepEqual(
    ui.criterionDetails(
      { claim: "satisfied", evidence_path: "src/a.go", citation: "line: 8" },
      { claim: "satisfied" },
    ),
    { report: "", source: "src/a.go · line: 8", check: "", tests: "" },
  );
  const row = {
    check_name: "unit",
    selected_cases: [
      { classname: "Suite", name: "test real", outcome: "passed" },
    ],
  };
  assert.deepEqual(
    ui.criterionDetails(row, {
      claim: "satisfied",
      note: "Keep original wording <&>",
    }),
    {
      report: "Keep original wording <&>",
      source: "",
      check: "unit",
      tests: "Suite/test real: passed",
    },
  );
  assert.equal(
    ui.criterionDetails({}, { claim: "The original human-readable claim" })
      .report,
    "The original human-readable claim",
  );
});

test("legacy recorded decisions preserve feedback and IDs without inventing actor or decision time", () => {
  const data = {
    status: { ref: "LOCAL-test" },
    review: {
      id: "review",
      report_id: "completion",
      human_decision: "reject",
      note: "Please fix this exact case",
      created_at: "review-creation-only",
    },
  };
  assert.deepEqual(ui.recordedDecision(data), {
    decision: "reject",
    note: "Please fix this exact case",
    ref: "LOCAL-test",
    review_id: "review",
    completion_id: "completion",
  });
  assert.equal(
    ui.recordedDecision({
      ...data,
      review: { ...data.review, human_decision: "" },
    }),
    null,
  );
  assert.equal(
    ui.recordedDecision({
      ...data,
      status: {
        ...data.status,
        recorded_acceptance_basis: {
          review_id: "different",
          actor: "old",
          observed_at: "old",
        },
      },
    }).actor,
    undefined,
  );
});

test("header appearance buttons expose current language and named theme action without selects", () => {
  const html = ui.appearanceControls(
    { lang: "vi", theme: "light" },
    {
      language: "Language",
      dark: "Switch to dark theme",
      light: "Switch to light theme",
    },
    "<svg></svg>",
  );
  assert.match(html, /data-language="vi"[^>]*aria-pressed="true"/);
  assert.match(html, /data-language="en"[^>]*aria-pressed="false"/);
  assert.match(
    html,
    /data-action="theme"[^>]*aria-label="Switch to dark theme"/,
  );
  assert.doesNotMatch(html, /<select/);
});
test("appearance actions persist only supported language or explicit theme choices", () => {
  const state = { lang: "vi", theme: "system" },
    writes = [];
  const persist = (key, value) => writes.push([key, value]);
  assert.equal(ui.setAppearance(state, "language", "en", persist), true);
  assert.equal(ui.setAppearance(state, "theme", "dark", persist), true);
  assert.deepEqual(writes, [
    ["specgate-local-language", "en"],
    ["specgate-local-theme", "dark"],
  ]);
  assert.equal(state.lang, "en");
  assert.equal(state.theme, "dark");
  assert.equal(
    ui.setAppearance(state, "language", "arbitrary", persist),
    false,
  );
  assert.equal(writes.length, 2);
});

test("legacy maintenance links open settings without losing the inspected work URL", () => {
  const result = ui.maintenanceLocation(
    "http://127.0.0.1:1234/?ref=LOCAL-real&tab=decision&page=tools",
  );
  assert.equal(result.openSettings, true);
  assert.equal(
    result.url,
    "http://127.0.0.1:1234/?ref=LOCAL-real&tab=decision",
  );
  assert.equal(
    ui.maintenanceLocation("http://127.0.0.1:1234/?ref=LOCAL-real")
      .openSettings,
    false,
  );
});

test("header tab order follows its visible language, notification, settings and theme order", () => {
  const html = ui.appearanceControls(
    { lang: "en", theme: "dark" },
    { language: "Language", dark: "Dark", light: "Light" },
    "",
    '<button data-action="notifications"></button><button data-action="settings"></button>',
  );
  const positions = [
    'data-language="en"',
    'data-language="vi"',
    'data-action="notifications"',
    'data-action="settings"',
    'data-action="theme"',
  ].map((value) => html.indexOf(value));
  assert.ok(positions.every((value) => value >= 0));
  assert.deepEqual(
    positions,
    [...positions].sort((a, b) => a - b),
  );
});

const markdown = require("./local_ui_assets/markdown.js");
test("Markdown renders nested inline content, lists, code and tables with the actual vendored parser", () => {
  const html = markdown.renderMarkdown(
    "# Original title\n\n**Bold with *nested emphasis*** and `a < b`.\n\n- first\n  - nested\n\n> Quoted text\n\n| Column | Value |\n| --- | --- |\n| a | **b** |\n\n```html\n<script>alert(1)</script>\n```",
  );
  assert.match(html, /<h1>Original title<\/h1>/);
  assert.match(html, /<strong>Bold with <em>nested emphasis<\/em><\/strong>/);
  assert.match(html, /<code>a &lt; b<\/code>/);
  assert.match(html, /<ul>[\s\S]*nested[\s\S]*<\/ul>/);
  assert.match(html, /<blockquote>/);
  assert.match(html, /<table>/);
  assert.match(html, /<strong>b<\/strong>/);
  assert.match(html, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
  assert.doesNotMatch(html, /<script/);
});
test("Markdown escapes raw HTML, disables images and permits only absolute HTTP(S) links", () => {
  const html = markdown.renderMarkdown(
    '<iframe src="https://bad.test"></iframe>\n\n<img src=x onerror=alert(1)>\n\n[bad](javascript:alert%281%29) [encoded](jav&#x61;script:alert%281%29) [data](data:text/html,bad) [relative](/api/decision) [protocol](//bad.test) [mail](mailto:a@b.test)\n\n[good **label**](https://example.test/path?a=1&b=2 "A safe title")\n\n![Original diagram <&>](https://bad.test/tracker.png)',
  );
  assert.doesNotMatch(html, /<(?:script|iframe|img)\b/i);
  assert.doesNotMatch(html, /href="(?:javascript|data|mailto|\/)/i);
  assert.match(html, /&lt;iframe/);
  assert.match(html, /&lt;img/);
  assert.equal((html.match(/<a /g) || []).length, 1);
  assert.match(html, /href="https:\/\/example\.test\/path\?a=1&amp;b=2"/);
  assert.match(html, /<strong>label<\/strong>/);
  assert.match(html, /Original diagram &lt;&amp;&gt;/);
});
test("Markdown handles attribute injection, reference links and task lists without active content", () => {
  const html = markdown.renderMarkdown(
    '[safe](https://example.test/ "&quot; onmouseover=&quot;evil")\n\n[unsafe][u]\n\n[u]: javascript:alert(1)\n\n- [x] complete\n- [ ] pending\n\n<div onclick="evil">**literal raw HTML**</div>',
  );
  assert.match(
    html,
    /title="&amp;quot; onmouseover=&amp;quot;evil" target="_blank"/,
  );
  assert.doesNotMatch(html, /" onmouseover="/);
  assert.doesNotMatch(html, /href="javascript:/i);
  assert.match(html, /disabled/);
  assert.match(html, /&lt;div onclick=/);
});
test("generated Quick Handoff groups only exact known technical headings and retains every original byte", () => {
  const source =
    "# Implementation Context Pack: LOCAL-real\r\n\r\n## Quick Handoff\r\n\r\n### Intent\r\n\r\nOriginal purpose\r\n\r\n### Acceptance criteria\r\n\r\n- First original AC\r\n\r\n### Impact declaration\r\n\r\nQuick route selected.\r\n\r\n### Policy snapshot\r\n\r\nPolicy digest: exact-digest\r\n";
  const result = markdown.renderContext(source, {
    generatedQuick: true,
    workRef: "LOCAL-real",
  });
  assert.equal(result.source, source);
  assert.match(result.html, /Original purpose/);
  assert.match(result.html, /First original AC/);
  assert.doesNotMatch(result.html, /Policy digest|Impact declaration/);
  assert.match(result.technicalHTML, /Impact declaration/);
  assert.match(result.technicalHTML, /exact-digest/);
  const authored = markdown.renderContext(source, {
    generatedQuick: false,
    workRef: "LOCAL-real",
  });
  assert.equal(authored.technicalHTML, "");
  assert.match(authored.html, /Policy digest/);
  const changed = markdown.renderContext(
    source.replace("### Impact declaration", "### Other section"),
    { generatedQuick: true, workRef: "LOCAL-real" },
  );
  assert.equal(changed.technicalHTML, "");
  assert.match(changed.html, /Other section/);
});

test("review presentation never upgrades grounded agent claims or commands into passing tests", () => {
  const claim = {
    criterion_id: "local-2",
    claim: "satisfied",
    grounding_status: "grounded",
    verdict: "met",
  };
  assert.equal(ui.reviewPresentation(claim, "vi").label, "Cần xem");
  assert.equal(ui.reviewPresentation(claim, "vi").id, "AC-02");
  const command = {
    ...claim,
    check_source: "specgate_cli",
    check_provenance: "command_only",
    check_status: "pass",
  };
  assert.equal(ui.reviewPresentation(command, "en").tone, "neutral");
  const tested = {
    ...command,
    check_provenance: "selected_test_observed",
    selected_cases: [{ outcome: "passed" }],
  };
  assert.equal(ui.reviewPresentation(tested, "vi").label, "Test đạt");
  assert.notEqual(
    ui.reviewPresentation({ ...tested, verdict: "unmet" }, "vi").tone,
    "success",
  );
  assert.equal(
    ui.reviewPresentation({ ...claim, criterion_id: "source-original" }, "en")
      .id,
    "source-original",
  );
});
test("a decision success state requires a recorded server decision", () => {
  const data = {
    can_decide: true,
    status: {
      ref: "LOCAL-real",
      review_id: "r",
      completion_id: "c",
      basis_digest: "d",
      state: "awaiting_acceptance",
    },
  };
  assert.equal(ui.recordedDecision(data), null);
  assert.ok(ui.decisionPins(data));
  assert.equal(
    ui.recordedDecision({
      ...data,
      review: {
        id: "r",
        report_id: "c",
        human_decision: "approve",
        note: "Original note",
      },
    }).decision,
    "approve",
  );
});

const presentation = require("./local_ui_assets/presentation.js");
test("Local attention reflects work transitions independently of Settings updates", async () => {
  const work = { ref: "LOCAL-review", title: "Check the evidence" };
  const maintainer = {
    ref: "LOCAL-blocked",
    title: "Missing delivery policy",
    state: "blocked",
    next_actor: "maintainer",
  };
  const accepted = { ...work, state: "accepted", next_actor: "none" };
  const unknown = { ref: "LOCAL-unknown", title: "Unknown", next_actor: null };
  let items = [
    { ...work, state: "awaiting_acceptance", next_actor: "human_reviewer" },
    unknown,
  ];
  let status = "current";
  let workReadError = false;
  let poll;
  const element = () => ({
    innerHTML: "",
    addEventListener(event, listener) { this[event] = listener; },
    querySelector() { return element(); },
    querySelectorAll() { return []; },
    setAttribute() {},
    focus() {},
    contains() { return false; },
    showModal() { this.open = true; },
  });
  const app = element(), dialog = element(), settings = element();
  const nodes = { app, dialog, "settings-dialog": settings };
  const reply = (data) => ({ ok: true, json: async () => ({ ok: true, data }) });
  const browser = {
    SpecGateUI: ui,
    SpecGatePresentation: presentation,
    SpecGateSource: { mount: () => ({ clear() {}, restore() {} }) },
    document: {
      getElementById: (id) => id === "queue-results" ? element() : nodes[id],
      querySelector: element,
      documentElement: { dataset: {} },
      activeElement: { tagName: "BODY" },
    },
    location: new URL("http://127.0.0.1:1234/"),
    URLSearchParams,
    window: element(),
    localStorage: { getItem: (key) => key.endsWith("language") ? "en" : null },
    matchMedia: element,
    setInterval: (callback) => { poll = callback; },
    fetch: async (path) => {
      switch (path) {
        case "/api/session": return reply({ csrf_token: "test" });
        case "/api/meta": return reply({ version: "fixture" });
        case "/api/works":
          if (workReadError) throw Error("Workspace read failed");
          return reply({ items });
        case "/api/update": return reply({ status, latest_version: "9.0.0" });
        case "/api/cache":
          return reply({ entries: [], reason: "Cache cleanup is unavailable on this platform." });
        default: assert.fail(`Unexpected request: ${path}`);
      }
    },
  };
  for (const asset of ["locale.js", "app.js"])
    runInNewContext(
      readFileSync(`${__dirname}/local_ui_assets/${asset}`, "utf8"),
      browser,
    );
  const flush = () => new Promise(setImmediate);
  const click = (kind, value) => app.click({
    target: {
      closest: (selector) => selector === `[data-${kind}]`
        ? { dataset: { [kind]: value } }
        : null,
    },
  });
  const bell = () =>
    app.innerHTML.match(/data-action="notifications"[^>]*>(.*?)<\/button>/)[0];
  await flush();
  assert.match(bell(), /class="count"[^>]*>1</);
  assert.match(app.innerHTML, /data-filter="human"[^>]*>Waiting on you<span>1<\/span>/);
  assert.match(app.innerHTML, /class="list-count">Showing 1 of 2 work items/);
  await click("action", "notifications");
  assert.match(dialog.innerHTML, /href="\/\?ref=LOCAL-review"/);
  assert.match(dialog.innerHTML, /Human reviewer · Review delivery/);
  assert.doesNotMatch(dialog.innerHTML, /LOCAL-unknown/);
  dialog.open = false;

  status = "available";
  items = [accepted, unknown];
  poll();
  await flush();
  await click("action", "refresh");
  assert.doesNotMatch(bell(), /class="count"/);
  assert.match(app.innerHTML, /data-filter="human"[^>]*>Waiting on you<span>0<\/span>/);
  await click("action", "notifications");
  assert.match(dialog.innerHTML, /Nothing needs your attention/);
  assert.doesNotMatch(dialog.innerHTML, /Update available|LOCAL-review/);
  dialog.open = false;
  await click("action", "settings");
  await flush();
  assert.match(settings.innerHTML, /Update available/);
  assert.match(settings.innerHTML, /Review update/);
  await click("action", "cache-preview");
  assert.match(dialog.innerHTML, /Cache cleanup is unavailable on this platform\./);
  assert.doesNotMatch(dialog.innerHTML, /id="dialog-confirm"/);
  dialog.open = false;
  settings.open = false;

  items = [
    maintainer,
    { ...work, state: "awaiting_review", next_actor: "human_reviewer" },
  ];
  poll();
  await flush();
  assert.match(bell(), /class="count"[^>]*>2</);
  assert.match(app.innerHTML, /data-action="settings"[^>]*aria-label="Local tools · Update available"/);
  await click("action", "notifications");
  assert.match(dialog.innerHTML, /Maintainer · Inspect blocker/);
  assert.match(dialog.innerHTML, /Human reviewer · Check evidence/);
  dialog.open = false;
  await click("language", "vi");
  assert.match(bell(), /aria-label="Thông báo \(2\)"/);
  assert.match(app.innerHTML, /class="list-count">Hiển thị 2 trong 2 công việc/);
  await click("action", "cache-preview");
  assert.match(dialog.innerHTML, /Nền tảng này chưa hỗ trợ dọn cache\./);
  assert.doesNotMatch(dialog.innerHTML, /id="dialog-confirm"/);
  dialog.open = false;
  await click("action", "notifications");
  assert.match(dialog.innerHTML, /Người bảo trì · Xem vướng mắc/);
  dialog.open = false;

  workReadError = true;
  poll();
  await flush();
  assert.match(bell(), /Chưa tải được danh sách công việc/);
  assert.doesNotMatch(bell(), /class="count"/);
  await click("action", "notifications");
  assert.match(dialog.innerHTML, /Chưa tải được danh sách công việc · Workspace read failed/);
  assert.doesNotMatch(dialog.innerHTML, /Chưa có việc cần bạn chú ý|LOCAL-blocked/);
});

test("shared status badges map waiting, accepted, agent and rework states to actual icon semantics", () => {
  assert.equal(
    presentation.statusPresentation({ state: "awaiting_acceptance" }).icon,
    "clock",
  );
  assert.equal(
    presentation.statusPresentation({ state: "awaiting_review" }).icon,
    "clock",
  );
  assert.equal(
    presentation.statusPresentation({ state: "accepted" }).icon,
    "check-circle",
  );
  assert.equal(
    presentation.statusPresentation({ state: "implementation" }).icon,
    "person",
  );
  assert.equal(
    presentation.statusPresentation({ state: "rework_requested" }).label,
    "Changes requested",
  );
  assert.equal(
    presentation.statusPresentation({ state: "blocked" }).tone,
    "warning",
  );
  assert.match(
    presentation.stateBadge({ state: "awaiting_acceptance" }, (x) => x),
    /data-icon="clock"/,
  );
});
test("test column describes observations only and never repeats an agent claim", () => {
  const claim = { claim: "satisfied", grounding_status: "grounded" };
  assert.deepEqual(presentation.testObservation(claim, "vi"), {
    text: "Chưa có test riêng cho tiêu chí này.",
    tone: "warning",
  });
  const command = {
    ...claim,
    check_source: "specgate_cli",
    check_status: "pass",
    check_provenance: "command_only",
  };
  assert.match(presentation.testObservation(command, "en").text, /command/);
  assert.match(
    presentation.testObservation(command, "en").text,
    /no selected test/,
  );
  const failed = {
    ...command,
    check_provenance: "selected_test_observed",
    check_status: "fail",
    selected_cases: [{ name: "real case", outcome: "failed" }],
  };
  assert.equal(presentation.testObservation(failed, "en").tone, "danger");
  assert.doesNotMatch(
    presentation.testObservation(failed, "en").text,
    /no |absent|missing/i,
  );
  const html = presentation.criterionView(
    {
      ...claim,
      id: "local-1",
      text: "Original AC",
      citation: "line: 73",
      evidence_path: "src/real.go",
    },
    [],
    "vi",
    (x) => x,
  );
  const column = html
    .split('<section class="test-result">')[1]
    .split("</section>")[0];
  assert.match(column, /Chưa có test riêng cho tiêu chí này\./);
  assert.doesNotMatch(column, /Agent báo đạt/);
  assert.match(html, /Dòng 73/);
  assert.match(html, /line: 73/);
});
test("source citation translates only canonical known line syntax and preserves all other original content", () => {
  assert.equal(presentation.citationLabel("line: 73", "vi"), "Dòng 73");
  assert.equal(presentation.citationLabel("line: 73", "en"), "Line 73");
  assert.equal(
    presentation.citationLabel("heading: Original heading", "vi"),
    "heading: Original heading",
  );
  assert.equal(
    presentation.citationLabel("unverified line: 73", "vi"),
    "unverified line: 73",
  );
  assert.equal(presentation.citationLabel("", "vi"), "");
});

const sourceUI = require("./local_ui_assets/source-view.js");
test("source viewer escapes hostile code and highlights only the cited numeric range", () => {
  const data = {
    origin: "current_checkout",
    head_revision: "abcdef1234567890",
    source: {
      available: true,
      highlight_start: 5,
      highlight_end: 6,
      lines: [
        { number: 4, text: "before" },
        { number: 5, text: "<img src=x onerror=alert(1)>" },
        { number: 6, text: 'const value = "&unsafe";' },
      ],
      truncated: true,
    },
    diff: { available: false, reason: "untracked" },
  };
  const html = sourceUI.renderSource(data, { lang: "en" });
  assert.match(html, /&lt;img src=x onerror=alert\(1\)&gt;/);
  assert.doesNotMatch(html, /<img/);
  assert.match(html, /source-line cited[^>]*data-line="5"/);
  assert.doesNotMatch(html, /source-line cited[^>]*data-line="4"/);
  assert.match(html, /abcdef12/);
  assert.doesNotMatch(html.split("<details>")[0], /abcdef1234567890/);
  assert.match(html, /<details>[\s\S]*abcdef1234567890/);
  assert.match(html, /truncated/i);
  assert.match(html, /Current checkout/);
  assert.match(html, /not frozen evidence/i);
  assert.match(html, /data-source-view="diff"[^>]*disabled/);
});
test("diff viewer renders actual escaped change lines and never fabricates unavailable source", () => {
  const data = {
    source: { available: false, reason: "no_numeric_citation" },
    diff: {
      available: true,
      base: "HEAD",
      text: "--- a/demo.go\n+++ b/demo.go\n@@ -1 +1 @@\n-old()\n+<script>new()</script>",
      truncated: true,
    },
  };
  const html = sourceUI.renderSource(data, { view: "diff", lang: "en" });
  assert.match(html, /diff-line added/);
  assert.match(html, /diff-line removed/);
  assert.match(html, /&lt;script&gt;new\(\)&lt;\/script&gt;/);
  assert.doesNotMatch(html, /<script/);
  assert.match(html, /Diff against HEAD/);
  assert.match(html, /truncated/i);
  const unavailable = sourceUI.renderSource(
    { ...data, diff: { available: false, reason: "untracked" } },
    { view: "diff", lang: "en" },
  );
  assert.match(unavailable, /Source unavailable/);
  assert.match(unavailable, /no_numeric_citation/);
  assert.doesNotMatch(unavailable, /source-line/);
});
test("source requests deduplicate by inspected completion and reject stale responses after scope changes", async () => {
  const pending = [],
    requests = [];
  const loader = sourceUI.createSourceLoader((path) => {
    requests.push(path);
    return new Promise((resolve) => pending.push(resolve));
  });
  const status = {
    ref: "LOCAL-real",
    review_id: "r1",
    completion_id: "c1",
    basis_digest: "d1",
  };
  const first = loader.load(status, "local-1"),
    duplicate = loader.load(status, "local-1");
  assert.equal(requests.length, 1);
  assert.match(
    requests[0],
    /^\/api\/source\?ref=LOCAL-real&criterion=local-1$/,
  );
  const second = loader.load({ ...status, completion_id: "c2" }, "local-1");
  assert.equal(requests.length, 2);
  pending[0]({
    source: { available: true, lines: [{ number: 1, text: "old" }] },
  });
  assert.equal(await first, null);
  assert.equal(await duplicate, null);
  pending[1]({
    completion_id: "c2",
    source: { available: true, lines: [{ number: 1, text: "new" }] },
  });
  assert.equal((await second).data.source.lines[0].text, "new");
  const third = loader.load({ ...status, completion_id: "c2" }, "local-2");
  loader.clear();
  pending[2]({ source: { available: true, lines: [] } });
  assert.equal(await third, null);
});

test("source response from a newer completion never renders under an older inspected criterion", async () => {
  const loader = sourceUI.createSourceLoader(async () => ({
    completion_id: "new-report",
    source: {
      available: true,
      lines: [{ number: 5, text: "new report code" }],
    },
  }));
  const result = await loader.load(
    { ref: "LOCAL-real", completion_id: "old-report", review_id: "old-review" },
    "local-1",
  );
  assert.equal(result.changed, true);
  assert.equal(result.data, undefined);
});
test("source unavailability uses friendly normal-flow copy and confines reason codes to technical details", () => {
  const html = sourceUI.renderSource(
    {
      source: { available: false, reason: "binary_or_invalid_utf8" },
      diff: { available: false, reason: "untracked" },
    },
    { lang: "vi" },
  );
  const ordinary = html
    .split('<p class="source-unavailable">')[1]
    .split("</p>")[0];
  assert.match(ordinary, /UTF-8/);
  assert.doesNotMatch(ordinary, /binary_or_invalid_utf8/);
  assert.match(html, /<details>[\s\S]*binary_or_invalid_utf8/);
});
