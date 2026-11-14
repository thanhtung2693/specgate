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

export function sample(version) {
  return {
    review: `sample-review-${version}`,
    completion: `sample-delivery-${version}`,
    criteria: [
      { id: "local-1", outcome: "passed", requirement: "GET /health → 200", symbol: "TestHealthOK", line: 6 },
      { id: "local-2", outcome: version === 2 ? "passed" : "missing", requirement: "DB down → 503", symbol: "TestHealthDBDown", line: 3 },
      { id: "local-3", outcome: "claim", requirement: "No error text in body", line: 3 },
    ],
    source: {
      path: "health.go",
      source: { available: true, lines: health.map((text, i) => ({ number: i + 1, text })) },
      diff: { available: true, text: diff },
    },
  };
}

export const reset = (version = 1) => ({ version, decision: null });

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
