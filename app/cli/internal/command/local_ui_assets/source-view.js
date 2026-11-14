/* Current checkout excerpts are inspection aids, separate from recorded evidence. */
(function (root, factory) {
  if (typeof module !== "undefined" && module.exports)
    module.exports = factory(require("./core.js"));
  else root.SpecGateSource = factory(root.SpecGateUI);
})(globalThis, function (core) {
  "use strict";
  const e = core.escape;
  const copy = (lang) =>
    lang === "vi"
      ? {
          code: "Code hiện tại",
          diff: "Diff so với HEAD",
          loading: "Đang đọc code hiện tại…",
          unavailable: "Chưa đọc được code",
          diffUnavailable: "Chưa có diff",
          truncated: "Nội dung đã được giới hạn.",
          empty: "Không có khác biệt so với HEAD.",
          details: "Chi tiết",
          headUnavailable: "Chưa có HEAD",
          note: "Checkout tại lúc đọc · không phải bằng chứng đã chốt. Tải lại để cập nhật.",
          added: "Thêm",
          removed: "Xóa",
          failed: "Không đọc được nguồn. Tải lại để thử lại.",
          changed: "Bản giao đã đổi. Tải lại để xem nguồn mới.",
        }
      : {
          code: "Current code",
          diff: "Diff against HEAD",
          loading: "Loading current code…",
          unavailable: "Source unavailable",
          diffUnavailable: "Diff unavailable",
          truncated: "Content is truncated.",
          empty: "No differences against HEAD.",
          details: "Details",
          headUnavailable: "HEAD unavailable",
          note: "Current checkout when fetched; not frozen evidence. Refresh to update.",
          added: "Added",
          removed: "Removed",
          failed: "Source request failed. Refresh to try again.",
          changed: "The completion changed. Refresh to inspect the new source.",
        };
  function reasonText(reason, lang) {
    const labels = {
      completion_unavailable: [
        "Completion evidence is unavailable.",
        "Chưa có bàn giao để đối chiếu.",
      ],
      citation_unavailable: [
        "No numeric code citation is recorded.",
        "Chưa có trích dẫn dòng code.",
      ],
      no_numeric_citation: [
        "No numeric code citation is recorded.",
        "Chưa có trích dẫn dòng code.",
      ],
      unsupported_evidence_kind: [
        "This evidence is not a code file.",
        "Nguồn bằng chứng này không phải tệp code.",
      ],
      unsafe_path: [
        "This source cannot be safely displayed.",
        "Nguồn này không thể hiển thị an toàn.",
      ],
      sensitive_path: [
        "This source cannot be safely displayed.",
        "Nguồn này không thể hiển thị an toàn.",
      ],
      symlink_not_allowed: [
        "Symbolic links are not read.",
        "Không đọc nội dung qua liên kết tượng trưng.",
      ],
      not_regular_file: [
        "This source is not a regular file.",
        "Nguồn này không phải tệp thông thường.",
      ],
      file_too_large: [
        "The file exceeds the reading limit.",
        "Tệp vượt giới hạn đọc.",
      ],
      file_unavailable: [
        "The file is unavailable in the current checkout.",
        "Chưa đọc được tệp trong checkout hiện tại.",
      ],
      binary_or_invalid_utf8: [
        "This file is not UTF-8 text.",
        "Tệp không phải văn bản UTF-8.",
      ],
      sensitive_content: [
        "Sensitive content is not displayed.",
        "Nội dung nhạy cảm không được hiển thị.",
      ],
      citation_out_of_range: [
        "The cited line is outside the current file.",
        "Dòng được trích dẫn không có trong tệp hiện tại.",
      ],
      head_unavailable: [
        "HEAD is unavailable for comparison.",
        "Chưa có HEAD để so sánh.",
      ],
      untracked: [
        "The file is not tracked by Git.",
        "Tệp chưa được Git theo dõi.",
      ],
      diff_too_large: [
        "The files exceed the line limit for comparison.",
        "Số dòng vượt giới hạn để so sánh.",
      ],
      diff_unavailable: ["The diff is unavailable.", "Chưa đọc được diff."],
    };
    return (
      labels[reason]?.[lang === "vi" ? 1 : 0] ||
      (lang === "vi"
        ? "Chưa thể đọc nguồn an toàn."
        : "The source could not be safely read.")
    );
  }
  const scope = (status) =>
    [
      status?.ref,
      status?.review_id,
      status?.completion_id,
      status?.basis_digest,
    ].join("\u0000");
  function createSourceLoader(api) {
    const gate = core.requestGate();
    let epoch = gate.start(),
      activeScope = "",
      cache = new Map();
    const clear = () => {
      epoch = gate.start();
      activeScope = "";
      cache.clear();
    };
    function load(status, criterion) {
      const current = scope(status);
      if (current !== activeScope) {
        clear();
        activeScope = current;
      }
      if (cache.has(criterion)) return cache.get(criterion);
      const ticket = epoch,
        completion = String(status.completion_id || "");
      const promise = Promise.resolve(
        api(
          "/api/source?" + new URLSearchParams({ ref: status.ref, criterion }),
        ),
      ).then(
        (data) =>
          gate.current(ticket)
            ? String(data.completion_id || "") === completion
              ? { data }
              : { changed: true }
            : null,
        (error) =>
          gate.current(ticket)
            ? { error: error.message || String(error) }
            : null,
      );
      cache.set(criterion, promise);
      return promise;
    }
    return { clear, load };
  }
  function renderSourceContent(data, view, lang) {
    const c = copy(lang),
      source = data.source || {},
      diff = data.diff || {},
      showDiff = view === "diff" && diff.available === true;
    const codeLines = (showDiff ? [] : source.lines || [])
      .map((line) => {
        const cited =
          Number.isInteger(line.number) &&
          Number.isInteger(source.highlight_start) &&
          Number.isInteger(source.highlight_end) &&
          line.number >= source.highlight_start &&
          line.number <= source.highlight_end;
        return `<div class="source-line${cited ? " cited" : ""}" data-line="${e(line.number)}"><span class="line-number" aria-hidden="true">${e(line.number)}</span><code>${e(line.text)}</code></div>`;
      })
      .join("");
    const diffLines = (showDiff && diff.text ? String(diff.text).split("\n") : [])
      .map((line) => {
        const kind =
          line.startsWith("+") && !line.startsWith("+++")
            ? "added"
            : line.startsWith("-") && !line.startsWith("---")
              ? "removed"
              : line.startsWith("@@")
                ? "hunk"
                : "context";
        const changed = kind === "added" || kind === "removed";
        return `<div class="diff-line ${kind}"><span class="diff-gutter"${changed ? ` aria-label="${e(c[kind])}"` : ""}>${changed ? e(line[0]) : ""}</span><code>${e(changed ? line.slice(1) : line)}</code></div>`;
      })
      .join("");
    const body = showDiff
      ? diff.text
        ? `<div class="source-code diff-code" tabindex="0" role="region" aria-label="${e(c.diff)}">${diffLines}</div>`
        : `<p class="source-unavailable">${e(c.empty)}</p>`
      : source.available
        ? `<div class="source-code" tabindex="0" role="region" aria-label="${e(c.code)}">${codeLines}</div>`
        : `<p class="source-unavailable">${e(c.unavailable)}${source.reason ? ` · ${e(reasonText(source.reason, lang))}` : ""}</p>`;
    return `${body}${(showDiff ? diff.truncated : source.truncated) ? `<p class="source-truncated">${e(c.truncated)}</p>` : ""}`;
  }
  function renderSource(data, { view = "code", lang = "en" } = {}) {
    const c = copy(lang), source = data.source || {}, diff = data.diff || {};
    const showDiff = view === "diff" && diff.available === true;
    const short = data.head_revision
      ? `HEAD <code>${e(data.head_revision.slice(0, 8))}</code>`
      : e(c.headUnavailable);
    return `<div class="source-view-toolbar"><div class="source-view-controls"><button type="button" data-source-view="code" aria-pressed="${!showDiff}">${e(c.code)}</button><button type="button" data-source-view="diff" aria-pressed="${showDiff}" ${diff.available ? "" : "disabled"}${!diff.available && diff.reason ? ` title="${e(reasonText(diff.reason, lang))}"` : ""}>${e(c.diff)}</button></div><div class="source-version"><span>${short}</span><details><summary>${e(c.details)}</summary><code>${e(data.head_revision || "—")}</code>${source.reason ? `<p><code>${e(source.reason)}</code></p>` : ""}${diff.reason ? `<p><code>${e(diff.reason)}</code></p>` : ""}<p>${e(data.origin || "current_checkout")}</p></details></div></div><div data-source-content>${renderSourceContent(data, view, lang)}</div><p class="source-checkout-note">${e(c.note)}</p>`;
  }
  function mount(root, readWork, api, readLang) {
    const loader = createSourceLoader(api),
      opened = new Set(),
      views = new Map(),
      displayed = new WeakMap();
    async function show(details) {
      const status = readWork()?.status,
        criterion = details.dataset.criterion,
        slot = details.querySelector("[data-source-slot]");
      if (!status || !criterion || !slot) return;
      const inspected = scope(status),
        c = copy(readLang());
      slot.setAttribute("aria-busy", "true");
      slot.innerHTML = `<p class="source-loading" role="status">${e(c.loading)}</p>`;
      const result = await loader.load(status, criterion);
      if (
        !result ||
        !root.contains(details) ||
        scope(readWork()?.status) !== inspected
      )
        return;
      slot.setAttribute("aria-busy", "false");
      if (result.data) displayed.set(slot, { data: result.data, inspected });
      slot.innerHTML = result.changed
        ? `<p class="source-unavailable" role="alert">${e(copy(readLang()).changed)}</p>`
        : result.error
          ? `<p class="source-unavailable" role="alert">${e(c.failed)} ${e(result.error)}</p>`
          : renderSource(result.data, {
              view: views.get(criterion) || "code",
              lang: readLang(),
            });
    }
    root.addEventListener(
      "toggle",
      (event) => {
        const details = event.target;
        if (
          !details.matches?.("details.criterion[data-criterion]") ||
          !root.contains(details)
        )
          return;
        if (details.open) {
          opened.add(details.dataset.criterion);
          show(details);
        } else opened.delete(details.dataset.criterion);
      },
      true,
    );
    root.addEventListener("click", (event) => {
      const button = event.target.closest("[data-source-view]");
      if (!button || button.disabled) return;
      const details = button.closest("details.criterion");
      const slot = details?.querySelector("[data-source-slot]");
      const snapshot = displayed.get(slot);
      if (!snapshot || snapshot.inspected !== scope(readWork()?.status)) return;
      const view = button.dataset.sourceView;
      if (button.getAttribute("aria-pressed") === "true") return;
      views.set(details.dataset.criterion, view);
      slot.querySelector('[data-source-view="code"]').setAttribute("aria-pressed", String(view === "code"));
      slot.querySelector('[data-source-view="diff"]').setAttribute("aria-pressed", String(view === "diff"));
      slot.querySelector("[data-source-content]").innerHTML = renderSourceContent(snapshot.data, view, readLang());
    });
    return {
      clear() {
        loader.clear();
        opened.clear();
        views.clear();
      },
      restore() {
        root
          .querySelectorAll("details.criterion[data-criterion]")
          .forEach((details) => {
            if (opened.has(details.dataset.criterion)) {
              details.open = true;
              show(details);
            }
          });
      },
    };
  }
  return { renderSource, createSourceLoader, mount };
});
