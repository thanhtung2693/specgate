const health = [
  "func health(w http.ResponseWriter, r *http.Request) {",
  "    if err := db.PingContext(r.Context()); err != nil {",
  '        writeJSON(w, 503, "degraded")',
  "        return",
  "    }",
  '    writeJSON(w, 200, "ok")',
  "}",
];

const diff = [
  "--- a/health.go",
  "+++ b/health.go",
  "@@ -1,3 +1,7 @@",
  " " + health[0],
  ...health.slice(1, 5).map((line) => "+" + line),
  ...health.slice(5).map((line) => " " + line),
].join("\n");

const observed = { check_source: "specgate_cli", check_status: "pass", check_name: "go test ./..." };
const testCase = (name) => ({ classname: "health", name, outcome: "passed" });

/* Rows use the shape the Local review presentation reads, so its ladder and labels apply unchanged. */
export function sample(version) {
  const revised = version === 2;
  return {
    review: `sample-review-${version}`,
    completion: `sample-delivery-${version}`,
    rows: [
      { criterion_id: "local-1", text: "GET /health → 200", claim: "satisfied", evidence_path: "health.go", citation: "line: 6", verdict: "met", ...observed, check_provenance: "selected_test_observed", selected_cases: [testCase("TestHealthOK")] },
      { criterion_id: "local-2", text: "DB down → 503", claim: "satisfied", evidence_path: "health.go", citation: "line: 3", ...(revised && { verdict: "met" }), ...observed, check_provenance: revised ? "selected_test_observed" : "command_only", selected_cases: revised ? [testCase("TestHealthDBDown")] : [] },
      { criterion_id: "local-3", text: "No error text in body", claim: "satisfied", evidence_path: "health.go", citation: "line: 3" },
    ],
    completion_data: {
      selected_tests_required: ["local-1", "local-2"],
      checks: [{ name: "go test ./...", command: "go test ./...", status: "pass", source: "specgate_cli", selected_tests_required: true, selected_cases: [testCase("TestHealthOK"), ...(revised ? [testCase("TestHealthDBDown")] : [])] }],
    },
    source: {
      path: "health.go",
      source: { available: true, lines: health.map((text, i) => ({ number: i + 1, text })) },
      diff: { available: true, text: diff },
    },
  };
}

export const reset = (version = 1) => ({ version, decision: null });

export function tourStep(state, index) {
  return [
    {
      title: "Check the 503 evidence",
      text: state.version === 1
        ? "The sample test run reported no errors, but AC-02 still has no 503 test result."
        : "AC-02 now shows a passing 503 test result in the revised sample.",
      target: { criterion: "local-2", source: "code", anchor: "evidence" },
    },
    {
      title: "Inspect the cited code",
      text: "The diff shows where the 503 response was added. Check the test result to see if that path ran and passed.",
      target: { criterion: "local-2", source: "diff", anchor: "source" },
    },
    {
      title: "Try a decision",
      text: state.decision
        ? "This sample already has a decision. Use Reset demo to start over."
        : state.version === 1
          ? "Ask for the missing 503 test result in your note. Choose Ask for changes, confirm, then open the revised sample."
          : "The 503 test now has a result. Check AC-03, which still has only the agent’s claim, then accept or request changes.",
      target: { anchor: "dock" },
    },
  ][index];
}

export function beginDecision(state, verdict, draft) {
  const note = draft.trim();
  if (state.decision || (verdict !== "approve" && verdict !== "reject") || (verdict === "reject" && !note)) return null;
  const { review, completion } = sample(state.version);
  return { review, completion, verdict, note };
}

export function recordDecision(state, confirmation) {
  if (!confirmation) return state;
  const expected = beginDecision(state, confirmation.verdict, confirmation.note);
  if (!expected || expected.review !== confirmation.review || expected.completion !== confirmation.completion) return state;
  return { ...state, decision: { verdict: expected.verdict, note: expected.note } };
}
