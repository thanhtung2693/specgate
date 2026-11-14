const { test } = require("node:test");
const assert = require("node:assert/strict");
const presentation = require("./local_ui_assets/presentation.js");
const t = (text) => text;

test("real gaps and claims are visible without treating optional JUnit as missing evidence", () => {
  const row = { id: "local-1", text: "Original criterion", claim: "partial" };
  const html = presentation.criterionView({ ...row, gaps: ["citation: <unsafe>"] }, [], "en", t);
  assert.match(html.split("</summary>")[0], /Agent reports partial.*Evidence gap/);
  assert.doesNotMatch(html, /<unsafe>/);
  const grounded = presentation.criterionView(
    { ...row, claim: "satisfied", grounding_status: "grounded" }, [], "en", t,
  );
  assert.doesNotMatch(grounded.split("</summary>")[0], /Evidence gap/);
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

test("a required selected test with an invalid report stays explicit", () => {
  const row = {
    id: "local-1", claim: "satisfied", check_name: "unit", check_source: "specgate_cli",
    check_status: "fail", check_provenance: "command_only", selected_tests_required: true,
    why: "required JUnit report unavailable", gaps: ["bound check unit: fail"],
  };
  const html = presentation.criterionView(row, [], "en", t);
  assert.match(html, /Complete selected-test results are not available/);
  assert.match(html, /required JUnit report unavailable/);
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
