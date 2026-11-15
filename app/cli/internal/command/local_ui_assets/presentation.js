/* Review presentation keeps observation copy and source references separate. */
(function (root, factory) {
  if (typeof module !== "undefined" && module.exports)
    module.exports = factory(require("./core.js"));
  else root.SpecGatePresentation = factory(root.SpecGateUI);
})(globalThis, function (core) {
  "use strict";
  const { escape: e, reviewPresentation, criterionDetails, decisionPins, recordedDecision } = core;
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
    "chevron-left": '<path d="m15 5-7 7 7 7"/>',
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
    inbox: '<path d="M4 5h16l2 9v5H2v-5Zm-2 9h6l2 2h4l2-2h6"/>',
    folder: '<path d="M3 6h7l2 3h9v10H3Z"/>',
    evidence: '<rect x="4" y="3" width="16" height="18" rx="2"/><path d="m8 12 2 2 5-5m-7 8h7"/>',
    terminal: '<rect x="2" y="4" width="20" height="16" rx="2"/><path d="m6 8 4 4-4 4m7 0h4"/>',
    quote: '<path d="M3 8h6v5l-3 4m9-9h6v5l-3 4M3 8v6h6m6-6v6h6"/>',
    lock: '<rect x="5" y="10" width="14" height="11" rx="2"/><path d="M8 10V6a4 4 0 0 1 8 0v4"/>',
  };
  const icon = (name, modifier = "") =>
    `<svg class="icon ${modifier}" data-icon="${name}" viewBox="0 0 24 24" aria-hidden="true">${icons[name] || icons.arrow}</svg>`;

  const own = (record, key) => record && Object.hasOwn(record, key) ? record[key] : undefined;

  function statusPresentation(item) {
    return (
      own({
        awaiting_acceptance: {
          icon: "person",
          tone: "accent",
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
                ? "Các test đã quan sát đạt. Kết quả kiểm tra chung chưa đạt."
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
              ? "CLI ghi nhận kiểm tra đạt. Chưa gắn test riêng."
              : "CLI observed a passing check. No selected test attached."
            : row.check_status === "skipped"
              ? vi ? "Đã bỏ qua kiểm tra. Chưa gắn test riêng." : "The check was skipped. No selected test attached."
              : vi ? "CLI ghi nhận kiểm tra chưa đạt. Chưa gắn test riêng." : "The CLI-observed check did not pass. No selected test attached.",
        tone: row.check_status === "fail" ? "danger" : "neutral",
      };
    return {
      text: vi
        ? "Chưa gắn test riêng cho tiêu chí này."
        : "No selected test attached to this criterion.",
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

  function criterionSummary(row, lang, t) {
    const p = reviewPresentation(row, lang);
    const observation = testObservation(row, lang);
    const hasGap = Boolean(row.gaps?.length);
    const observedCheck = row.check_source === "specgate_cli";
    const observedTests = row.check_provenance === "selected_test_observed" && Boolean(row.selected_cases?.length);
    const checkResult = own({ pass: "Check passed", fail: "Check failed", skipped: "Check skipped" }, row.check_status);
    const label = hasGap && !observedCheck ? t("Evidence gap")
      : p.kind === "test" && row.verdict === "met" ? p.label
      : row.selected_tests_required || observedTests ? t("Test needs attention")
      : observedCheck ? t(checkResult || "Observed check") : t(row.claim ? "Agent claim" : "Not reviewed");
    const tone = observation.tone === "danger" ? "danger" : hasGap || row.selected_tests_required && !observedTests ? "warning" : observedTests ? p.tone : "neutral";
    return { ...p, label, tone, observation };
  }

  const lineCited = (citation) => /^lines?:\s*\d/i.test(citation || "");
  const tested = (row) => row.check_provenance === "selected_test_observed" && Boolean(row.selected_cases?.length);

  /* Four rungs of evidence. Only CLI-observed checks and selected-test results count as observed. */
  function rungs(row) {
    const cases = row.selected_cases || [];
    const observed = row.check_source === "specgate_cli";
    const claim = row.claim === "satisfied" ? "said" : row.claim === "partial" || row.claim === "not_done" ? "gap" : "none";
    const source = row.evidence_path && lineCited(row.citation) ? "said" : "none";
    const check = !observed ? "none" : own({ pass: "ok", fail: "bad", skipped: "gap" }, row.check_status) || "none";
    const test = tested(row)
      ? cases.some((item) => ["failed", "error"].includes(item.outcome)) ? "bad"
        : cases.every((item) => item.outcome === "passed") ? "ok" : "gap"
      : row.selected_tests_required ? "gap" : "none";
    const bad = check === "bad" || test === "bad";
    const problem = bad ? "bad" : row.gaps?.length || claim === "gap" || check === "gap" || test === "gap" ? "gap" : "";
    return { claim, source, check, test, problem };
  }
  const rungNames = ["Claim", "Source", "Check", "Test"];

  function rungStates(row, lang, t) {
    const r = rungs(row), observation = testObservation(row, lang), cases = row.selected_cases || [];
    const attested = row.check_source !== "specgate_cli" && row.check_status;
    return {
      Claim: [r.claim, claimLabel(row.claim, t)],
      Source: [r.source, row.evidence_path ? `${row.evidence_path} · ${row.citation ? citationLabel(row.citation, lang) : t("No line cited")}` : t("No cited source")],
      Check: [r.check, r.check !== "none" ? `${t("Observed check")} · ${checkStatusLabel(row.check_status, t)}` : attested ? `${t("Agent claim")} · ${checkStatusLabel(row.check_status, t)}` : t("No CLI-observed check")],
      Test: [r.test, cases.length || row.selected_tests_required ? observation.text : t("No selected test")],
    };
  }

  function ladder(r) {
    const tones = [r.claim, r.source, r.check, r.test];
    return `<span class="ladder" aria-hidden="true">${tones.map((tone, i) => `${i ? `<i class="l ${tone}"></i>` : ""}<i class="n ${tone}"></i>`).join("")}</span>`;
  }

  function criteriaList(rows, selectedId, lang, t) {
    return `<ol class="crit-list" aria-label="${e(t("Acceptance criteria"))}">${rows.map((row) => {
      const id = row.criterion_id || row.id, r = rungs(row), summary = criterionSummary(row, lang, t);
      const selected = id === selectedId, shown = selected || r.problem;
      const label = r.problem === "bad" ? summary.label : row.gaps?.length ? t("Evidence gap") : r.claim === "gap" ? claimLabel(row.claim, t) : summary.label;
      const states = rungStates(row, lang, t);
      const spoken = rungNames.map((name) => `${t(name)}: ${states[name][1]}`).join(". ");
      return `<li><button type="button" class="crit${selected ? " sel" : ""}${r.problem ? " " + r.problem : ""}" data-criterion-select="${e(id)}"${selected ? ' aria-current="true"' : ""}><code>${e(summary.id)}</code><span class="tx">${e(row.text)}</span><span class="lad">${ladder(r)}<span class="res${shown ? "" : " visually-hidden"}">${e(label)}</span></span>${selected ? `<span class="names" aria-hidden="true">${rungNames.map((name) => `<span>${e(t(name))}</span>`).join("")}</span>` : ""}<span class="visually-hidden">${e(spoken)}</span></button></li>`;
    }).join("")}</ol>`;
  }

  function reviewSummary(rows, t) {
    if (!rows.length) return null;
    const problems = rows.filter((row) => rungs(row).problem), observed = rows.filter((row) => rungs(row).check !== "none" || rungs(row).test !== "none");
    const fill = (text, n) => t(text).replace("{n}", n).replace("{total}", rows.length);
    if (problems.length) return { tone: problems.some((row) => rungs(row).problem === "bad") ? "bad" : "gap", text: fill("{n} of {total} criteria need attention", problems.length) };
    if (!observed.length) return { tone: "none", text: t("Agent claims only. No criterion has a CLI-observed result.") };
    return { tone: "ok", text: fill("{n} of {total} criteria have CLI-observed results", observed.length) };
  }

  function evidencePane(row, reports, lang, t) {
    const id = row.criterion_id || row.id,
      original = (reports || []).find((item) => item.criterion_id === id),
      detail = criterionDetails(row, original),
      states = rungStates(row, lang, t),
      cases = row.selected_cases || [],
      attested = row.check_source !== "specgate_cli" && row.check_status;
    const cell = (name, glyph, body) => `<div class="${states[name][0]}"><dt>${e(t(name))}</dt><dd>${icon(glyph)}${body}</dd></div>`;
    const gaps = row.gaps?.length ? `<div class="gaps"><h3>${e(t("Evidence gaps"))}</h3><ul>${row.gaps.map((gap) => `<li>${e(gap)}</li>`).join("")}</ul></div>` : "";
    const source = row.evidence_path ? `<code>${e(row.evidence_path)}</code><span>${e(row.citation ? citationLabel(row.citation, lang) : t("No line cited"))}</span>` : `<span>${e(states.Source[1])}</span>`;
    const check = states.Check[0] !== "none" ? `<span>${e(states.Check[1])}</span>` : attested ? `<q>${e(checkStatusLabel(row.check_status, t))}</q><span>${e(t("Agent claim"))} · ${e(t("not observed"))}</span>` : `<span>${e(states.Check[1])}</span>`;
    const test = `<span>${e(states.Test[1])}</span>${cases.length ? cases.map((item) => `<code>${e(`${item.classname || ""}/${item.name}: ${item.outcome}`)}</code>`).join("") : ""}`;
    return `<div class="evidence" data-source-pane data-criterion="${e(id)}"><code class="id">${e(reviewPresentation(row, lang).id)}</code><h2>${e(row.text)}</h2>${gaps}<dl class="rr">${cell("Claim", "quote", `${detail.report ? `<q>${e(detail.report)}</q>` : ""}<span>${e(states.Claim[1])}</span>`)}${cell("Source", "code", source)}${cell("Check", "terminal", `${check}${row.check_name ? `<code>${e(row.check_name)}</code>` : ""}${row.check_name && row.why ? `<span>${e(row.why)}</span>` : ""}`)}${cell("Test", "evidence", test)}</dl>${row.evidence_path ? `<div class="source-reference"><h3>${e(t("Cited source"))}</h3><div class="source-inspector"><div class="source-excerpt" data-source-slot></div></div></div>` : ""}<details class="raw-details"><summary>${icon("chevron", "chevron")}${e(t("View original data"))}</summary><pre tabindex="0">${e(JSON.stringify({ review: row, ...(original ? { original_report: original } : {}) }, null, 2))}</pre></details></div>`;
  }

  function ageLabel(iso, lang, now = Date.now()) {
    const time = Date.parse(iso);
    if (!Number.isFinite(time)) return null;
    const seconds = (time - now) / 1000;
    const [unit, size] = [["year", 31536000], ["month", 2592000], ["week", 604800], ["day", 86400], ["hour", 3600], ["minute", 60]].find(([, size]) => Math.abs(seconds) >= size) || ["second", 1];
    return {
      text: new Intl.RelativeTimeFormat(lang, { numeric: "auto", style: "narrow" }).format(Math.trunc(seconds / size), unit),
      date: new Date(time).toLocaleString(lang, { dateStyle: "medium", timeStyle: "short" }),
    };
  }

  function reviewChecks(completion, t) {
    const checks = completion?.checks || [];
    const report = completion?.summary ? `<p class="evidence-text">${e(completion.summary)}</p>` : "";
    if (!report && !checks.length) return "";
    return `<details class="evidence-disclosure"><summary>${icon("chevron", "chevron")}${e(t("Delivery report and checks"))} <span class="muted">· ${checks.length} ${e(t("checks"))}</span></summary>${report}${checks.length ? `<p class="muted">${e(t("Check results cover the commands and selected tests shown here. Review the reported scope and limits."))}</p>` : ""}${checks.map((check) => {
      const cases = check.selected_cases || [];
      const source = check.source !== "specgate_cli" ? t("Agent claim") : t(cases.length ? "Selected tests observed" : "Observed check");
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
    const checkProvenance = own({ agent_attested: "Agent claim", command_only: "Observed check", selected_test_observed: "Selected tests observed" }, row.check_provenance);
    const check = row.check_name ? [row.check_name, row.check_status ? checkStatusLabel(row.check_status, t) : t("No check recorded"), checkProvenance && t(checkProvenance)].filter(Boolean).join(" · ") : "";
    const tests = row.selected_cases?.length ? testObservation(row, lang).text : "";
    return [claimLabel(row.claim, t), check, tests, source, report].filter(Boolean).map((item) => `<p class="evidence-text">${e(item)}</p>`).join("");
  }

  function comparisonChecks(checks, t) {
    if (!Array.isArray(checks) || !checks.length) return e(t("No check recorded"));
    return checks.map((check) => {
      const source = check.source === "specgate_cli" ? t(check.selected_cases?.length ? "Selected tests observed" : "Observed check") : t("Agent claim");
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

  function withRequiredTests(rows, work) {
    const required = new Set(work.completion?.selected_tests_required);
    return rows.map((row) => ({ ...row, selected_tests_required: required.has(row.criterion_id || row.id) }));
  }

  const kv = (values, t) =>
    `<dl class="kv">${values.map(([name, value]) => `<div><dt>${e(t(name))}</dt><dd>${e(value || "—")}</dd></div>`).join("")}</dl>`;

  function technicalView(s, t) {
    return `<details class="version-details"><summary>${icon("info")}${e(t("Version details"))}</summary>${kv(
      [
        ["State", s.state],
        ["Evidence readiness", s.evidence],
        ["Assurance", s.assurance],
        ["Decision", s.decision],
        ["Receipt", s.receipt],
        ["Changes since verification", s.freshness],
        ["Next actor", s.next_actor],
        ["Context digest", s.context_digest],
        ["Review ID", s.review_id],
        ["Completion ID", s.completion_id],
        ["Current evidence basis", s.basis_digest],
        ["Evidence gaps", (s.missing || []).join("\n")],
        ["Guidance", s.guidance],
      ], t,
    )}${s.next_command ? `<p>${e(t("Continue in your terminal"))}</p><pre>${e(s.next_command)}</pre>` : ""}</details>`;
  }

  function decisionDock(d, note, actor, t) {
    const s = d.status,
      pins = decisionPins(d),
      recorded = recordedDecision(d);
    if (recorded)
      return `<div class="dock done">${icon("check-circle")}<p><strong>${e(t("Recorded decision"))}</strong> · ${e(t(recorded.decision === "approve" ? "Accepted" : "Rejected"))} · ${e(actor(s.next_actor))}${recorded.observed_at ? ` · ${e(recorded.observed_at)}` : ""}${recorded.note ? `<q tabindex="0">${e(recorded.note)}</q>` : ""}</p></div>`;
    if (!pins)
      return `<div class="dock done">${icon("lock")}<p><strong>${e(t("Decision unavailable for this work."))}</strong> ${e(t(s.decision))}</p></div>`;
    return `<div class="dock"><textarea id="decision-note" maxlength="20000" aria-label="${e(t("Your note"))}" aria-describedby="note-error" placeholder="${e(t("Note · required to ask for changes"))}">${e(note)}</textarea><button type="button" class="button" data-action="request-changes">${e(t("Ask for changes"))}</button><button type="button" class="button primary" data-action="accept">${icon("check")}${e(t("Accept delivery"))}</button><p id="note-error" class="hint" role="alert"></p></div>`;
  }

  function contextView(d, doc, t, renderContext, button) {
    const docs = d.documents || [];
    let body = `<p class="busy" role="status">${e(t("Loading approved context…"))}</p>`;
    if (doc?.error)
      body = `<p role="alert" class="warning">${e(doc.error)}</p>`;
    else if (doc?.content !== undefined) {
      let content = `<pre class="context-raw"><code>${e(doc.content)}</code></pre>`;
      if (!doc.showSource) {
        const rendered = renderContext(doc.content, {
          generatedQuick:
            doc.selected < 0 &&
            !d.work.artifact_id &&
            doc.path === "context.md" &&
            doc.role === "context",
          workRef: d.status.ref,
        });
        content = `<div class="markdown-body">${rendered.html}</div><details class="technical-context"><summary>${icon("chevron", "chevron")}${e(t("Technical details"))}<span>${e(t("Policy and version identifiers"))}</span></summary>${rendered.technicalHTML ? `<div class="markdown-body">${rendered.technicalHTML}</div>` : ""}<code>${e(doc.digest)}</code></details>`;
      }
      body = `<article class="context-reader"><div class="document-meta"><div><code>${e(doc.path)}</code>${doc.selected >= 0 ? `<span>${e(t("Role"))}: ${e(doc.role)}</span>` : ""}</div>${button("context-source", doc.showSource ? "Formatted view" : "Original source", false, 'aria-controls="context-content"')}</div><div id="context-content">${content}</div></article>`;
    }
    return `<div class="scope-reader"><div><h2 class="eyebrow">${e(t("Approved documents"))}</h2><div class="document-list"><button data-document="-1" class="${(doc?.selected ?? -1) < 0 ? "active" : ""}" aria-pressed="${(doc?.selected ?? -1) < 0}">${icon("file")}<span>${e(t("Context Pack"))}</span></button>${docs.map((item, i) => `<button data-document="${i}" class="${doc?.selected === i ? "active" : ""}" aria-pressed="${doc?.selected === i}">${icon("file")}<code>${e(item.path)}</code><small>${e(item.role)}</small></button>`).join("")}</div><p class="field-hint">${e(t("Original content is shown without translation."))}</p></div>${body}</div>`;
  }

  const drawerFrame = (heading, closeLabel, body) =>
    `<header><h2 id="scope-title">${icon("file")}${e(heading)}</h2><button type="button" class="icon-button" data-close-scope aria-label="${e(closeLabel)}">${icon("close")}</button></header><div class="drawer-body">${body}</div>`;

  const decisionConfirmation = (d, pins, note, t) =>
    `<p>${e(t("Record this decision for the exact review shown below."))}</p>${kv([
      ["Work", d.work.title],
      ["Review ID", pins.review_id],
      ["Completion ID", pins.completion_id],
      ["Current evidence basis", pins.basis_digest],
      ["Your note", note],
    ], t)}<p>${e(t("If the review or evidence changed, SpecGate will refuse this decision. Refresh and inspect it again."))}</p>`;

  const updateReview = (version, preview, t) =>
    `${kv([["Running version", version], ["Available version", preview.latest_version], ["Install target", preview.target]], t)}<ul>${(preview.scope || []).map((line) => `<li>${e(t(line))}</li>`).join("")}</ul><p>${e(t("The running UI keeps its current version until restarted. Finish any open decision before updating."))}</p>`;

  const selectedEvidence = (rows, work, id, lang, t) => {
    const row = rows.find((item) => (item.criterion_id || item.id) === id);
    return row ? evidencePane(row, work.completion?.criteria, lang, t) : `<p class="muted">${e(t("No acceptance criteria recorded."))}</p>`;
  };

  const attention = (item, t) =>
    item.stale ? core.staleNotice(item, t("Review freshness"))
      : item.state === "rework_requested" ? t("Changes requested")
      : item.next_actor === "maintainer" ? t("Needs maintainer")
      : "";

  function queueRow(item, lang, t, href) {
    const status = statusPresentation(item), why = attention(item, t), age = item.created_at && ageLabel(item.created_at, lang);
    const created = age && `${t("Created")} ${age.date}`;
    return `<li><a class="row" href="${e(href(item.ref))}" data-nav><span class="mark ${status.tone}">${icon(status.icon)}<span class="visually-hidden">${e(t(status.label))}</span></span><strong class="t" data-title-ref="${e(item.ref)}">${e(item.title)}</strong>${why ? `<span class="why">${e(why)}</span>` : ""}${age ? `<time class="age" datetime="${e(item.created_at)}" title="${e(created)}"><span aria-hidden="true">${e(age.text)}</span><span class="visually-hidden">${e(created)}</span></time>` : ""}</a></li>`;
  }

  function queueList(items, { items: all, filter, search, lang }, t, href) {
    const count = `<p class="list-count">${e(t("Showing {shown} of {total} work items").replace("{shown}", items.length).replace("{total}", all.length))}</p>`;
    return items.length
      ? `<ul class="rows">${items.map((item) => queueRow(item, lang, t, href)).join("")}</ul>${count}`
      : `<section class="empty">${icon("inbox")}<h2>${e(t(!search && filter === "human" ? "No decisions waiting." : "No work matches this view."))}</h2><p>${e(t(!all.length ? "Work preparation and approval start with your agent or CLI." : "Try another filter or search."))}</p></section>${count}`;
  }

  function queueResults(state, t, href) {
    if (state.queueError) return queueError(state.queueError, t);
    const search = state.search.toLocaleLowerCase();
    const items = state.items.filter((item) =>
      (state.filter === "all" || (state.filter === "human" ? core.needsHuman(item) : item.next_actor === "implementing_agent")) &&
      `${item.title} ${item.ref}`.toLocaleLowerCase().includes(search));
    return queueList(items, state, t, href);
  }

  function cacheBody(preview, t) {
    const entries = preview.entries || [];
    return `<p>${e(t(preview.scope))}</p>${entries.length ? `<ul class="file-list">${entries.map((file) => `<li><strong>${e(file.name)}</strong><code>${e(file.path)}</code><span>${e(file.bytes)} bytes</span></li>`).join("")}</ul><p>${e(t("Total"))}: ${e(preview.count)} ${e(t("Files"))} · ${e(preview.bytes)} bytes</p>` : `<p>${e(t(preview.reason || "No eligible cache files."))}</p>`}<p>${e(t("Work, specs, evidence, acceptance records and backups are kept."))}</p>`;
  }

  function reviewScreen(d, rows, { selected, enter, note, lang, t, actor }) {
    const s = d.status, status = statusPresentation(s), summary = reviewSummary(rows, t);
    const shortDate = d.review?.created_at && new Date(d.review.created_at).toLocaleDateString(lang, { dateStyle: "medium" });
    const meta = [e(t(status.label)), shortDate && e(shortDate), d.completion?.agent?.name && e(d.completion.agent.name), s.completion_id ? "" : e(t("No delivery yet."))].filter(Boolean).join(" · ");
    const sumIcon = summary && icon(summary.tone === "ok" ? "check-circle" : summary.tone === "none" ? "quote" : "blocked");
    return `<div class="review${enter ? " enter" : ""}"><div id="delivery-notice" role="status" aria-live="polite"></div><section class="list" aria-label="${e(t("Acceptance criteria"))}"><h1 data-title-ref="${e(s.ref)}">${e(d.work.title)}</h1>${summary ? `<p class="sum ${summary.tone}">${sumIcon}${e(summary.text)}</p>` : ""}<p class="meta">${icon(status.icon)}<span>${meta}</span></p>${s.historical_verification ? `<p class="warning">${e(t("Recorded verification comes from another Local store generation."))}</p>` : ""}${s.stale ? `<p class="warning">${e(core.staleNotice(s, t("Review freshness")))}</p>` : ""}<div id="crit-list">${criteriaList(rows, selected, lang, t)}</div><details class="more"><summary>${icon("chevron", "chevron")}${e(t("Delivery details"))}</summary>${reviewChecks(d.completion, t)}${reviewComparison(d.review_comparison, t, { rows, completion: d.completion, lang })}${technicalView(s, t)}</details></section><section class="detail" id="evidence-pane">${selectedEvidence(rows, d, selected, lang, t)}</section>${decisionDock(d, note, actor, t)}</div>`;
  }

  const queueTitle = (filter, t) => t({ human: "Waiting on you", agent: "Agent’s next step", all: "All work" }[filter]);

  function queueScreen({ filter, search }, counts, results, t) {
    const tabs = ["human", "agent", "all"].map((key, i) => `<button type="button" data-filter="${key}" aria-pressed="${filter === key}">${e(queueTitle(key, t))}<span>${counts[i]}</span></button>`).join("");
    const label = e(t("Search work or reference"));
    return `<div class="q"><header class="queue-head"><h1 class="visually-hidden">${e(queueTitle(filter, t))}</h1><div class="filters tabs" role="group" aria-label="${e(t("Filter work"))}">${tabs}</div><input class="search" id="search" type="search" value="${e(search)}" aria-label="${label}" placeholder="${label}"></header><div id="queue-results">${results}</div></div>`;
  }

  /* The one full-width bar: brand and version, then the queue or review context, then the controls. */
  function topBar({ t, href, lang, theme, notifications, settings, version, workspace, ref, scopeHref, back = true, waiting = [], home = "/", logo = "/assets/logo.svg" }) {
    const brand = `<a class="brand" href="${e(home)}" data-nav aria-label="SpecGate"><img src="${e(logo)}" alt="" width="26" height="26"><span class="wordmark">SpecGate</span>${version ? ` <span class="version" title="${e(t("Running version"))}">${e(version)}</span>` : ""}</a>`;
    const at = waiting.indexOf(ref);
    const step = (to, label, glyph) => `<a class="ibtn"${to ? ` href="${e(href(to))}" data-nav` : ' aria-disabled="true"'} aria-label="${e(t(label))}">${icon(glyph)}</a>`;
    const pager = at < 0 ? "" : `<span class="pager"><span class="pos">${at + 1} / ${waiting.length}</span>${step(waiting[at - 1], "Previous work", "chevron-left")}${step(waiting[at + 1], "Next work", "chevron")}</span>`;
    const context = ref
      ? `${back ? `<a class="back" href="/" data-nav>${icon("chevron-left")}${e(t("Queue"))}</a>` : ""}<code class="ref">${e(ref)}</code>${pager}<span class="grow"></span><a class="button quiet" href="${e(scopeHref)}" data-nav data-scope aria-haspopup="dialog">${icon("file")}<span class="scope-label">${e(t("Scope"))}</span></a>`
      : `<span class="workspace-name">${e(workspace || t("Local workspace"))}</span><span class="grow"></span><button type="button" class="ibtn" data-action="refresh" aria-label="${e(t("Refresh"))}">${icon("refresh")}</button>`;
    const labels = { language: t(lang === "vi" ? "Switch to English" : "Switch to Vietnamese"), dark: t("Switch to dark theme"), light: t("Switch to light theme") };
    return `<header class="navbar">${brand}${context}${core.appearanceControls({ lang, theme }, labels, icon(theme === "dark" ? "sun" : "moon"), notifications, settings)}</header>`;
  }

  const errorScreen = (message, t, retry) =>
    `<section class="error" role="alert"><h1>${e(t("The Local session is unavailable."))}</h1><p>${e(message)}</p><p>${e(t("Refresh to read the workspace again. Decisions require current workspace data."))}</p>${retry}</section>`;
  const queueError = (message, t) =>
    `<p class="warning" role="alert">${e(t("Work list unavailable"))} · ${e(message)}</p>`;
  const noticeItems = (items, t, href, actor) => items.length
    ? items.map((item) => `<a class="notice-item" href="${e(href(item.ref))}" data-dialog-nav><strong>${e(item.title)}</strong><small>${e(actor(item.next_actor))} · ${e(t(core.workAction(item)))} · ${e(item.ref)}</small></a>`).join("")
    : `<p>${e(t("Nothing needs your attention."))}</p>`;

  function updateLabel(update, t) {
    return t(
      {
        unchecked: "Not checked",
        available: "Update available",
        current: "Up to date",
        disabled: "Self-update unavailable",
        updated: "Update installed",
        failed: "Update failed",
        running: "Update running",
      }[update?.status] || "Not checked",
    );
  }
  function toolsView(state, settingsBusy, t, button) {
    const checking = settingsBusy === "update-check";
    const available = state.update?.status === "available";
    const checkingLabel = checking ? "Working…" : state.update?.latest_version ? "Check again" : "Check for updates";
    const checkAgain = button("update-check", checkingLabel, false, state.update?.status === "running" || settingsBusy ? "disabled" : "");
    const reviewUpdate = available ? button("update-preview", settingsBusy === "update-preview" ? "Working…" : "Review update", true, settingsBusy ? "disabled" : "") : "";
    return `<div class="settings-body">${state.maintenanceError ? `<p role="alert" class="warning">${e(state.maintenanceError)}</p>` : ""}<section class="settings-row">${icon("terminal")}<div><h3>${e(t("Version"))}</h3><p>${e(t("Running version"))} <code>${e(state.meta?.version)}</code> · ${e(updateLabel(state.update, t))}${state.update?.latest_version && available ? ` <code>${e(state.update.latest_version)}</code>` : ""}</p>${state.update?.reason ? `<p>${e(t(state.update.reason))}</p>` : ""}${state.update?.restart_required ? `<p class="warning">${e(t("Restart the Local UI to use the installed version."))}</p>` : ""}</div><div class="settings-actions">${reviewUpdate}${checkAgain}</div></section><section class="settings-row">${icon("folder")}<div><h3>${e(t("Cache"))}</h3><p>${e(t("Only SpecGate-owned update-check cache is eligible."))}</p></div>${button("cache-preview", settingsBusy === "cache-preview" ? "Working…" : "Preview cleanup", false, settingsBusy === "cache-preview" ? "disabled" : "")}</section></div>`;
  }
  return {
    icon,
    toolsView,
    updateLabel,
    kv,
    technicalView,
    decisionDock,
    statusPresentation,
    contextView,
    drawerFrame,
    decisionConfirmation,
    updateReview,
    queueResults,
    testObservation,
    citationLabel,
    rungs,
    selectedEvidence,
    reviewScreen,
    queueList,
    queueScreen,
    topBar,
    errorScreen,
    queueError,
    noticeItems,
    queueTitle,
    cacheBody,
    criteriaList,
    evidencePane,
    reviewSummary,
    withRequiredTests,
    ageLabel,
    reviewChecks,
    reviewComparison,
  };
});
