import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { test } from "node:test";
import { createRequire } from "node:module";
import { runInNewContext } from "node:vm";
import { sample, beginDecision, recordDecision, reset, tourStep } from "./sample.mjs";

const { rungs, withRequiredTests } = createRequire(import.meta.url)("../../../app/cli/internal/command/local_ui_assets/presentation.js");
const ladder = (version) => withRequiredTests(sample(version).rows, { completion: sample(version).completion_data }).map((row) => rungs(row));

test("the built page uses the shared presentation without starting the production app", async () => {
  const html = await readFile(new URL("index.html", import.meta.url), "utf8");
  const assets = [...html.matchAll(/(?:src|href)="(\.\/[^\"]+)"/g)].map(([, path]) => path);
  assert.ok(assets.includes("./shared/presentation.js"));
  assert.ok(assets.includes("./shared/source-view.js"));
  assert.ok(assets.includes("./demo.mjs"));
  assert.ok(!assets.some((path) => path.endsWith("/app.js")));
  const built = await Promise.all(assets.map((path) => readFile(new URL(path, import.meta.url), "utf8")));
  assert.ok(built.every((content) => content.length > 0));
  assert.match(built[assets.indexOf("./shared/style.css")], /url\("\.\/assets\/geist\.woff2"\)/);
});

test("sample opening matches the landing story and includes real example source and diff", () => {
  const delivery = sample(1);
  assert.match(delivery.source.diff.text, /^\+.*503/m);
  assert.ok(delivery.source.source.lines.some((line) => line.text.includes("503")));
  assert.match(delivery.source.diff.text, /--- a\/health\.go\n\+\+\+ b\/health\.go/);
});

test("the sample rows give the shared ladder the landing story, and the revision closes the gap", () => {
  assert.deepEqual(ladder(1).map(({ test, check, problem }) => ({ test, check, problem })), [
    { test: "ok", check: "ok", problem: "" },
    { test: "gap", check: "ok", problem: "gap" },
    { test: "none", check: "none", problem: "" },
  ]);
  assert.deepEqual(ladder(1).map((r) => [r.claim, r.source]), [["said", "said"], ["said", "said"], ["said", "said"]]);
  assert.deepEqual(ladder(2).map((r) => r.problem), ["", "", ""]);
  assert.equal(ladder(2)[1].test, "ok");
});

test("request changes needs a note and the exact sample confirmation", () => {
  const opening = reset();
  assert.equal(beginDecision(opening, "reject", "   "), null);
  const held = beginDecision(opening, "reject", " Add the 503 test. ");
  assert.equal(held.review, "sample-review-1");
  assert.equal(held.completion, "sample-delivery-1");
  assert.equal(recordDecision(opening, { ...held, completion: "sample-delivery-2" }), opening);
  const requested = recordDecision(opening, held);
  assert.deepEqual(requested.decision, { verdict: "reject", note: "Add the 503 test." });
  assert.equal(beginDecision(requested, "approve", ""), null);
  assert.equal(recordDecision(requested, held), requested);
});

test("the revised sample, acceptance and reset remain reversible and local", () => {
  const revised = reset(2);
  const next = recordDecision(revised, beginDecision(revised, "approve", ""));
  assert.equal(next.decision.verdict, "approve");
  assert.equal(next.version, 2);
  assert.deepEqual(reset(), { version: 1, decision: null });
});

test("the tour goes from 503 evidence to the diff and then the decision", () => {
  const state = reset();
  assert.deepEqual(
    [0, 1, 2].map((index) => tourStep(state, index).target),
    [
      { criterion: "local-2", source: "code", anchor: "evidence" },
      { criterion: "local-2", source: "diff", anchor: "source" },
      { anchor: "dock" },
    ],
  );
  assert.ok(sample(1).rows.some((row) => row.criterion_id === "local-2" && row.text.includes("503")));
  assert.equal(tourStep(state, -1), undefined);
  assert.equal(tourStep(state, 3), undefined);
});

test("the demo reuses the shared review renderer instead of its own", async () => {
  const code = await readFile(new URL("demo.mjs", import.meta.url), "utf8");
  for (const name of ["decisionView", "evidenceView", "outcomes"]) assert.ok(!code.includes(name), name);
  for (const name of ["reviewScreen", "withRequiredTests"]) assert.ok(code.includes(name), name);
});

test("restarting the tour explains the current sample and preserves its exact decision", () => {
  const revised = reset(2);
  const accepted = recordDecision(revised, beginDecision(revised, "approve", "Looks good"));
  assert.match(tourStep(accepted, 0).text, /passing 503/);
  assert.match(tourStep(revised, 2).text, /AC-03.*agent’s claim/);
  assert.match(tourStep(accepted, 2).text, /already has a decision/);
  assert.equal(sample(accepted.version).completion, "sample-delivery-2");
  assert.deepEqual(accepted.decision, { verdict: "approve", note: "Looks good" });
});

test("the demo opens the sample scope in a drawer dialog, not a full-page view", async () => {
  const code = await readFile(new URL("demo.mjs", import.meta.url), "utf8");
  const html = await readFile(new URL("index.html", import.meta.url), "utf8");
  for (const name of ["scope-view", "scopeOpen", "Back to review", "scope-close"]) assert.ok(!code.includes(name), name);
  assert.match(html, /<dialog id="scope-dialog" class="drawer"/);
  assert.match(code, /scopeDialog\.showModal\(\)/);
  assert.match(code, /drawerFrame\(/);
  assert.match(code, /data-close-scope/);
  assert.match(code, /event\.target === scopeDialog/);
  assert.match(code, /scopeDialog\.addEventListener\("close"[^\n]*\.focus\(\)/);
});

test("the demo renders the shared single top bar and never patches the old review bar", async () => {
  const code = await readFile(new URL("demo.mjs", import.meta.url), "utf8");
  const html = await readFile(new URL("index.html", import.meta.url), "utf8");
  const css = await readFile(new URL("demo.css", import.meta.url), "utf8");
  assert.ok(!code.includes("review-bar"));
  assert.ok(!(html + css).includes("review-bar"));
  assert.match(code, /topBar\(\{[^}]*ref: "DEMO-001"[^}]*back: false/);
  assert.match(html, /<div id="bar"><\/div>/);
  assert.doesNotMatch(html, /<header|navbar-inner|id="controls"/);
  assert.match(css, /\.demo-banner \{[^}]*padding: 6px 12px 6px 16px/);
  const { topBar } = createRequire(import.meta.url)("../../../app/cli/internal/command/local_ui_assets/presentation.js");
  const bar = topBar({ t: (x) => x, href: () => "#", lang: "en", theme: "light", ref: "DEMO-001", scopeHref: "#", back: false, home: "../", logo: "../logo.svg" });
  assert.match(bar, /^<header class="navbar"><a class="brand" href="\.\.\/"/);
  assert.match(bar, /<code class="ref">DEMO-001<\/code>[\s\S]*data-scope[\s\S]*data-language="vi"/);
  assert.doesNotMatch(bar, /class="back"|class="pos"/);
});

test("demo criterion shortcuts preserve native keys outside the list and at its ends", async () => {
  const code = await readFile(new URL("demo.mjs", import.meta.url), "utf8");
  const start = code.indexOf('window.addEventListener("keydown"');
  const handlerSource = code.slice(start, code.indexOf('\n});', start) + 4);
  let handler;
  const scopeDialog = { open: false }, dialog = { open: false };
  const selected = [];
  runInNewContext(handlerSource, {
    window: { addEventListener: (_, listener) => { handler = listener; } },
    scopeDialog, dialog,
    select: (id) => { selected.push(id); },
  });
  const next = { dataset: { criterionSelect: "local-2" }, getAttribute: () => null };
  const criterion = { parentElement: { nextElementSibling: { querySelector: () => next } } };
  function press(key, button) {
    let prevented = false;
    handler({ key, target: { closest: () => button }, preventDefault() { prevented = true; } });
    return prevented;
  }
  assert.equal(press("ArrowDown", null), false);
  assert.equal(press("ArrowUp", criterion), false);
  assert.equal(press("j", criterion), true);
  assert.deepEqual(selected, ["local-2"]);
  scopeDialog.open = true;
  assert.equal(press("ArrowDown", criterion), false);
});
