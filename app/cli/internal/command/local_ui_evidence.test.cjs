const { test } = require("node:test");
const assert = require("node:assert/strict");
const presentation = require("./local_ui_assets/presentation.js");
const t = (text) => text;

const row = (extra) => ({ criterion_id: "local-1", id: "local-1", text: "Original criterion", ...extra });
const label = (html) => html.match(/<span class="res[^"]*">([^<]*)<\/span>/)[1];

test("real gaps and claims show a text label while optional JUnit is never missing evidence", () => {
  const gap = presentation.criteriaList([row({ claim: "partial", gaps: ["citation: <unsafe>"] })], "other", "en", t);
  assert.match(gap, /class="crit gap"/);
  assert.match(gap, /<span class="res">Evidence gap<\/span>/);
  assert.doesNotMatch(gap, /<unsafe>/);
  const optional = presentation.criteriaList([row({ claim: "satisfied", grounding_status: "grounded" })], "other", "en", t);
  assert.match(optional, /class="crit"/);
  assert.doesNotMatch(optional, /Evidence gap/);
  assert.match(optional, /class="res visually-hidden">Agent claim</);
  assert.equal(presentation.rungs(row({ claim: "satisfied" })).problem, "");
});

test("the four rungs separate observed results from agent claims and required-but-missing tests", () => {
  const claim = presentation.rungs(row({ claim: "satisfied", check_status: "pass", check_source: "agent_attested", evidence_path: "a.go", citation: "heading: Intro" }));
  assert.deepEqual(claim, { claim: "said", source: "none", check: "none", test: "none", problem: "" });
  const cited = presentation.rungs(row({ evidence_path: "a.go", citation: "line: 8" }));
  assert.equal(cited.source, "said");
  for (const [status, tone, problem] of [["pass", "ok", ""], ["fail", "bad", "bad"], ["skipped", "gap", "gap"]])
    assert.deepEqual(
      [presentation.rungs(row({ claim: "satisfied", check_source: "specgate_cli", check_status: status })).check, presentation.rungs(row({ check_source: "specgate_cli", check_status: status })).problem],
      [tone, problem],
    );
  const observed = { check_source: "specgate_cli", check_provenance: "selected_test_observed", check_status: "pass" };
  assert.equal(presentation.rungs(row({ ...observed, selected_cases: [{ outcome: "passed" }] })).test, "ok");
  assert.equal(presentation.rungs(row({ ...observed, selected_cases: [{ outcome: "error" }] })).test, "bad");
  assert.equal(presentation.rungs(row({ ...observed, selected_cases: [{ outcome: "skipped" }] })).problem, "gap");
  assert.deepEqual(presentation.rungs(row({ selected_tests_required: true })), { claim: "none", source: "none", check: "none", test: "gap", problem: "gap" });
  assert.equal(presentation.rungs(row({ selected_tests_required: false })).problem, "");
});

test("the compact result distinguishes observed check outcomes from passing tests", () => {
  const base = { claim: "satisfied", verdict: "met", check_source: "specgate_cli", check_provenance: "command_only" };
  for (const [status, text] of [["pass", "Check passed"], ["fail", "Check failed"], ["skipped", "Check skipped"]]) {
    const html = presentation.criteriaList([row({ ...base, check_status: status })], "local-1", "en", t);
    assert.equal(label(html), text);
    assert.match(html, /Check: Observed check · /);
    assert.doesNotMatch(html, /Test passed/);
  }
});

test("only selected and problem criteria show a text label and the selected one names the rungs", () => {
  const rows = [row({ criterion_id: "a", claim: "satisfied" }), row({ criterion_id: "b", claim: "satisfied", gaps: ["x"] }), row({ criterion_id: "c", claim: "satisfied" })];
  const [first, second, third] = presentation.criteriaList(rows, "c", "en", t).split("<li>").slice(1);
  assert.match(first, /class="res visually-hidden"/);
  assert.doesNotMatch(first, /class="names"/);
  assert.match(second, /class="res">Evidence gap</);
  assert.match(third, /aria-current="true"/);
  assert.match(third, /class="res">Agent claim</);
  assert.match(third, /class="names" aria-hidden="true"><span>Claim<\/span><span>Source<\/span><span>Check<\/span><span>Test<\/span>/);
  assert.doesNotMatch(presentation.criteriaList(rows, "c", "en", t).split("<li>")[1], /data-criterion-select="a"[^>]*aria-current/);
});

test("a partial or not-done claim is a problem with its own text label", () => {
  for (const [claim, text] of [["partial", "Agent reports partial"], ["not_done", "Agent reports not done"]]) {
    const data = row({ claim });
    assert.equal(presentation.rungs(data).problem, "gap");
    const html = presentation.criteriaList([data], "another", "en", t);
    assert.match(html, /class="crit gap"/);
    assert.equal(label(html), text);
  }
});

test("a passing selected test does not hide a source gap", () => {
  const html = presentation.criteriaList([row({ claim: "satisfied", verdict: "met", check_source: "specgate_cli", check_provenance: "selected_test_observed", check_status: "pass", selected_cases: [{ name: "real", outcome: "passed" }], gaps: ["citation is unanchored"] })], "x", "en", t);
  assert.match(html, /class="crit gap"/);
  assert.equal(label(html), "Evidence gap");
  assert.match(html, /<i class="n ok"><\/i><\/span>/);
});

test("the evidence pane names all four rungs in English and Vietnamese and quotes claims", () => {
  require("./local_ui_assets/locale.js");
  const vi = (text) => globalThis.SpecGateLocalVi[text] ?? text;
  const data = row({ claim: "satisfied", evidence_path: "src/a.go", citation: "line: 73", check_source: "specgate_cli", check_status: "pass", check_name: "unit" });
  const en = presentation.evidencePane(data, [{ criterion_id: "local-1", summary: "I <changed> it" }], "en", t);
  for (const name of ["Claim", "Source", "Check", "Test"]) assert.match(en, new RegExp(`<dt>${name}</dt>`));
  assert.match(en, /<q>I &lt;changed&gt; it<\/q>/);
  assert.match(en, /src\/a\.go<\/code><span>Line 73</);
  assert.match(en, /data-source-slot/);
  const vn = presentation.evidencePane(data, [], "vi", vi);
  for (const name of ["Báo cáo", "Nguồn", "Kiểm tra", "Test"]) assert.match(vn, new RegExp(`<dt>${name}</dt>`));
  assert.match(vn, /Dòng 73/);
});

test("an agent-attested check is a quoted claim and never rendered as observed", () => {
  const html = presentation.evidencePane(row({ claim: "satisfied", check_status: "pass", check_source: "agent_attested", check_name: "unit" }), [], "en", t);
  const check = html.split("<dt>Check</dt>")[1].split("</div>")[0];
  assert.match(check, /<q>Passed<\/q>/);
  assert.match(check, /not observed/);
  assert.doesNotMatch(check, /Observed check/);
  assert.match(html.split("<dt>Check</dt>")[0].split("<dt>Claim</dt>")[0], /<div class="said">|<div class="none">/);
  assert.match(html, /<div class="none"><dt>Check<\/dt>/);
  const optional = html.split("<dt>Test</dt>")[1];
  assert.match(optional, /No selected test/);
  assert.match(html, /<div class="none"><dt>Test<\/dt>/);
});

test("review summary leads with problems, then claim-only, then observed results", () => {
  assert.equal(presentation.reviewSummary([], t), null);
  assert.deepEqual(presentation.reviewSummary([row({ gaps: ["x"] }), row()], t), { tone: "gap", text: "1 of 2 criteria need attention" });
  assert.equal(presentation.reviewSummary([row({ check_source: "specgate_cli", check_status: "fail" })], t).tone, "bad");
  assert.equal(presentation.reviewSummary([row({ claim: "satisfied" })], t).tone, "none");
  assert.deepEqual(presentation.reviewSummary([row({ check_source: "specgate_cli", check_status: "pass" }), row()], t), { tone: "ok", text: "1 of 2 criteria have CLI-observed results" });
});

test("work age is compact and relative with the exact date kept separate", () => {
  const now = Date.parse("2026-11-15T12:00:00Z");
  assert.equal(presentation.ageLabel("2026-11-13T12:00:00Z", "en", now).text, "2d ago");
  assert.match(presentation.ageLabel("2026-11-13T12:00:00Z", "en", now).date, /2026/);
  assert.equal(presentation.ageLabel("nope", "en", now), null);
});

test("every chosen check is expandable with original scope and separate provenance", () => {
  const html = presentation.reviewChecks({
    summary: "Network was stubbed <only>",
    checks: [
      { name: "unit", source: "specgate_cli", command: "go test ./a", cwd: "app", status: "pass", detail: "executed by specgate: exit 0", selected_cases: [{ classname: "Package", name: "Main", outcome: "passed" }], run_freshness: "matching_endpoints" },
      { name: "unit", command: "go test ./b", status: "skipped", detail: "No audio device" },
    ],
  }, t);
  assert.match(html, /Network was stubbed &lt;only&gt;/);
  assert.equal((html.match(/>unit</g) || []).length, 2);
  assert.match(html, /go test \.\/a/);
  assert.match(html, /go test \.\/b/);
  assert.match(html, /Selected tests observed/);
  assert.match(html, /Package\/Main: passed/);
  assert.match(html, /Agent claim/);
  assert.match(html, /No audio device/);
  assert.doesNotMatch(html, /<only>/);
});

test("an invalid required test report fails the check even when the command exits zero", () => {
  const data = row({
    claim: "satisfied", check_name: "unit", check_source: "specgate_cli",
    check_status: "fail", check_provenance: "command_only", selected_tests_required: true,
    why: "executed by specgate: exit 0 — required JUnit report unavailable",
    selected_cases: [], gaps: ["bound check unit: fail"],
  });
  const html = presentation.evidencePane(data, [], "en", t);
  assert.match(html, /Complete selected-test results are not available/);
  assert.match(html, /required JUnit report unavailable/);
  assert.match(html, /Observed check · Failed/);
  assert.match(html, /<div class="bad"><dt>Check<\/dt>/);
  assert.match(html, /<div class="gap"><dt>Test<\/dt>/);
  assert.doesNotMatch(html, /Command (?:failed|observed)/i);
  assert.doesNotMatch(html, /no selected test/);
});

test("prior decisions show both identities and escape raw notes", () => {
  const html = presentation.reviewComparison({
    state: "available", review_id: "new-review", completion_id: "new-completion",
    prior: { review_id: "old-review", completion_id: "old-completion", decision: "reject", verdict: "failed", note: "<unsafe>" },
    changed_criteria: ["local-2"], changed_checks: ["unit"], summary_changed: true,
  }, t);
  for (const value of ["old-review", "new-review", "old-completion", "new-completion", "local-2", "unit", "&lt;unsafe&gt;"])
    assert.ok(html.includes(value), value);
  assert.doesNotMatch(html, /<unsafe>/);
  assert.match(presentation.reviewComparison({ state: "no_baseline" }, t), /No earlier human decision/);
  assert.match(presentation.reviewComparison({ state: "unavailable" }, t), /Prior evidence is unavailable/);
});

test("comparison leads with the prior note and readable changed evidence", () => {
  const comparison = {
    state: "available", review_id: "new-review", completion_id: "new-completion",
    prior: { review_id: "old-review", completion_id: "old-completion", decision: "reject", verdict: "failed", note: "Review the production path", summary: "Earlier summary" },
    changed_criteria: ["local-2"], changed_checks: ["unit"], summary_changed: true,
    prior_criteria: { "local-2": { claim: "satisfied", check_status: "pass", check_name: "unit", check_provenance: "command_only" } },
    prior_checks: { unit: [{ name: "unit", command: "run first", source: "specgate_cli", status: "pass" }] },
  };
  const rows = [{ criterion_id: "local-2", text: "Production behavior is preserved", claim: "partial", check_name: "unit", check_status: "skipped", check_provenance: "agent_attested" }];
  const completion = {
    summary: "Current summary", checks: [
      { name: "unit", command: "run first", status: "skipped" },
      { name: "unit", command: "run second", status: "fail", detail: "Wrong fixture" },
    ],
  };
  const html = presentation.reviewComparison(comparison, t, { rows, completion });
  const [readable, technical] = html.split('<details class="raw-details">');
  for (const value of ["Review the production path", "AC-02", "Production behavior is preserved", "Agent reports met", "Agent reports partial", "Passed", "Skipped", "run first", "run second", "Wrong fixture", "Earlier summary", "Current summary"])
    assert.ok(readable.includes(value), value);
  assert.ok(readable.indexOf("Review the production path") < readable.indexOf("Production behavior is preserved"));
  assert.doesNotMatch(readable, /(?:new|old)-(?:review|completion)/);
  assert.match(technical, /old-review/);
});

test("new prototype-named checks render with their recorded status and endpoint changes", () => {
  const comparison = {
    state: "available", changed_checks: ["constructor", "unit"],
    prior_checks: { unit: [{ name: "unit", status: "pass", claimed_status: "fail", run_freshness: "matching_endpoints" }] },
  };
  const completion = { checks: [
    { name: "constructor", command: "run a new check", status: "toString" },
    { name: "unit", status: "pass", claimed_status: "skipped", run_freshness: "<changed>" },
  ] };
  const html = presentation.reviewComparison(comparison, t, { completion });
  const readable = html.split('<details class="raw-details">')[0];
  for (const value of ["constructor", "run a new check", "toString", "No check recorded", "Agent-reported status", "Failed", "Skipped", "Verification endpoints", "matching_endpoints", "&lt;changed&gt;"])
    assert.ok(readable.includes(value), value);
  assert.doesNotMatch(readable, /\[native code\]|<changed>/);
});
