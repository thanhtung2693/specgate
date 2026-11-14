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
    const glyph = tone === "success" ? "check-circle" : tone === "warning" || tone === "danger" ? "blocked" : "circle";
    const markers = [
      ["quote", claimLabel(row.claim, t), row.claim && row.claim !== "missing" ? "neutral" : "unmarked"],
      ["terminal", observedCheck ? t("Observed check") + " · " + checkStatusLabel(row.check_status, t) : t("No CLI-observed check"), observedCheck ? row.check_status === "pass" ? "success" : row.check_status === "fail" ? "danger" : "warning" : "unmarked"],
      ["evidence", observation.text, observedTests || row.selected_tests_required ? observation.tone : "unmarked"],
    ].map(([name, help, tone]) => `<span class="${tone}" title="${e(help)}">${icon(name)}<span class="visually-hidden">${e(help)}</span></span>`).join("");
    return { ...p, label, tone, glyph, markers, observation, gap: hasGap && observedCheck };
  }

  function criterionView(row, reports, lang, t) {
    const p = criterionSummary(row, lang, t),
      original = reports.find((item) => item.criterion_id === row.criterion_id),
      detail = criterionDetails(row, original),
      observation = p.observation;
    const claim = claimLabel(row.claim, t);
    const gaps = row.gaps?.length ? `<ul>${row.gaps.map((gap) => `<li>${e(gap)}</li>`).join("")}</ul>` : "";
    return `<details class="criterion ${p.tone}" data-criterion="${e(row.criterion_id || row.id)}"><summary>${icon(p.glyph, "result-mark")}<span class="criterion-text"><code class="criterion-id">${e(p.id)}</code> ${e(row.text)}</span><span class="criterion-observation"><span class="markers">${p.markers}</span>${p.gap ? `<span class="gap-label">${e(t("Evidence gap"))} · </span>` : ""}${e(p.label)}</span>${icon("chevron", "chevron")}</summary><div class="criterion-body">${gaps ? `<div class="warning"><strong>${e(t("Evidence gaps"))}</strong>${gaps}</div>` : ""}<div class="evidence-columns"><section class="agent-report"><h3>${icon("quote")}${e(t("Agent report"))}</h3><p class="evidence-text">${e(detail.report || claim)}</p></section><section class="test-result"><h3>${icon("evidence")}${e(t("Run results"))}</h3><p class="observation ${observation.tone}-text">${e(observation.text)}</p>${detail.check ? `<code>${e(detail.check)}</code>` : ""}${detail.tests ? `<p class="mono evidence-text">${e(detail.tests)}</p>` : ""}${row.check_name && row.why ? `<p>${e(row.why)}</p>` : ""}</section></div>${detail.source ? `<div class="source-reference"><h3>${icon("code")}${e(t("Cited source"))}</h3><div class="source-inspector"><div class="source-location"><code>${e(row.evidence_path)}</code>${row.citation ? `<span>${e(citationLabel(row.citation, lang))}</span>` : ""}</div><div class="source-excerpt" data-source-slot></div></div></div>` : ""}<details class="raw-details"><summary>${icon("chevron", "chevron")}${e(t("View original data"))}</summary><pre tabindex="0">${e(JSON.stringify({ review: row, ...(original ? { original_report: original } : {}) }, null, 2))}</pre></details></div></details>`;
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

  function evidenceReview(rows, work, lang, t) {
    rows = withRequiredTests(rows, work);
    return `${!work.status?.completion_id ? `<p class="empty">${icon("clock")}${e(t("No delivery yet."))}</p>` : ""}${rows.length ? `<div class="criteria" aria-label="${e(t("Acceptance criteria"))}">${rows.map((row) => criterionView(row, work.completion?.criteria || [], lang, t)).join("")}</div>` : ""}<div class="evidence-disclosures">${reviewChecks(work.completion, t)}${reviewComparison(work.review_comparison, t, { rows, completion: work.completion, lang })}</div>`;
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

  function decisionView(d, note, actor, t, rows = [], lang = "en") {
    const s = d.status,
      pins = decisionPins(d),
      recorded = recordedDecision(d);
    const checklist = withRequiredTests(rows, d).map((row) => {
      const p = criterionSummary(row, lang, t);
      return `<li class="${p.tone}">${icon(p.glyph, "result-mark")}<span><code>${e(p.id)}</code> ${e(row.text)}</span><small>${p.gap ? `<span class="gap-label">${e(t("Evidence gap"))} · </span>` : ""}${e(p.label)}</small></li>`;
    }).join("");
    const framing = (body) => `<div class="decision-layout">${checklist ? `<section><h2>${e(t("Acceptance criteria"))}</h2><ul class="decision-criteria">${checklist}</ul></section>` : ""}${body}</div>`;
    if (recorded)
      return framing(`<section class="decision-form recorded"><div class="recorded-notice ${recorded.decision === "approve" ? "success" : "warning"}">${icon("check")}<div><h2>${e(t("Recorded decision"))}</h2><p>${e(t(recorded.decision === "approve" ? "Accepted" : "Rejected"))} · ${e(actor(s.next_actor))}</p></div></div>${recorded.note ? `<h3>${e(t("Your note"))}</h3><blockquote class="recorded-note">${e(recorded.note)}</blockquote>` : ""}${technicalView(s, t)}${kv(
        [
          ["Actor", recorded.actor],
          ["Decision recorded at", recorded.observed_at],
        ].filter(([, value]) => value), t,
      )}</section>`);
    if (!pins)
      return `<section class="decision-form"><h2>${e(t("Decision unavailable for this work."))}</h2><p>${e(t(s.decision))}</p><p class="muted">${e(t("This work is not currently actionable in the browser."))}</p></section>`;
    return framing(`<section class="decision-form"><h2><label for="decision-note">${e(t("Your note"))}</label></h2><textarea id="decision-note" maxlength="20000" aria-describedby="note-hint note-error" placeholder="${e(t("What should change, or why are you accepting this delivery?"))}">${e(note)}</textarea><p class="field-hint" id="note-hint">${e(t("Required when requesting changes."))} ${e(t("The agent reads your decision and note through SpecGate."))}</p><p id="note-error" class="dialog-error" role="alert"></p><div class="decision-actions"><button data-action="request-changes">${e(t("Ask for changes"))}</button><button class="primary" data-action="accept">${icon("check")}${e(t("Accept delivery"))}</button></div><p class="field-hint">${e(t("You will confirm before saving the decision."))}</p></section>`);
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
    decisionView,
    statusPresentation,
    contextView,
    testObservation,
    citationLabel,
    criterionView,
    reviewChecks,
    reviewComparison,
    evidenceReview,
  };
});
