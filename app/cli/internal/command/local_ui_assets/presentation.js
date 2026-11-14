/* Review presentation keeps observation copy and source references separate. */
(function (root, factory) {
  if (typeof module !== "undefined" && module.exports)
    module.exports = factory(require("./core.js"));
  else root.SpecGatePresentation = factory(root.SpecGateUI);
})(globalThis, function (core) {
  "use strict";
  const { escape: e, reviewPresentation, criterionDetails } = core;
  const icons = {
    moon: '<path d="M20.9 13a9 9 0 0 1-9.9-9.9A9 9 0 1 0 20.9 13Z"/>',
    sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5"/>',
    gear: '<path d="m9 3-.6 2.1-2 .9-2-.5-2 3.5 1.5 1.6v2.8L2.4 15l2 3.5 2-.5 2 .9L9 21h4l.6-2.1 2-.9 2 .5 2-3.5-1.5-1.6v-2.8L19.6 9l-2-3.5-2 .5-2-.9L13 3Z"/><circle cx="11" cy="12" r="3"/>',
    refresh:
      '<path d="M20 7v5h-5M4 17v-5h5M6 8a7 7 0 0 1 12-2l2 3M4 15l2 3a7 7 0 0 0 12-2"/>',
    arrow: '<path d="M5 12h14m-5-5 5 5-5 5"/>',
    back: '<path d="M19 12H5m5-5-5 5 5 5"/>',
    check: '<path d="m5 12 4 4L19 6"/>',
    "check-circle": '<circle cx="12" cy="12" r="9"/><path d="m8 12 3 3 5-6"/>',
    clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
    person:
      '<circle cx="12" cy="7" r="4"/><path d="M4 21v-2a8 8 0 0 1 16 0v2Z"/>',
    changes: '<path d="M4 10V4m0 0h6M4 4l4 4a8 8 0 1 1-2 8"/>',
    blocked: '<path d="m9 3-6 6v6l6 6h6l6-6V9l-6-6Z M12 8v5m0 4h.01"/>',
    minus: '<path d="M5 12h14"/>',
    chevron: '<path d="m9 5 7 7-7 7"/>',
    info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v6m0-10h.01"/>',
    code: '<path d="m8 6-6 6 6 6m8-12 6 6-6 6"/>',
    bell: '<path d="M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9m-9 12h6"/>',
    tools:
      '<path d="m14 6 4 4M4 20l9-9m3-8a6 6 0 0 0-5 8L3 19l2 2 8-8a6 6 0 0 0 8-5l-4 4-4-4 4-4Z"/>',
    search: '<circle cx="10" cy="10" r="6"/><path d="m15 15 6 6"/>',
    close: '<path d="m6 6 12 12M6 18 18 6"/>',
    download: '<path d="M12 3v12m-4-4 4 4 4-4M4 17v4h16v-4"/>',
    cache:
      '<rect x="3" y="3" width="18" height="4" rx="1"/><path d="M5 7v14h14V7M9 11h6"/>',
    pin: '<path d="m8 3 8 0-1 7 3 4H6l3-4-1-7M12 14v7"/>',
    file: '<path d="M14 3H5v18h14V8Z M14 3v5h5"/>',
    circle: '<circle cx="12" cy="12" r="9"/>',
    lock: '<rect x="5" y="10" width="14" height="11" rx="2"/><path d="M8 10V6a4 4 0 0 1 8 0v4"/>',
  };
  const icon = (name, modifier = "") =>
    `<svg class="icon ${modifier}" data-icon="${name}" viewBox="0 0 24 24" aria-hidden="true">${icons[name] || icons.arrow}</svg>`;

  const own = (record, key) => record && Object.hasOwn(record, key) ? record[key] : undefined;

  function statusPresentation(item) {
    return (
      own({
        awaiting_acceptance: {
          icon: "clock",
          tone: "warning",
          label: "Awaiting acceptance",
        },
        awaiting_review: {
          icon: "clock",
          tone: "warning",
          label: "Awaiting human review",
        },
        review_pending: {
          icon: "clock",
          tone: "neutral",
          label: "Review pending",
        },
        accepted: { icon: "check-circle", tone: "success", label: "Accepted" },
        implementation: {
          icon: "person",
          tone: "neutral",
          label: "Agent is preparing",
        },
        rework_requested: {
          icon: "changes",
          tone: "warning",
          label: "Changes requested",
        },
        blocked: {
          icon: "blocked",
          tone: "warning",
          label: "Needs maintainer",
        },
      }, item.state) || {
        icon: "minus",
        tone: "neutral",
        label: item.state || "Not reviewed",
      }
    );
  }
  function stateBadge(item, t) {
    const state = statusPresentation(item);
    return `<span class="badge state-badge ${state.tone}">${icon(state.icon)}${e(t(state.label))}</span>`;
  }
  function testObservation(row, lang) {
    const vi = lang === "vi";
    if (row.selected_tests_required && !row.selected_cases?.length)
      return {
        text: vi
          ? "Chưa có kết quả đầy đủ cho test được yêu cầu."
          : "Complete selected-test results are not available.",
        tone: "warning",
      };
    if (row.check_provenance === "selected_test_observed") {
      const cases = row.selected_cases || [];
      if (cases.some((item) => ["failed", "error"].includes(item.outcome)))
        return {
          text: vi
            ? "CLI đã quan sát test được yêu cầu không đạt."
            : "CLI observed the selected test failing.",
          tone: "danger",
        };
      if (cases.some((item) => item.outcome === "skipped"))
        return {
          text: vi
            ? "Test được yêu cầu đã bị bỏ qua."
            : "The selected test was skipped.",
          tone: "warning",
        };
      if (cases.length && cases.every((item) => item.outcome === "passed"))
        return {
          text:
            row.check_status === "pass"
              ? vi
                ? "CLI đã quan sát test được yêu cầu chạy đạt."
                : "CLI observed the selected test passing."
              : vi
                ? "Các test đã quan sát đạt; lệnh kiểm tra chưa đạt."
                : "Observed tests passed; the check did not pass.",
          tone: row.check_status === "pass" ? "success" : "warning",
        };
      return {
        text: vi
          ? "Chưa có kết quả đầy đủ cho test được yêu cầu."
          : "Complete selected-test results are not available.",
        tone: "warning",
      };
    }
    if (row.check_source === "specgate_cli")
      return {
        text:
          row.check_status === "pass"
            ? vi
              ? "CLI đã quan sát lệnh chạy đạt; chưa có test riêng."
              : "CLI observed a passing command; no selected test."
            : vi
              ? "Lệnh được CLI quan sát chưa đạt; chưa có test riêng."
              : "The CLI-observed command did not pass; no selected test.",
        tone: row.check_status === "pass" ? "warning" : "danger",
      };
    return {
      text: vi
        ? "Chưa có test riêng cho tiêu chí này."
        : "No selected test for this criterion.",
      tone: "warning",
    };
  }
  function citationLabel(citation, lang) {
    const line = /^line: ([1-9][0-9]*)$/.exec(citation || "");
    return line
      ? `${lang === "vi" ? "Dòng" : "Line"} ${line[1]}`
      : citation || "";
  }
  const claimLabel = (claim, t) => t(own({
    satisfied: "Agent reports met",
    partial: "Agent reports partial",
    not_done: "Agent reports not done",
  }, claim) || "No agent report");
  const checkStatusLabel = (status, t) => {
    const label = own({ pass: "Passed", fail: "Failed", skipped: "Skipped" }, status);
    return label ? t(label) : status || t("Unknown");
  };

  function criterionView(row, reports, lang, t) {
    const p = reviewPresentation(row, lang),
      original = reports.find((item) => item.criterion_id === row.criterion_id),
      detail = criterionDetails(row, original),
      observation = testObservation(row, lang);
    const claim = claimLabel(row.claim, t);
    const hasGap = Boolean(row.gaps?.length);
    const badge = hasGap ? t("Evidence gap") : p.kind === "claim" ? t("Agent claim") : p.label;
    const tone = hasGap ? "warning" : p.kind === "claim" ? "neutral" : p.tone;
    const observationLabel = p.kind === "claim" && !row.selected_tests_required ? "" : ` · ${observation.text}`;
    const gaps = hasGap ? `<ul>${row.gaps.map((gap) => `<li>${e(gap)}</li>`).join("")}</ul>` : "";
    return `<details class="criterion" data-criterion="${e(row.criterion_id || row.id)}"><summary>${icon("chevron", "chevron")}<span class="result-mark ${tone}">${icon(tone === "success" ? "check-circle" : "circle")}</span><code class="criterion-id">${e(p.id)}</code><strong>${e(row.text)}<small class="criterion-observation">${e(claim + observationLabel)}</small></strong><span class="badge ${tone}">${e(badge)}</span></summary><div class="criterion-body">${gaps ? `<div class="warning"><strong>${e(t("Evidence gaps"))}</strong>${gaps}</div>` : ""}<div class="evidence-columns"><section class="agent-report"><h3>${e(t("Agent report"))}</h3><p>${e(detail.report || claim)}</p></section><section class="test-result"><h3>${e(t("Test result"))}</h3><p class="observation ${observation.tone}-text">${e(observation.text)}</p>${detail.check ? `<code>${e(detail.check)}</code>` : ""}${detail.tests ? `<p class="mono">${e(detail.tests)}</p>` : ""}${row.check_name && row.why ? `<p>${e(row.why)}</p>` : ""}</section></div>${detail.source ? `<div class="source-reference"><div>${icon("file")}${e(t("Cited source"))}</div><div class="source-location"><code>${e(row.evidence_path)}</code>${row.citation ? `<span>${e(citationLabel(row.citation, lang))}</span>` : ""}</div><div class="source-excerpt" data-source-slot></div></div>` : ""}<details class="raw-details"><summary>${icon("chevron", "chevron")}${e(t("View original data"))}</summary><pre>${e(JSON.stringify({ review: row, ...(original ? { original_report: original } : {}) }, null, 2))}</pre></details></div></details>`;
  }

  function reviewChecks(completion, t) {
    const checks = completion?.checks || [];
    const report = completion?.summary ? `<p class="evidence-text">${e(completion.summary)}</p>` : "";
    if (!report && !checks.length) return "";
    return `<details class="evidence-disclosure"><summary>${icon("chevron", "chevron")}${e(t("Delivery report and checks"))} <span class="muted">· ${checks.length} ${e(t("checks"))}</span></summary>${report}${checks.length ? `<p class="muted">${e(t("Check results cover the commands and selected tests shown here. Review the reported scope and limits."))}</p>` : ""}${checks.map((check) => {
      const cases = check.selected_cases || [];
      const source = check.source !== "specgate_cli" ? t("Agent claim") : t(cases.length ? "Selected tests observed" : "Command observed");
      const tests = cases.length ? `<ul>${cases.map((item) => `<li><code>${e(`${item.classname}/${item.name}: ${item.outcome}`)}</code></li>`).join("")}</ul>` : check.selected_tests_required ? `<p>${e(t("Complete selected-test results are not available."))}</p>` : "";
      return `<details class="evidence-check"><summary>${icon("chevron", "chevron")}<strong>${e(check.name || t("Unnamed check"))}</strong><span class="muted">${e(checkStatusLabel(check.status, t))} · ${e(source)}</span></summary>${check.command ? `<pre class="evidence-text"><code>${e(check.command)}</code></pre>` : ""}${check.cwd ? `<p>${e(t("Working directory"))} <code>${e(check.cwd)}</code></p>` : ""}${check.claimed_status ? `<p>${e(t("Agent-reported status"))} <code>${e(check.claimed_status)}</code></p>` : ""}${check.detail ? `<p class="evidence-text">${e(check.detail)}</p>` : ""}${check.run_freshness ? `<p>${e(t("Verification endpoints"))} <code>${e(check.run_freshness)}</code></p>` : ""}${tests}</details>`;
    }).join("")}</details>`;
  }

  function comparisonPair(title, before, after, t) {
    return `<div class="evidence-change"><h3>${title}</h3><dl class="kv"><div><dt>${e(t("Previous"))}</dt><dd>${before}</dd></div><div><dt>${e(t("Current"))}</dt><dd>${after}</dd></div></dl></div>`;
  }

  function comparisonCriterion(row, lang, t) {
    if (!row) return e(t("No evidence recorded"));
    const source = [row.evidence_path, citationLabel(row.citation, lang)].filter(Boolean).join(" · ");
    const report = row.gaps?.length ? row.gaps.join("\n") : criterionDetails(row, row).report;
    const checkProvenance = own({ agent_attested: "Agent claim", command_only: "Command observed", selected_test_observed: "Selected tests observed" }, row.check_provenance);
    const check = row.check_name ? [row.check_name, row.check_status ? checkStatusLabel(row.check_status, t) : t("No check recorded"), checkProvenance && t(checkProvenance)].filter(Boolean).join(" · ") : "";
    const tests = row.selected_cases?.length ? testObservation(row, lang).text : "";
    return [claimLabel(row.claim, t), check, tests, source, report].filter(Boolean).map((item) => `<p class="evidence-text">${e(item)}</p>`).join("");
  }

  function comparisonChecks(checks, t) {
    if (!Array.isArray(checks) || !checks.length) return e(t("No check recorded"));
    return checks.map((check) => {
      const source = check.source === "specgate_cli" ? t(check.selected_cases?.length ? "Selected tests observed" : "Command observed") : t("Agent claim");
      const cases = (check.selected_cases || []).map((item) => `${item.classname}/${item.name}: ${item.outcome}`).join("\n");
      return `<p class="evidence-text">${e(`${checkStatusLabel(check.status, t)} · ${source}`)}${check.command ? `<br><code>${e(check.command)}</code>` : ""}${check.cwd ? `<br><code>${e(check.cwd)}</code>` : ""}${check.claimed_status ? `<br>${e(t("Agent-reported status"))} <code>${e(checkStatusLabel(check.claimed_status, t))}</code>` : ""}${check.run_freshness ? `<br>${e(t("Verification endpoints"))} <code>${e(check.run_freshness)}</code>` : ""}${check.detail ? `<br>${e(check.detail)}` : ""}${cases ? `<br><code>${e(cases)}</code>` : ""}</p>`;
    }).join("");
  }

  function reviewComparison(comparison, t, { rows = [], completion = {}, lang = "en" } = {}) {
    if (!comparison) return "";
    const { state, prior, summary_changed, changed_criteria = [], changed_checks = [] } = comparison;
    if (state === "no_baseline") return `<p class="muted">${e(t("No earlier human decision to compare."))}</p>`;
    const identities = `<dl class="kv">${[
      ["Previous review ID", prior?.review_id], ["Current review ID", comparison.review_id],
      ["Previous completion ID", prior?.completion_id], ["Current completion ID", comparison.completion_id],
      ["Previous evidence verdict", prior?.verdict],
    ].filter(([, value]) => value).map(([label, value]) => `<div><dt>${e(t(label))}</dt><dd>${e(value)}</dd></div>`).join("")}</dl>`;
    const changed = [
      ...changed_criteria.map((id) => {
        const row = rows.find((item) => (item.criterion_id || item.id) === id);
        const title = row ? `${reviewPresentation(row, lang).id} · ${row.text}` : id;
        const original = completion.criteria?.find((item) => item.criterion_id === id);
        const earlier = own(comparison.prior_criteria, id);
        const before = comparisonCriterion(earlier, lang, t);
        const after = comparisonCriterion(row && { ...row, summary: criterionDetails(row, original).report }, lang, t);
        const changedSource = earlier?.grounding_digest && original?.evidence?.grounding?.digest && earlier.grounding_digest !== original.evidence.grounding.digest;
        const note = changedSource ? t("Recorded source content changed.") : before === after ? t("Other recorded criterion evidence changed. See the evidence details.") : "";
        return comparisonPair(e(title), before, `${after}${note ? `<p class="muted">${e(note)}</p>` : ""}`, t);
      }),
      ...changed_checks.map((name) => comparisonPair(`<code>${e(name || t("Unnamed check"))}</code>`, comparisonChecks(own(comparison.prior_checks, name), t), comparisonChecks(completion.checks?.filter((check) => check.name === name), t), t)),
      ...(summary_changed ? [comparisonPair(e(t("Delivery summary changed")), `<p class="evidence-text">${e(prior?.summary || t("No summary recorded"))}</p>`, `<p class="evidence-text">${e(completion.summary || t("No summary recorded"))}</p>`, t)] : []),
    ].join("");
    const outcome = state !== "available" ? t("Prior evidence is unavailable.") : changed ? t("Recorded evidence changed") : t("No material changes to recorded evidence");
    const previousData = {
      ...(summary_changed ? { summary: prior?.summary || "" } : {}),
      ...(comparison.prior_criteria ? { criteria: comparison.prior_criteria } : {}),
      ...(comparison.prior_checks ? { checks: comparison.prior_checks } : {}),
    };
    const decision = prior?.decision ? `<p><strong>${e(t("Previous human decision"))}</strong> · ${e(t(prior.decision === "reject" ? "Rejected" : "Accepted"))}</p>` : "";
    return `<details class="evidence-disclosure"><summary>${icon("chevron", "chevron")}${e(t("Since the last human review"))} <span class="muted">· ${e(outcome)}</span></summary>${decision}${prior?.note ? `<blockquote class="recorded-note evidence-text">${e(prior.note)}</blockquote>` : ""}${changed}<details class="raw-details"><summary>${icon("chevron", "chevron")}${e(t("Previous evidence details"))}</summary>${identities}${Object.keys(previousData).length ? `<pre>${e(JSON.stringify(previousData, null, 2))}</pre>` : ""}</details></details>`;
  }

  function evidenceReview(rows, work, lang, t) {
    const required = new Set(work.completion?.selected_tests_required);
    rows = rows.map((row) => ({ ...row, selected_tests_required: required.has(row.criterion_id || row.id) }));
    const gaps = rows.filter((row) => row.gaps?.length).length;
    return `${gaps ? `<p class="warning">${icon("info")}${e(t("Criteria with evidence gaps"))} · ${gaps}</p>` : ""}<div class="section-head"><h2>${e(t("Acceptance criteria"))}</h2><span class="muted">${rows.length} ${e(t("criteria"))}</span></div>${rows.length ? `<div class="criteria">${rows.map((row) => criterionView(row, work.completion?.criteria || [], lang, t)).join("")}</div>` : `<section class="empty">${e(t("No criterion report yet."))}</section>`}<div class="evidence-disclosures">${reviewChecks(work.completion, t)}${reviewComparison(work.review_comparison, t, { rows, completion: work.completion, lang })}</div>`;
  }
  return {
    icon,
    statusPresentation,
    stateBadge,
    testObservation,
    citationLabel,
    criterionView,
    reviewChecks,
    reviewComparison,
    evidenceReview,
  };
});
