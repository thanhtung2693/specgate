import { sample, beginDecision, recordDecision, reset } from "./sample.mjs";

const { escape: e, appearanceControls } = globalThis.SpecGateUI;
const { icon, decisionView, kv } = globalThis.SpecGatePresentation;
const { renderMarkdown } = globalThis.SpecGateMarkdown;
const { renderSource } = globalThis.SpecGateSource;
const vi = {
  ...globalThis.SpecGateLocalVi,
  "Demo · sample data · changes only affect this demo.": "Demo · Dữ liệu mẫu · Thay đổi chỉ áp dụng trong demo.",
  "Interactive demo": "Demo tương tác",
  "Reset demo": "Đặt lại demo",
  "Review a /health delivery": "Duyệt bản giao /health",
  "Sample delivery {n}": "Bản giao mẫu {n}",
  "Check which requirements have test evidence before deciding.": "Kiểm tra bằng chứng cho từng tiêu chí trước khi quyết định.",
  "Agent claim · Requirement met": "Agent báo đã đáp ứng tiêu chí",
  "The sample report claims this requirement is met.": "Báo cáo mẫu ghi đã đáp ứng tiêu chí này.",
  "Sample command · Exit 0": "Lệnh mẫu · Mã thoát 0",
  "The sample suite passed. The selected 503 test has no result in this delivery.": "Bộ test mẫu chạy đạt. Bản giao này thiếu kết quả cho test 503.",
  "The revised sample includes passing results for the 200 and 503 tests.": "Bản mẫu đã sửa có kết quả đạt cho hai test 200 và 503.",
  "Sample test passed": "Test mẫu đạt",
  "Sample selected-test result missing": "Thiếu kết quả test mẫu",
  "No test attached": "Chưa gắn test riêng",
  "Cited sample source": "Mã nguồn mẫu được trích dẫn",
  "Sample code": "Mã mẫu",
  "Sample diff": "Diff mẫu",
  "Compared with the baseline sample file.": "So sánh với tệp mẫu ban đầu.",
  "Approved sample scope": "Phạm vi mẫu đã duyệt",
  "Review demo decision": "Thử quyết định",
  "The note is used only within this demo.": "Ghi chú chỉ dùng trong demo này.",
  "You will confirm before recording the sample decision.": "Bạn sẽ xác nhận trước khi ghi quyết định mẫu.",
  "Record this decision for the sample review below.": "Ghi quyết định cho bản duyệt mẫu dưới đây.",
  "This decision is only reflected in this demo.": "Quyết định chỉ được ghi trong demo này.",
  "Sample decision recorded": "Đã ghi quyết định mẫu",
  "Open revised sample": "Mở bản mẫu đã sửa",
  "Demo reset.": "Đã đặt lại demo.",
  "Demo changes requested": "Đã yêu cầu sửa trong demo",
  "Demo delivery accepted": "Đã duyệt bản giao trong demo",
};
const overrides = {
  "Recorded decision": "Sample decision recorded",
  "The agent reads your decision and note through SpecGate.": "The note is used only within this demo.",
  "You will confirm before saving the decision.": "You will confirm before recording the sample decision.",
};

const main = document.querySelector("#main");
const dialog = document.querySelector("#dialog");
const controls = document.querySelector("#controls");
const banner = document.querySelector("#demo-banner");
const feedback = document.querySelector("#feedback");
const themePreference = matchMedia("(prefers-color-scheme: dark)");
let lang = "en", theme = null, tab = "evidence", state = reset(), draft = "", pending = null;
const expanded = new Set();
const sourceModes = new Map();
const t = (label) => {
  const key = overrides[label] || label;
  return lang === "vi" ? vi[key] || key : key;
};
const button = (action, text, extra = "", primary = false) =>
  `<button type="button" class="button${primary ? " primary" : ""}" data-action="${action}" ${extra}>${e(t(text))}</button>`;

const scopeText = {
  en: `# Add a database-aware health endpoint

Report the service health so monitoring can distinguish a healthy service from a failed dependency.

- **HTTP 200** with \`ok\` when the database responds.
- **HTTP 503** with \`degraded\` when the database is down.
- Keep raw error text out of the response body.

Evidence should cover both branches with individually named tests.`,
  vi: `# Thêm endpoint theo dõi sức khỏe dịch vụ

Báo tình trạng để hệ thống giám sát phân biệt dịch vụ khỏe và phụ thuộc bị lỗi.

- **HTTP 200** với \`ok\` khi cơ sở dữ liệu phản hồi.
- **HTTP 503** với \`degraded\` khi cơ sở dữ liệu ngừng hoạt động.
- Không đưa nội dung lỗi gốc vào phần thân phản hồi.

Cần có kết quả từ test riêng cho từng nhánh.`,
};

function displaySource(container, delivery, criterion, view) {
  const source = { ...delivery.source.source, highlight_start: criterion.line, highlight_end: criterion.line };
  const template = document.createElement("template");
  template.innerHTML = renderSource({ ...delivery.source, source }, { view, lang });
  const bar = template.content.querySelector(".source-view-toolbar");
  bar.querySelector(".source-version").remove();
  for (const control of bar.querySelectorAll("[data-source-view]"))
    control.textContent = t(control.dataset.sourceView === "code" ? "Sample code" : "Sample diff");
  const content = template.content.querySelector("[data-source-content]");
  content.querySelector("[role=region]")?.setAttribute("aria-label", t(view === "code" ? "Sample code" : "Sample diff"));
  const displayed = container.querySelector("[data-source-content]");
  if (displayed) {
    for (const control of container.querySelectorAll("[data-source-view]"))
      control.setAttribute("aria-pressed", String(control.dataset.sourceView === view));
    displayed.replaceChildren(...content.childNodes);
  } else {
    template.content.querySelector(".source-checkout-note").textContent = t("Compared with the baseline sample file.");
    container.append(template.content);
  }
}

const outcomes = {
  passed: ["Sample test passed", "success", "check-circle"],
  missing: ["Sample selected-test result missing", "warning", "blocked"],
  claim: ["No test attached", "neutral", "quote"],
};

function evidenceView(delivery) {
  const reportLabel = state.version === 1
    ? "The sample suite passed. The selected 503 test has no result in this delivery."
    : "The revised sample includes passing results for the 200 and 503 tests.";
  return `<p class="demo-note">${e(t(reportLabel))}</p><div class="criteria">${delivery.criteria.map((criterion, i) => {
    const [outcome, tone, glyph] = outcomes[criterion.outcome];
    return `<details class="criterion demo-criterion ${tone}" data-criterion="${criterion.id}" ${expanded.has(criterion.id) ? "open" : ""}>
      <summary>${icon(glyph, "result-mark")}<span class="criterion-text"><code class="criterion-id">AC-0${i + 1}</code> ${e(criterion.requirement)}</span><span class="criterion-observation">${e(t(outcome))}</span>${icon("chevron", "chevron")}</summary>
      <div class="criterion-body"><div class="evidence-columns">
        <section class="agent-report"><h3>${icon("quote")}${e(t("Agent claim · Requirement met"))}</h3><p>${e(t("The sample report claims this requirement is met."))}</p></section>
        <section class="test-result"><h3>${icon("evidence")}${e(t(outcome))}</h3>${criterion.symbol ? `<code>${e(criterion.symbol)}</code>` : ""}</section>
      </div><div class="source-reference"><h3>${icon("code")}${e(t("Cited sample source"))}</h3>
      <div class="source-inspector"><div class="source-location"><code>health.go</code><span>${e(globalThis.SpecGatePresentation.citationLabel("line: " + criterion.line, lang))}</span></div><div class="source-excerpt" data-sample-source></div></div></div></div>
    </details>`;
  }).join("")}</div>
  <details class="evidence-disclosure"><summary>${icon("chevron", "chevron")}${e(t("Sample command · Exit 0"))}</summary><p>${e(t(reportLabel))}</p><pre><code>go test ./...</code></pre></details>
  <div class="evidence-footer">${button("tab", "Review demo decision", 'data-tab="decision"', true)}</div>`;
}

function decisionPane(delivery) {
  const view = {
    can_decide: !state.decision,
    status: { ref: "DEMO-001", review_id: delivery.review, completion_id: delivery.completion },
    ...(state.decision && {
      review: { id: delivery.review, report_id: delivery.completion, human_decision: state.decision.verdict, note: state.decision.note },
    }),
  };
  const end = state.decision?.verdict === "reject" && state.version === 1
    ? `<div class="demo-after">${button("revised", "Open revised sample", "", true)}</div>`
    : "";
  const title = state.decision?.verdict === "approve" ? "Demo delivery accepted" : "Demo changes requested";
  const template = document.createElement("template");
  template.innerHTML = decisionView(view, draft, () => "", t);
  template.content.querySelector(".recorded-notice p")?.replaceChildren(t(title));
  template.content.querySelector(".version-details")?.remove();
  template.content.querySelector(".kv")?.remove();
  const checklist = delivery.criteria.map((criterion, i) => {
    const [outcome, tone, glyph] = outcomes[criterion.outcome];
    return `<li class="${tone}">${icon(glyph, "result-mark")}<span><code>AC-0${i + 1}</code> ${e(criterion.requirement)}</span><small>${e(t(outcome))}</small></li>`;
  }).join("");
  return `<div class="decision-layout"><section><h2>${e(t("Acceptance criteria"))}</h2><ul class="decision-criteria">${checklist}</ul></section>${template.content.querySelector(".decision-form").outerHTML}</div>${end}`;
}

function render() {
  const dark = theme ?? themePreference.matches;
  document.documentElement.dataset.theme = dark ? "dark" : "light";
  document.documentElement.lang = lang;
  document.title = "SpecGate · " + t("Interactive demo");
  document.querySelector("#skip").textContent = t("Skip to content");
  controls.innerHTML = appearanceControls({ lang, theme: dark ? "dark" : "light" }, {
    language: t("Language"), light: t("Switch to light theme"), dark: t("Switch to dark theme"),
  }, icon(dark ? "sun" : "moon"));
  banner.innerHTML = `<strong>${e(t("Demo · sample data · changes only affect this demo."))}</strong>${button("reset", "Reset demo")}`;
  const delivery = sample(state.version);
  const body = tab === "evidence" ? evidenceView(delivery)
    : tab === "scope" ? `<article class="context-reader demo-scope" aria-label="${e(t("Approved sample scope"))}"><div class="markdown-body">${renderMarkdown(scopeText[lang])}</div></article>`
    : decisionPane(delivery);
  main.innerHTML = `<header class="page-head"><span class="tile accent">${icon("person")}</span><div><h1>${e(t("Review a /health delivery"))}</h1><div class="work-meta"><code>DEMO-001</code><span>${e(t("Sample delivery {n}").replace("{n}", state.version))}</span></div></div><p class="work-need">${e(t("Check which requirements have test evidence before deciding."))}</p></header>
    <nav class="tabs demo-tabs" aria-label="${e(t("Review details"))}">${[["evidence", "Evidence", "evidence"], ["scope", "Scope", "file"], ["decision", "Decision", "person"]].map(([key, label, glyph]) =>
      `<button type="button" class="button quiet" data-action="tab" data-tab="${key}" ${tab === key ? 'aria-current="page"' : ""}>${icon(glyph)}${e(t(label))}</button>`,
    ).join("")}</nav><div class="demo-view">${body}</div>`;
  for (const details of main.querySelectorAll(".demo-criterion")) {
    const id = details.dataset.criterion;
    const mount = () => displaySource(details.querySelector("[data-sample-source]"), delivery, delivery.criteria.find((item) => item.id === id), sourceModes.get(id) || "code");
    if (details.open) mount();
    details.addEventListener("toggle", () => {
      if (details.open) {
        expanded.add(id);
        if (!details.querySelector("[data-source-content]")) mount();
      } else expanded.delete(id);
    });
    details.addEventListener("click", (event) => {
      const control = event.target.closest("[data-source-view]");
      if (!control || control.getAttribute("aria-pressed") === "true") return;
      sourceModes.set(id, control.dataset.sourceView);
      mount();
    });
  }
}

function replaceSample(version) {
  state = reset(version);
  pending = null;
  draft = "";
  tab = "evidence";
  expanded.clear();
  sourceModes.clear();
  render();
  main.focus();
}

function openConfirmation(verdict) {
  pending = beginDecision(state, verdict, draft);
  if (!pending) {
    const note = document.querySelector("#decision-note");
    if (verdict === "reject" && note) {
      note.setAttribute("aria-invalid", "true");
      document.querySelector("#note-error").textContent = t("A note is required when requesting changes.");
      note.focus();
    }
    return;
  }
  const action = verdict === "approve" ? "Accept delivery" : "Ask for changes";
  dialog.innerHTML = `<header><h2 id="dialog-title">${icon("person")}${e(t(verdict === "approve" ? "Confirm acceptance" : "Confirm request for changes"))}</h2></header>
    <p>${e(t("Record this decision for the sample review below."))}</p>
    ${kv([["Review ID", pending.review], ["Completion ID", pending.completion], ["Your note", pending.note]], t)}
    <p>${e(t("This decision is only reflected in this demo."))}</p>
    <div class="decision-actions">${button("cancel", "Cancel", "autofocus")}${button("confirm", action, "", true)}</div>`;
  dialog.showModal();
}

document.addEventListener("input", (event) => {
  if (event.target.id !== "decision-note") return;
  draft = event.target.value;
  if (!draft.trim()) return;
  event.target.removeAttribute("aria-invalid");
  document.querySelector("#note-error").textContent = "";
});

document.addEventListener("click", (event) => {
  const language = event.target.closest("[data-language]");
  if (language) {
    lang = language.dataset.language;
    render();
    controls.querySelector(`[data-language="${lang}"]`).focus();
    return;
  }
  const control = event.target.closest("[data-action]");
  if (!control) return;
  switch (control.dataset.action) {
    case "theme":
      theme = !(theme ?? themePreference.matches);
      render();
      controls.querySelector(".theme-button").focus();
      break;
    case "tab":
      tab = control.dataset.tab;
      render();
      main.querySelector(`.demo-tabs [data-tab="${tab}"]`).focus();
      break;
    case "reset":
      replaceSample(1);
      feedback.textContent = t("Demo reset.");
      break;
    case "revised":
      replaceSample(2);
      break;
    case "accept":
      openConfirmation("approve");
      break;
    case "request-changes":
      openConfirmation("reject");
      break;
    case "cancel":
      dialog.close();
      break;
    case "confirm":
      state = recordDecision(state, pending);
      dialog.close();
      render();
      main.focus();
      feedback.textContent = t("Sample decision recorded");
      break;
  }
});
dialog.addEventListener("close", () => { if (!dialog.open) pending = null; });
themePreference.addEventListener("change", () => { if (theme === null) render(); });
render();
