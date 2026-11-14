/* Shared projections keep original work content separate from interface copy. */
(function (root) {
  "use strict";
  const escape = (value) =>
    String(value ?? "").replace(
      /[&<>"']/g,
      (char) =>
        ({
          "&": "&amp;",
          "<": "&lt;",
          ">": "&gt;",
          '"': "&quot;",
          "'": "&#39;",
        })[char],
    );
  function criteria(status, canonical) {
    const evidence = status.criterion_evidence || [];
    const verdicts = status.criteria || [];
    const rows = verdicts.length
      ? verdicts
      : (canonical || []).map((text, i) => ({
          criterion_id: `local-${i + 1}`,
          text,
          verdict: "not_reviewed",
        }));
    return rows.map((row) => ({
      ...(evidence.find((item) => item.id === row.criterion_id) || {}),
      ...row,
    }));
  }
  function provenance(row, lang) {
    const vi = lang === "vi";
    if (row.check_provenance === "selected_test_observed") {
      const passed =
        row.check_status === "pass" &&
        row.selected_cases?.length > 0 &&
        row.selected_cases.every((item) => item.outcome === "passed");
      return passed
        ? {
            kind: "test",
            label: vi ? "Test đã chạy" : "Test observed",
          }
        : {
            kind: "missing",
            label: vi ? "Test cần xem" : "Test needs attention",
          };
    }
    if (row.check_source === "specgate_cli")
      return {
        kind: "command",
        label: vi ? "Lệnh đã chạy" : "Command observed",
      };
    return {
      kind: "claim",
      label: vi ? "Agent báo cáo" : "Agent claim",
    };
  }
  function decisionPins(data) {
    const s = data?.status;
    if (!data?.can_decide || !s || !s.ref || !s.review_id || !s.completion_id)
      return null;
    return {
      ref: s.ref,
      review_id: s.review_id,
      completion_id: s.completion_id,
      ...(s.basis_digest ? { basis_digest: s.basis_digest } : {}),
    };
  }
  function requestGate() {
    let sequence = 0;
    return { start: () => ++sequence, current: (token) => token === sequence };
  }
  const needsHuman = (item) =>
    item.next_actor === "human_reviewer" || item.next_actor === "maintainer";
  function workAction(item) {
    if (item.next_actor === "human_reviewer") {
      if (item.state === "awaiting_acceptance") return "Review delivery";
      if (item.state === "awaiting_review") return "Check evidence";
    }
    if (item.next_actor === "maintainer" && item.state === "blocked")
      return "Inspect blocker";
    return "Inspect work";
  }
  const staleNotice = (item, label = "Review freshness") =>
    item.stale ? [label, item.stale_reason].filter(Boolean).join(" · ") : "";
  function criterionDetails(row, original = {}) {
    const prose =
      original.summary ||
      original.detail ||
      original.explanation ||
      original.note ||
      "";
    const claim =
      typeof original.claim === "string" &&
      !["satisfied", "partial", "not_done", "missing"].includes(original.claim)
        ? original.claim
        : "";
    return {
      report: prose || claim,
      source: [row.evidence_path, row.citation].filter(Boolean).join(" · "),
      check: row.check_name || "",
      tests: (row.selected_cases || [])
        .map((c) => `${c.classname || ""}/${c.name}: ${c.outcome}`)
        .join("\n"),
    };
  }
  function recordedDecision(data) {
    const review = data.review,
      basis = data.status.recorded_acceptance_basis;
    const decision = review ? review.human_decision : basis?.decision;
    if (!["approve", "reject"].includes(decision)) return null;
    const matchingBasis =
      basis && (!review || basis.review_id === review.id) ? basis : null;
    return {
      decision,
      note: review?.note ?? matchingBasis?.note ?? "",
      ref: data.status.ref,
      review_id: review?.id || matchingBasis?.review_id,
      completion_id: review?.report_id || matchingBasis?.completion_id,
      ...(matchingBasis?.actor ? { actor: matchingBasis.actor } : {}),
      ...(matchingBasis?.observed_at
        ? { observed_at: matchingBasis.observed_at }
        : {}),
      ...(matchingBasis?.digest ? { basis_digest: matchingBasis.digest } : {}),
    };
  }
  function appearanceControls(state, labels, themeIcon, adjacentControls = "") {
    return `<div class="language" role="group" aria-label="${escape(labels.language)}">${["en", "vi"].map((lang) => `<button type="button" data-language="${lang}" aria-pressed="${state.lang === lang}"><span>${lang.toUpperCase()}</span></button>`).join("")}</div>${adjacentControls}<button class="icon-button theme-button" data-action="theme" aria-label="${escape(state.theme === "dark" ? labels.light : labels.dark)}" aria-pressed="${state.theme === "dark"}">${themeIcon}</button>`;
  }
  function setAppearance(state, kind, value, persist) {
    if (kind === "language" && ["en", "vi"].includes(value)) state.lang = value;
    else if (kind === "theme" && ["light", "dark"].includes(value))
      state.theme = value;
    else return false;
    persist(`specgate-local-${kind}`, value);
    return true;
  }
  function maintenanceLocation(value) {
    const url = new URL(value),
      openSettings = url.searchParams.get("page") === "tools";
    if (openSettings) url.searchParams.delete("page");
    return { openSettings, url: url.href };
  }
  function reviewPresentation(row, lang) {
    const observed = provenance(row, lang),
      vi = lang === "vi";
    const id = row.criterion_id || row.id || "";
    const match = /^local-(\d+)$/.exec(id);
    const passed = observed.kind === "test" && row.verdict === "met";
    return {
      ...observed,
      id: match ? `AC-${match[1].padStart(2, "0")}` : id,
      tone: passed
        ? "success"
        : observed.kind === "command"
          ? "neutral"
          : "warning",
      label: passed
        ? vi
          ? "Test đạt"
          : "Test passed"
        : observed.kind === "command"
          ? observed.label
          : vi
            ? "Cần xem"
            : "Needs review",
    };
  }
  const api = {
    escape,
    maintenanceLocation,
    appearanceControls,
    setAppearance,
    criteria,
    criterionDetails,
    recordedDecision,
    provenance,
    reviewPresentation,
    decisionPins,
    requestGate,
    needsHuman,
    workAction,
    staleNotice,
  };
  if (typeof module !== "undefined" && module.exports) module.exports = api;
  else root.SpecGateUI = api;
})(globalThis);
