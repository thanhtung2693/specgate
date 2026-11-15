import { sample, beginDecision, recordDecision, reset, tourStep } from "./sample.mjs";

const { escape: e } = globalThis.SpecGateUI;
const { icon, kv, rungs, reviewScreen, topBar, withRequiredTests, drawerFrame } = globalThis.SpecGatePresentation;
const { renderMarkdown } = globalThis.SpecGateMarkdown;
const { renderSource } = globalThis.SpecGateSource;
const vi = {
  ...globalThis.SpecGateLocalVi,
  "Demo · sample data · changes only affect this demo.": "Demo · Dữ liệu mẫu · Thay đổi chỉ áp dụng trong demo.",
  "Interactive demo": "Demo tương tác",
  "Reset demo": "Đặt lại demo",
  "Review a /health delivery": "Duyệt bản giao /health",
  "Sample code": "Mã mẫu",
  "Sample diff": "Diff mẫu",
  "Compared with the baseline sample file.": "So sánh với tệp mẫu ban đầu.",
  "Approved sample scope": "Phạm vi mẫu đã duyệt",
  "Record this decision for the sample review below.": "Ghi quyết định cho bản duyệt mẫu dưới đây.",
  "This decision is only reflected in this demo.": "Quyết định chỉ được ghi trong demo này.",
  "Sample decision recorded": "Đã ghi quyết định mẫu",
  "Open revised sample": "Mở bản mẫu đã sửa",
  "Demo reset.": "Đã đặt lại demo.",
  "Restart tour": "Xem lại hướng dẫn",
  "Tour step {n} of 3": "Hướng dẫn · Bước {n}/3",
  "Back": "Quay lại",
  "Next": "Tiếp theo",
  "Finish tour": "Xong hướng dẫn",
  "Skip tour": "Bỏ qua",
  "Check the 503 evidence": "Xem bằng chứng cho mã 503",
  "The sample test run reported no errors, but AC-02 still has no 503 test result.": "Bộ test mẫu chạy xong không báo lỗi, nhưng AC-02 vẫn thiếu kết quả test 503.",
  "AC-02 now shows a passing 503 test result in the revised sample.": "Trong bản mẫu đã sửa, AC-02 đã có kết quả test đạt cho mã 503.",
  "Inspect the cited code": "Đọc mã được trích dẫn",
  "The diff shows where the 503 response was added. Check the test result to see if that path ran and passed.": "Diff cho thấy chỗ thêm phản hồi 503. Xem kết quả test để biết nhánh đó đã được chạy và kiểm tra đạt chưa.",
  "Try a decision": "Thử đưa ra quyết định",
  "This sample already has a decision. Use Reset demo to start over.": "Bản mẫu này đã có quyết định. Chọn Đặt lại demo để bắt đầu lại.",
  "Ask for the missing 503 test result in your note. Choose Ask for changes, confirm, then open the revised sample.": "Ghi chú yêu cầu bổ sung kết quả test 503. Chọn Yêu cầu sửa, xác nhận rồi mở bản đã sửa.",
  "The 503 test now has a result. Check AC-03, which still has only the agent’s claim, then accept or request changes.": "Test 503 đã có kết quả. Xem AC-03 vẫn chỉ có báo cáo của agent, rồi chọn duyệt hoặc yêu cầu sửa.",
};
const overrides = {
  "Recorded decision": "Sample decision recorded",
  "Note · required to ask for changes": "Demo note, kept in this demo · required to ask for changes",
};
vi["Demo note, kept in this demo · required to ask for changes"] = "Ghi chú trong demo, không rời trang này · bắt buộc khi yêu cầu sửa";

const main = document.querySelector("#main");
const dialog = document.querySelector("#dialog");
const scopeDialog = document.querySelector("#scope-dialog");
const bar = document.querySelector("#bar");
const banner = document.querySelector("#demo-banner");
const feedback = document.querySelector("#feedback");
const themePreference = matchMedia("(prefers-color-scheme: dark)");
let lang = "en", theme = null, selected = "", state = reset(), draft = "", pending = null;
const sourceModes = new Map();
let tourIndex = 0;
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

function displaySource(container, delivery, row, view) {
  const line = Number(/\d+/.exec(row.citation)?.[0]);
  const source = { ...delivery.source.source, highlight_start: line, highlight_end: line };
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

const rowId = (row) => row.criterion_id;
const actor = () => t("Human reviewer");

function view() {
  const delivery = sample(state.version), decision = state.decision;
  const rows = withRequiredTests(delivery.rows, { completion: delivery.completion_data });
  const chosen = rows.find((row) => rowId(row) === selected) || rows.find((row) => rungs(row).problem) || rows[0];
  const work = {
    work: { title: t("Review a /health delivery") },
    can_decide: !decision,
    status: {
      ref: "DEMO-001", review_id: delivery.review, completion_id: delivery.completion, next_actor: "human_reviewer",
      state: decision ? (decision.verdict === "approve" ? "accepted" : "rework_requested") : "awaiting_acceptance",
    },
    completion: delivery.completion_data,
    ...(decision && { review: { id: delivery.review, report_id: delivery.completion, human_decision: decision.verdict, note: decision.note } }),
  };
  return { delivery, rows, work, selected: rowId(chosen) };
}

function tourCard(step) {
  const progress = t("Tour step {n} of 3").replace("{n}", tourIndex + 1);
  return `<section class="demo-tour" aria-labelledby="tour-title"><div>
    <p class="eyebrow">${e(progress)}</p>
    <h2 id="tour-title" tabindex="-1" aria-describedby="tour-description">${e(t(step.title))}</h2>
    <p id="tour-description">${e(t(step.text))}</p>
    </div><div class="tour-actions">
    ${button("tour-stop", "Skip tour")}
    ${tourIndex > 0 ? button("tour-back", "Back") : ""}
    ${tourIndex < 2 ? button("tour-next", "Next", "", true) : button("tour-stop", "Finish tour", "", true)}
    </div></section>`;
}

/* The shared review screen, with the Scope control and the decision dock adapted for the demo. */
function reviewHtml(v, step) {
  const template = document.createElement("template");
  template.innerHTML = reviewScreen(v.work, v.rows, { selected: v.selected, enter: false, note: draft, lang, t, actor });
  const root = template.content;
  const delivery = v.delivery, row = v.rows.find((item) => rowId(item) === v.selected);
  const slot = root.querySelector("[data-source-slot]");
  if (slot) displaySource(slot, delivery, row, sourceModes.get(v.selected) || "code");
  const dock = root.querySelector(".dock");
  const wrap = document.createElement("div");
  wrap.className = "dock-wrap";
  dock.replaceWith(wrap);
  wrap.append(dock);
  if (state.decision?.verdict === "reject" && state.version === 1)
    wrap.insertAdjacentHTML("beforeend", `<div class="demo-after">${button("revised", "Open revised sample", "", true)}</div>`);
  if (step) {
    const place = { evidence: root.querySelector(".evidence > .rr"), source: root.querySelector(".source-reference"), dock };
    place[step.target.anchor].insertAdjacentHTML("beforebegin", tourCard(step));
  }
  return template.innerHTML;
}

function goToTour(index, focus = true) {
  const step = tourStep(state, index);
  if (!step) return;
  tourIndex = index;
  const { criterion, source } = step.target;
  if (criterion) {
    selected = criterion;
    sourceModes.set(criterion, source);
  }
  render();
  if (focus) document.querySelector("#tour-title").focus();
}

function select(id) {
  if (id === view().selected) return;
  selected = id;
  const target = tourIndex === null ? null : tourStep(state, tourIndex).target.criterion;
  if (target && target !== id) tourIndex = null;
  render();
  main.querySelector(`[data-criterion-select="${id}"]`)?.focus({ preventScroll: true });
}

function render() {
  const dark = theme ?? themePreference.matches;
  document.documentElement.dataset.theme = dark ? "dark" : "light";
  document.documentElement.lang = lang;
  document.title = "SpecGate · " + t("Interactive demo");
  document.querySelector("#skip").textContent = t("Skip to content");
  bar.innerHTML = topBar({ t, href: () => "#", lang, theme: dark ? "dark" : "light", ref: "DEMO-001", scopeHref: "#", back: false, home: "../", logo: "../logo.svg" });
  banner.innerHTML = `<strong>${e(t("Demo · sample data · changes only affect this demo."))}</strong><div class="demo-banner-actions">
    ${tourIndex === null ? button("tour-start", "Restart tour") : ""}${button("reset", "Reset demo")}</div>`;
  main.className = "review-main";
  main.innerHTML = reviewHtml(view(), tourIndex === null ? null : tourStep(state, tourIndex));
}

function openScope() {
  scopeDialog.innerHTML = drawerFrame(t("Scope"), t("Close dialog"), `<article class="context-reader" aria-label="${e(t("Approved sample scope"))}"><div class="markdown-body">${renderMarkdown(scopeText[lang])}</div></article>`);
  scopeDialog.showModal();
}

function replaceSample(version) {
  state = reset(version);
  pending = null;
  draft = "";
  selected = "";
  tourIndex = null;
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
  if (event.target.closest("[data-scope]")) {
    event.preventDefault();
    return openScope();
  }
  const language = event.target.closest("[data-language]");
  if (language) {
    lang = language.dataset.language;
    render();
    bar.querySelector("[data-language]").focus();
    return;
  }
  const criterion = event.target.closest("[data-criterion-select]");
  if (criterion) return select(criterion.dataset.criterionSelect);
  const mode = event.target.closest("[data-source-view]");
  if (mode && mode.getAttribute("aria-pressed") !== "true") {
    const pane = mode.closest("[data-source-pane]"), v = view();
    sourceModes.set(pane.dataset.criterion, mode.dataset.sourceView);
    displaySource(pane.querySelector("[data-source-slot]"), v.delivery, v.rows.find((row) => rowId(row) === pane.dataset.criterion), mode.dataset.sourceView);
    return;
  }
  const control = event.target.closest("[data-action]");
  if (!control) return;
  switch (control.dataset.action) {
    case "theme":
      theme = !(theme ?? themePreference.matches);
      render();
      bar.querySelector(".theme-button").focus();
      break;
    case "tour-start":
      goToTour(0);
      break;
    case "tour-next":
      goToTour(tourIndex + 1);
      break;
    case "tour-back":
      goToTour(tourIndex - 1);
      break;
    case "tour-stop":
      tourIndex = null;
      render();
      (main.querySelector("#decision-note") || main.querySelector(".crit.sel")).focus();
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
      tourIndex = null;
      dialog.close();
      render();
      main.focus();
      feedback.textContent = t("Sample decision recorded");
      break;
  }
});
window.addEventListener("keydown", (event) => {
  const step = { j: 1, ArrowDown: 1, k: -1, ArrowUp: -1 }[event.key];
  if (!step || event.ctrlKey || event.metaKey || event.altKey || event.shiftKey || scopeDialog.open || dialog.open) return;
  const row = event.target?.closest?.("[data-criterion-select]")?.parentElement;
  const next = (step > 0 ? row?.nextElementSibling : row?.previousElementSibling)?.querySelector("[data-criterion-select]");
  if (!next) return;
  event.preventDefault();
  if (next.getAttribute("aria-current") === "true") next.focus();
  else select(next.dataset.criterionSelect);
});
dialog.addEventListener("close", () => { if (!dialog.open) pending = null; });
scopeDialog.addEventListener("click", (event) => {
  if (event.target === scopeDialog || event.target.closest("[data-close-scope]")) scopeDialog.close();
});
scopeDialog.addEventListener("close", () => bar.querySelector("[data-scope]")?.focus());
themePreference.addEventListener("change", () => { if (theme === null) render(); });
goToTour(0, false);
