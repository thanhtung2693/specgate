/* Local inspection and human decisions. Work preparation stays in the CLI. */
(() => {
  "use strict";
  const {
    escape: e,
    appearanceControls,
    maintenanceLocation,
    setAppearance,
    criteria,
    recordedDecision,
    decisionPins,
    requestGate,
    needsHuman,
    workAction,
    staleNotice,
  } = SpecGateUI;
  const { icon, stateBadge, evidenceReview } = SpecGatePresentation;
  const app = document.getElementById("app");
  const dialog = document.getElementById("dialog");
  const settings = document.getElementById("settings-dialog");
  const legacySettings = maintenanceLocation(location.href);
  if (legacySettings.openSettings)
    history.replaceState(history.state, "", legacySettings.url);
  let settingsBusy = "";
  const gate = requestGate(),
    queueGate = requestGate(),
    contextGate = requestGate(),
    routeGate = requestGate(),
    dialogGate = requestGate();
  let updateTimer;
  const saved = (key) => {
    try {
      return localStorage.getItem(key);
    } catch {
      return null;
    }
  };
  const persist = (key, value) => {
    try {
      localStorage.setItem(key, value);
    } catch {
      /* Preferences are optional. */
    }
  };
  const state = {
    lang: saved("specgate-local-language") || "vi",
    theme: saved("specgate-local-theme") || "system",
    meta: null,
    items: [],
    work: null,
    context: null,
    update: null,
    error: "",
    queueError: "",
    loading: true,
    filter: "human",
    search: "",
    note: "",
    csrf: "",
    maintenanceError: "",
  };
  const vi = SpecGateLocalVi;
  const t = (text) => (state.lang === "vi" && Object.hasOwn(vi, text) ? vi[text] : text);
  const sourceReader = SpecGateSource.mount(
    app,
    () => state.work,
    api,
    () => state.lang,
  );
  const button = (action, text, primary = false, extra = "") =>
    `<button class="button ${primary ? "primary" : ""}" data-action="${action}" ${extra}>${action === "update-check" ? icon("refresh") : action === "context-source" ? icon("code") : ""}${e(t(text))}</button>`;
  const href = (ref = "", tab = "evidence") => {
    const q = new URLSearchParams();
    if (ref) q.set("ref", ref);
    if (ref && tab !== "evidence") q.set("tab", tab);
    return "/" + (q.size ? "?" + q : "");
  };
  const route = () => {
    const q = new URLSearchParams(location.search);
    return {
      ref: q.get("ref") || "",
      tab: ["evidence", "context", "decision"].includes(q.get("tab"))
        ? q.get("tab")
        : "evidence",
    };
  };
  const actor = (value) =>
    t(
      {
        human_reviewer: "Human reviewer",
        implementing_agent: "Implementing agent",
        maintainer: "Maintainer",
        none: "No action needed",
      }[value] ||
        value ||
        "None",
    );
  const kv = (values) =>
    `<dl class="kv">${values.map(([name, value]) => `<div><dt>${e(t(name))}</dt><dd>${e(value || "—")}</dd></div>`).join("")}</dl>`;
  function applyTheme() {
    const theme =
      state.theme === "system"
        ? matchMedia("(prefers-color-scheme: dark)").matches
          ? "dark"
          : "light"
        : state.theme;
    document.documentElement.dataset.theme = theme;
    document.documentElement.lang = state.lang;
    document.querySelector(".skip").textContent = t("Skip to content");
  }
  const attentionItems = () => state.items.filter(needsHuman);
  const notificationsCount = () =>
    state.loading || state.error || state.queueError ? 0 : attentionItems().length;
  function notificationsText() {
    if (state.error || state.queueError)
      return `${t("Notifications")} · ${t("Work list unavailable")}`;
    const count = notificationsCount();
    return t("Notifications") + (count ? ` (${count})` : "");
  }
  function bell() {
    const count = notificationsCount();
    return icon("bell") + (count ? `<span class="count" aria-hidden="true">${count}</span>` : "");
  }
  function updateAttentionControls() {
    const target = app.querySelector(".notification");
    if (target) {
      target.setAttribute("aria-label", notificationsText());
      target.innerHTML = bell();
    }
    const mobile = settings.querySelector('[data-action="notifications"]');
    if (mobile) mobile.innerHTML = icon("bell") + e(notificationsText());
    const content = dialog.querySelector("#attention-content");
    if (dialog.open && content) {
      const html = notificationsView();
      if (content.innerHTML !== html) {
        if (content.contains(document.activeElement))
          dialog.querySelector("[data-close]").focus();
        content.innerHTML = html;
      }
    }
  }
  const updateAvailable = () => state.update?.status === "available";
  const settingsLabel = () => t("Local tools") + (updateAvailable() ? ` · ${t("Update available")}` : "");
  const gear = () => icon("gear") + (updateAvailable() ? `<span class="update-dot" aria-hidden="true"></span>` : "");
  function updateSettingsIndicator() {
    const target = app.querySelector(".settings-button");
    if (target) {
      target.setAttribute("aria-label", settingsLabel());
      target.innerHTML = gear();
    }
  }
  function render() {
    applyTheme();
    const r = route();
    const theme = document.documentElement.dataset.theme;
    const adjacentControls = `<button class="icon-button notification desktop-notification" data-action="notifications" aria-label="${e(notificationsText())}">${bell()}</button><button class="icon-button settings-button" data-action="settings" aria-label="${e(settingsLabel())}" aria-haspopup="dialog" aria-controls="settings-dialog">${gear()}</button>`;
    app.innerHTML = `<header class="navbar"><div class="navbar-inner"><a class="brand" href="/" data-nav><img src="/assets/logo.svg" alt="" width="32" height="32">SpecGate <span class="version" title="${e(t("Running version"))}">${e(state.meta?.version || "…")}</span></a><div class="nav-controls">${appearanceControls({ lang: state.lang, theme }, { language: t("Language"), dark: t("Switch to dark theme"), light: t("Switch to light theme") }, icon(theme === "dark" ? "sun" : "moon"), adjacentControls)}</div></div></header><div class="workspace-line">${icon("lock")}<strong>${e(state.meta?.workspace?.name || t("Local workspace"))}</strong><span>· Local</span></div><main id="main" tabindex="-1">${state.error ? errorView(state.error) : state.loading ? `<p class="busy" role="status">${e(t("Connecting to SpecGate…"))}</p>` : r.ref ? workView(r) : queueView()}<footer class="footer"><span>${e(t("Claims, test observations and human acceptance remain distinct."))}</span><span>${e(state.meta?.workspace?.slug || "")}</span></footer></main>`;
    sourceReader.restore();
  }

  function errorView(message) {
    return `<section class="error" role="alert"><h1>${e(t("The Local session is unavailable."))}</h1><p>${e(message)}</p><p>${e(t("Refresh to read the workspace again. Decisions require current workspace data."))}</p>${button("refresh", "Retry connection")}</section>`;
  }
  const queueErrorView = (message) =>
    `<p class="warning" role="alert">${e(t("Work list unavailable"))} · ${e(message)}</p>`;
  function queueView() {
    return `<header class="queue-head"><div class="between"><h1>${e(t(notificationsCount() ? "Needs your attention" : "Your work"))}</h1>${refreshButton()}</div></header><div class="queue-tools"><div class="filters">${[
      ["human", "Waiting on you"],
      ["agent", "Agent’s next step"],
      ["all", "All work"],
    ]
      .map(
        ([key, label]) =>
          `<button data-filter="${key}" class="${state.filter === key ? "active" : ""}" aria-pressed="${state.filter === key}">${e(t(label))}<span>${queueCount(key)}</span></button>`,
      )
      .join(
        "",
      )}</div><label class="search">${icon("search")}<span class="visually-hidden">${e(t("Search work or reference"))}</span><input id="search" type="search" value="${e(state.search)}" placeholder="${e(t("Search work or reference"))}"></label></div><div id="queue-results">${queueResults()}</div>`;
  }
  function queueCount(filter) {
    if (state.queueError) return "—";
    if (filter === "human") return notificationsCount();
    if (filter === "all") return state.items.length;
    return state.items.filter((item) => item.next_actor === "implementing_agent").length;
  }
  function queueResults() {
    if (state.queueError) return queueErrorView(state.queueError);
    const items = state.items.filter(
      (item) =>
        (state.filter === "all" ||
          (state.filter === "human"
            ? needsHuman(item)
            : item.next_actor === "implementing_agent")) &&
        `${item.title} ${item.ref}`
          .toLocaleLowerCase()
          .includes(state.search.toLocaleLowerCase()),
    );
    return items.length
      ? `<div class="queue"><div class="work-row queue-labels"><span>${e(t("Work"))}</span><span>${e(t("State"))}</span><span>${e(t("Next action"))}</span></div>${items.map((item) => `<article class="work-row"><div class="work-cell"><code class="work-ref">${e(item.ref)}</code><div class="work-info"><h3><a href="${e(href(item.ref))}" data-nav>${e(item.title)}</a></h3><p>${e(t(item.decision))}${item.stale ? ` · ${e(staleNotice(item, t("Review freshness")))}` : ""}</p></div></div><div>${stateBadge(item, t)}</div><a class="button quiet" href="${e(href(item.ref))}" data-nav>${e(t(workAction(item)))}${icon("arrow")}</a></article>`).join("")}</div><p class="list-count">${e(t("Showing {shown} of {total} work items").replace("{shown}", items.length).replace("{total}", state.items.length))}</p>`
      : `<section class="empty"><h2>${e(t(!state.items.length || (state.filter === "human" && !state.search) ? "No decisions waiting." : "No work matches this view."))}</h2><p>${e(t(!state.items.length ? "Work preparation and approval start with your agent or CLI." : "Try another filter or search."))}</p></section>`;
  }
  const refreshButton = () =>
    `<button data-action="refresh" class="button quiet">${icon("refresh")}${e(t("Refresh"))}</button>`;
  function workView(r) {
    const d = state.work;
    if (!d)
      return `<p class="busy" role="status">${e(t("Connecting to SpecGate…"))}</p>`;
    const s = d.status,
      rows = criteria(s, d.work.acceptance_criteria);
    return `<div class="work-crumb"><a href="/" data-nav>${icon("back")}${e(t("Back to decisions"))}</a><span class="crumb-divider"></span><strong>${e(s.ref)}</strong><span class="muted">· ${e(t("Current delivery"))}</span></div><header class="page-head"><div class="between"><h1>${e(d.work.title)}</h1>${stateBadge(s, t)}</div><p>${e(t("Inspect evidence, compare approved requirements, then decide."))}</p></header><nav class="tabs" aria-label="${e(t("Review details"))}">${[
      ["evidence", "Evidence"],
      ["context", "Approved context"],
      ["decision", "Your decision"],
    ]
      .map(
        ([tab, label]) =>
          `<a href="${e(href(s.ref, tab))}" data-nav class="${r.tab === tab ? "active" : ""}" ${r.tab === tab ? 'aria-current="page"' : ""}>${e(t(label))}</a>`,
      )
      .join(
        "",
      )}</nav>${s.historical_verification ? `<p class="warning">${e(t("Recorded verification comes from another Local store generation."))}</p>` : ""}${s.stale ? `<p class="warning">${e(staleNotice(s, t("Review freshness")))}</p>` : ""}${r.tab === "context" ? contextView() : r.tab === "decision" ? decisionView() : evidenceView(rows)}`;
  }
  function evidenceView(rows) {
    return `${evidenceReview(rows, state.work, state.lang, t)}<div class="review-version">${icon("pin")}<span>${e(t("Decision binds to this delivery."))}</span>${technicalView()}</div>${decisionFooter()}`;
  }
  function technicalView() {
    const s = state.work.status;
    return `<details class="version-details"><summary>${icon("chevron", "chevron")}${e(t("Version details"))}</summary>${kv(
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
      ],
    )}${s.next_command ? `<p>${e(t("Continue in your terminal"))}</p><pre>${e(s.next_command)}</pre>` : ""}</details>`;
  }
  function decisionFooter() {
    const d = state.work;
    return decisionPins(d)
      ? `<footer class="decision-footer"><div><h3>${e(t("You decide this delivery."))}</h3><p>${e(t("Your decision is recorded for the agent’s next step."))}</p></div><div class="actions"><button data-action="go-decision" data-intent="reject">${e(t("Ask for changes"))}</button><button class="primary" data-action="go-decision" data-intent="approve">${icon("check")}${e(t("Accept delivery"))}</button></div></footer>`
      : `<p class="next-actor">${e(actor(d.status.next_actor))}</p>`;
  }
  function contextView() {
    const d = state.work,
      docs = d.documents || [],
      doc = state.context;
    let body = `<p class="busy" role="status">${e(t("Loading approved context…"))}</p>`;
    if (doc?.error)
      body = `<p role="alert" class="warning">${e(doc.error)}</p>`;
    else if (doc?.content !== undefined) {
      let content = `<pre class="context-raw"><code>${e(doc.content)}</code></pre>`;
      if (!doc.showSource) {
        const rendered = SpecGateMarkdown.renderContext(doc.content, {
          generatedQuick:
            doc.selected < 0 &&
            !d.work.artifact_id &&
            doc.path === "context.md" &&
            doc.role === "context",
          workRef: d.status.ref,
        });
        content = `<div class="markdown-body">${rendered.html}</div><details class="technical-context"><summary>${icon("chevron", "chevron")}${e(t("Technical details"))}<span>${e(t("Policy and version identifiers"))}</span></summary>${rendered.technicalHTML ? `<div class="markdown-body">${rendered.technicalHTML}</div>` : ""}<code>${e(doc.digest)}</code></details>`;
      }
      body = `<article class="context-reader"><div class="document-meta">${icon("file")}<strong>${e(doc.path)}</strong>${doc.selected >= 0 ? `<span>${e(t("Role"))}: ${e(doc.role)}</span>` : ""}<span>· ${e(t("Original content is shown without translation."))}</span></div><div id="context-content">${content}</div></article>`;
    }
    return `<div class="document-toolbar"><div class="document-list"><button data-document="-1" class="${(doc?.selected ?? -1) < 0 ? "active" : ""}">${icon("file")}${e(t("Context Pack"))}</button>${docs.map((item, i) => `<button data-document="${i}" class="${doc?.selected === i ? "active" : ""}">${icon("file")}${e(item.path)} · ${e(item.role)}</button>`).join("")}</div>${doc?.content !== undefined ? button("context-source", doc.showSource ? "Formatted view" : "Original source", false, 'aria-controls="context-content"') : ""}</div>${body}<div class="context-footer">${icon("pin")}<span>${e(t("Approved context"))}</span><a class="button" href="${e(href(d.status.ref))}" data-nav>${e(t("Back to evidence"))}</a></div>`;
  }
  function decisionView() {
    const d = state.work,
      s = d.status,
      pins = decisionPins(d),
      recorded = recordedDecision(d);
    if (recorded)
      return `<section class="decision-form"><div class="recorded-notice ${recorded.decision === "approve" ? "success" : "warning"}">${icon("check")}<div><h2>${e(t("Recorded decision"))}</h2><p>${e(t(recorded.decision === "approve" ? "Accepted" : "Rejected"))} · ${e(actor(s.next_actor))}</p></div></div>${recorded.note ? `<h3>${e(t("Your note"))}</h3><blockquote class="recorded-note">${e(recorded.note)}</blockquote>` : ""}${technicalView()}${kv(
        [
          ["Actor", recorded.actor],
          ["Decision recorded at", recorded.observed_at],
        ].filter(([, value]) => value),
      )}</section>`;
    if (!pins)
      return `<section class="decision-form"><h2>${e(t("Decision unavailable for this work."))}</h2><p>${e(t(s.decision))}</p><p class="muted">${e(t("This work is not currently actionable in the browser."))}</p></section>`;
    return `<section class="decision-form"><h2>${e(t("Your delivery decision"))}</h2><p class="muted">${icon("pin")}${e(t("Acceptance applies to the exact review and evidence you inspected."))}</p><div class="decision-snapshot"><strong>${e(t("Current delivery"))}</strong>${technicalView()}</div><label for="decision-note">${e(t("Your note"))}</label><p class="field-hint">${e(t("Required when requesting changes."))}</p><textarea id="decision-note" maxlength="20000" placeholder="${e(t("What should change, or why are you accepting this delivery?"))}">${e(state.note)}</textarea><p class="field-hint">${e(t("The agent reads your decision and note through SpecGate."))}</p><p id="note-error" class="dialog-error" role="alert"></p><div class="decision-actions"><button data-action="request-changes">${e(t("Ask for changes"))}</button><button class="primary" data-action="accept">${icon("check")}${e(t("Accept delivery"))}</button></div><p class="field-hint">${e(t("You will confirm before saving the decision."))}</p></section>`;
  }
  function updateLabel() {
    return t(
      {
        unchecked: "Not checked",
        available: "Update available",
        current: "Up to date",
        disabled: "Self-update unavailable",
        updated: "Update installed",
        failed: "Update failed",
        running: "Update running",
      }[state.update?.status] || "Not checked",
    );
  }
  function toolsView() {
    return `<p class="settings-description">${e(t("For this machine’s Local installation only."))}</p>${state.maintenanceError ? `<p role="alert" class="warning">${e(state.maintenanceError)}</p>` : ""}<section class="settings-section"><h3>${icon("download")}${e(t("Version"))}<span class="badge neutral">${e(updateLabel())}</span></h3><div class="installed-version"><span>${e(t("Running version"))}</span><code>${e(state.meta?.version)}</code>${state.update?.latest_version ? `<span>${e(t("Available version"))} <strong>${e(state.update.latest_version)}</strong></span>` : ""}</div>${state.update?.reason ? `<p class="muted">${e(t(state.update.reason))}</p>` : ""}${state.update?.restart_required ? `<p class="warning">${e(t("Restart the Local UI to use the installed version."))}</p>` : ""}<div class="settings-action-row"><p>${e(t("Check the official release before updating."))}</p><div class="actions">${updateAvailable() ? button("update-preview", settingsBusy === "update-preview" ? "Working…" : "Review update", true, settingsBusy === "update-preview" ? "disabled" : "") : ""}${button("update-check", settingsBusy === "update-check" ? "Working…" : state.update?.latest_version ? "Check again" : "Check for updates", false, state.update?.status === "running" || settingsBusy === "update-check" ? "disabled" : "")}</div></div></section><section class="settings-section"><h3>${icon("cache")}${e(t("Cache & temporary files"))}</h3><div class="settings-action-row"><div><p>${e(t("Only SpecGate-owned update-check cache is eligible."))}</p><p class="muted">${e(t("Work, specs, evidence, acceptance records and backups are kept."))}</p></div>${button("cache-preview", settingsBusy === "cache-preview" ? "Working…" : "Preview cleanup", false, settingsBusy === "cache-preview" ? "disabled" : "")}</div><p class="settings-safety">${icon("lock")}${e(t("Review the scope and confirm before changes."))}</p></section><footer class="settings-footer"><button data-action="close-settings">${e(t("Close"))}</button></footer>`;
  }
  async function api(path, body) {
    const response = await fetch(path, {
      method: body === undefined ? "GET" : "POST",
      credentials: "same-origin",
      headers:
        body === undefined
          ? {}
          : {
              "Content-Type": "application/json",
              "X-SpecGate-CSRF": state.csrf,
            },
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    });
    let result;
    try {
      result = await response.json();
    } catch {
      throw Error(t("Request failed."));
    }
    if (!response.ok || result.ok !== true)
      throw Error(
        result.error?.message ||
          (response.status === 401
            ? t("Session expired. Restart or reopen the Local UI.")
            : t("Request failed.")),
      );
    return result.data;
  }
  async function load() {
    sourceReader.clear();
    const ticket = gate.start();
    const queueTicket = queueGate.start();
    contextGate.start();
    state.loading = true;
    state.error = "";
    state.work = null;
    state.context = null;
    state.note = "";
    render();
    try {
      if (!state.csrf) state.csrf = (await api("/api/session")).csrf_token;
      const [meta, list] = await Promise.all([
        api("/api/meta"),
        api("/api/works"),
      ]);
      if (!gate.current(ticket)) return;
      state.meta = meta;
      if (queueGate.current(queueTicket)) {
        state.items = list.items || [];
        state.queueError = "";
      }
      const r = route();
      if (r.ref) {
        const d = await api("/api/work?" + new URLSearchParams({ ref: r.ref }));
        if (!gate.current(ticket)) return;
        state.work = d;
      }
      state.loading = false;
      render();
      updateAttentionControls();
      if (r.tab === "context" && r.ref) loadContext(-1);
      loadUpdate(false);
    } catch (error) {
      if (!gate.current(ticket)) return;
      state.loading = false;
      state.error = error.message;
      render();
      updateAttentionControls();
    }
  }
  async function loadContext(index) {
    const d = state.work;
    if (!d) return;
    const ticket = contextGate.start(),
      ref = d.status.ref,
      doc = index >= 0 ? d.documents[index] : null;
    const query = new URLSearchParams({ ref });
    if (doc) {
      query.set("path", doc.path);
      query.set("role", doc.role);
    }
    state.context = { selected: index };
    render();
    try {
      const result = await api("/api/context?" + query);
      if (!contextGate.current(ticket) || state.work?.status.ref !== ref)
        return;
      state.context = { ...result, selected: index };
      render();
    } catch (error) {
      if (contextGate.current(ticket)) {
        state.context = { error: error.message, selected: index };
        render();
      }
    }
  }
  function renderSettings() {
    if (!settings.open || dialog.open) return;
    const focusAction = settings.contains(document.activeElement)
      ? document.activeElement.closest("[data-action]")?.dataset.action
      : null;
    const closeFocused =
      settings.querySelector("[data-close-settings]") ===
      document.activeElement;
    const scroll = settings.scrollTop;
    settings.innerHTML = `<header><h2 id="settings-title">${e(t("Local tools"))}</h2><button class="icon-button" data-close-settings aria-label="${e(t("Close dialog"))}">${icon("close")}</button></header><button class="button settings-notifications" data-action="notifications">${icon("bell")}${e(notificationsText())}</button>${toolsView()}`;
    settings.querySelector("[data-close-settings]").onclick = () =>
      settings.close();
    settings.scrollTop = scroll;
    if (closeFocused)
      settings
        .querySelector("[data-close-settings]")
        ?.focus({ preventScroll: true });
    else if (focusAction)
      settings
        .querySelector(`[data-action="${focusAction}"]`)
        ?.focus({ preventScroll: true });
  }
  function openSettings() {
    const opening = !settings.open;
    if (opening) settings.showModal();
    renderSettings();
    if (opening) settings.querySelector("[data-close-settings]")?.focus();
    loadUpdate(false);
  }
  settings.addEventListener("close", () =>
    app.querySelector(".settings-button")?.focus({ preventScroll: true }),
  );
  dialog.addEventListener("close", renderSettings);
  async function loadUpdate(check) {
    try {
      state.update = await api(
        check ? "/api/update/check" : "/api/update",
        check ? {} : undefined,
      );
      state.maintenanceError = "";
      updateSettingsIndicator();
      renderSettings();
      if (state.update.status === "running") watchUpdate();
    } catch (error) {
      if (check || settings.open) {
        state.maintenanceError = error.message;
        renderSettings();
      }
    }
  }
  function watchUpdate() {
    clearTimeout(updateTimer);
    updateTimer = setTimeout(async () => {
      try {
        state.update = await api("/api/update");
        updateSettingsIndicator();
        renderSettings();
        if (state.update.status === "running") watchUpdate();
        else
          toast(
            state.update.status === "updated"
              ? t("Restart the Local UI to use the installed version.")
              : state.update.reason || updateLabel(),
          );
      } catch (error) {
        state.maintenanceError = error.message;
        renderSettings();
        toast(error.message);
      }
    }, 1000);
  }
  function go(url, reload = false) {
    routeGate.start();
    const before = route();
    history.pushState(null, "", url);
    const after = route();
    if (!reload && before.ref === after.ref && state.work) {
      render();
      if (after.tab === "context" && !state.context) loadContext(-1);
    } else load();
    document.getElementById("main")?.focus({ preventScroll: true });
    window.scrollTo(0, 0);
  }
  function showDialog(title, content, onConfirm) {
    const ticket = dialogGate.start(), active = () => dialog.open && dialogGate.current(ticket);
    dialog.innerHTML = `<header><h2 id="dialog-title">${e(t(title))}</h2><button class="icon-button" data-close aria-label="${e(t("Close dialog"))}">${icon("close")}</button></header>${content}<p class="dialog-error" role="alert" id="dialog-error"></p>${onConfirm ? `<div class="decision-actions">${button("dialog-cancel", "Cancel")}<button class="button primary" id="dialog-confirm">${e(t(title === "Review the update" ? "Confirm update" : title === "Preview cache cleanup" ? "Remove this cache" : "Confirm"))}</button></div>` : ""}`;
    dialog.querySelector("[data-close]").onclick = () => dialog.close();
    dialog
      .querySelector('[data-action="dialog-cancel"]')
      ?.addEventListener("click", () => dialog.close());
    if (onConfirm)
      dialog.querySelector("#dialog-confirm").onclick = async () => {
        const buttons = [...dialog.querySelectorAll("button")];
        buttons.forEach((b) => (b.disabled = true));
        dialog.querySelector("#dialog-confirm").textContent = t("Working…");
        try {
          await onConfirm(active);
          if (active()) dialog.close();
        } catch (error) {
          if (!active()) return;
          dialog.querySelector("#dialog-error").textContent = error.message;
          buttons.forEach((b) => (b.disabled = false));
          dialog.querySelector("#dialog-confirm").textContent = t("Confirm");
        }
      };
    dialog.showModal();
  }
  function confirmDecision(decision) {
    const pins = decisionPins(state.work);
    if (!pins) return;
    const note = state.note.trim();
    if (decision === "reject" && !note) {
      document.getElementById("note-error").textContent = t(
        "A note is required when requesting changes.",
      );
      document.getElementById("decision-note").focus();
      return;
    }
    const ticket = routeGate.start(), inspected = state.work;
    showDialog(
      decision === "approve"
        ? "Confirm acceptance"
        : "Confirm request for changes",
      `<p>${e(t("Record this decision for the exact review shown below."))}</p>${kv(
        [
          ["Work", state.work.work.title],
          ["Review ID", pins.review_id],
          ["Completion ID", pins.completion_id],
          ["Current evidence basis", pins.basis_digest],
          ["Your note", note],
        ],
      )}<p>${e(t("If the review or evidence changed, SpecGate will refuse this decision. Refresh and inspect it again."))}</p>`,
      async (active) => {
        const result = await api("/api/decision", { ...pins, decision, note });
        if (state.work === inspected && routeGate.current(ticket) && active()) {
          contextGate.start();
          sourceReader.clear();
          state.work = result;
          state.items = state.items.map((item) => item.ref === result.status.ref ? { ...item, ...result.status } : item);
          state.note = "";
          state.context = null;
          render();
          toast(t("Decision recorded."));
        }
        refreshQueue();
      },
    );
  }
  async function updatePreview() {
    try {
      const preview = await api("/api/update");
      state.update = preview;
      updateSettingsIndicator();
      renderSettings();
      if (preview.status !== "available" || !preview.confirmation_token) {
        toast(preview.reason || updateLabel());
        return;
      }
      showDialog(
        "Review the update",
        `${kv([
          ["Running version", state.meta.version],
          ["Available version", preview.latest_version],
          ["Install target", preview.target],
        ])}<ul>${(preview.scope || []).map((line) => `<li>${e(t(line))}</li>`).join("")}</ul><p>${e(t("The running UI keeps its current version until restarted. Finish any open decision before updating."))}</p>`,
        async () => {
          state.update = await api("/api/update", {
            version: preview.latest_version,
            confirmation_token: preview.confirmation_token,
          });
          updateSettingsIndicator();
          renderSettings();
          if (state.update.status === "running") {
            watchUpdate();
            toast(t("Update running"));
          } else if (state.update.status === "updated")
            toast(t("Restart the Local UI to use the installed version."));
          else throw Error(state.update.reason || t("Update failed"));
        },
      );
    } catch (error) {
      toast(error.message);
    }
  }
  async function cachePreview() {
    try {
      const preview = await api("/api/cache");
      const entries = preview.entries || [];
      showDialog(
        "Preview cache cleanup",
        `<p>${e(t(preview.scope))}</p>${entries.length ? `<ul class="file-list">${entries.map((file) => `<li><strong>${e(file.name)}</strong><code>${e(file.path)}</code><span>${e(file.bytes)} bytes</span></li>`).join("")}</ul><p>${e(t("Total"))}: ${e(preview.count)} ${e(t("Files"))} · ${e(preview.bytes)} bytes</p>` : `<p>${e(t(preview.reason || "No eligible cache files."))}</p>`}<p>${e(t("Work, specs, evidence, acceptance records and backups are kept."))}</p>`,
        entries.length
          ? async () => {
              await api("/api/cache", {
                preview_digest: preview.preview_digest,
              });
              toast(t("Cache cleared."));
            }
          : null,
      );
    } catch (error) {
      toast(error.message);
    }
  }
  function notificationsView() {
    if (state.error || state.queueError)
      return queueErrorView(state.error || state.queueError);
    if (state.loading)
      return `<p role="status">${e(t("Connecting to SpecGate…"))}</p>`;
    const items = attentionItems();
    return items.length
      ? items.map((item) => `<a class="notice-item" href="${e(href(item.ref))}" data-dialog-nav><strong>${e(item.title)}</strong><small>${e(actor(item.next_actor))} · ${e(t(workAction(item)))} · ${e(item.ref)}</small></a>`).join("")
      : `<p>${e(t("Nothing needs your attention."))}</p>`;
  }
  dialog.addEventListener("click", (event) => {
    const link = event.target.closest("[data-dialog-nav]");
    if (!link || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey)
      return;
    event.preventDefault();
    dialog.close();
    if (settings.open) settings.close();
    go(link.getAttribute("href"), true);
  });
  function toast(message) {
    const notice = document.getElementById("notice");
    notice.textContent = message;
    notice.style.display = "block";
    clearTimeout(toast.timer);
    toast.timer = setTimeout(() => (notice.style.display = "none"), 6000);
  }
  async function refreshQueue() {
    if (!state.csrf || document.hidden) return;
    const ticket = queueGate.start();
    try {
      const list = await api("/api/works");
      if (!queueGate.current(ticket)) return;
      state.items = list.items || [];
      state.queueError = "";
    } catch (error) {
      if (!queueGate.current(ticket)) return;
      state.queueError = error.message || t("Request failed.");
    }
    const results = document.getElementById("queue-results");
    const activeElement = document.activeElement;
    if (results && !dialog.open && !settings.open && !app.contains(activeElement)) {
      render();
    } else if (results) {
      app.querySelector(".queue-head h1").textContent = t(
        notificationsCount() ? "Needs your attention" : "Your work",
      );
      app.querySelectorAll("[data-filter] span").forEach((count) => {
        count.textContent = queueCount(count.parentElement.dataset.filter);
      });
      const html = queueResults();
      if (results.innerHTML !== html) {
        if (results.contains(activeElement))
          document.getElementById("search").focus({ preventScroll: true });
        results.innerHTML = html;
      }
    }
    updateAttentionControls();
  }
  async function handleClick(event) {
    const language = event.target.closest("[data-language]");
    if (language) {
      if (
        setAppearance(state, "language", language.dataset.language, persist)
      ) {
        render();
        app
          .querySelector(`[data-language="${state.lang}"]`)
          ?.focus({ preventScroll: true });
      }
      return;
    }

    const link = event.target.closest("a[data-nav]");
    if (link) {
      if (event.ctrlKey || event.metaKey || event.shiftKey || event.altKey)
        return;
      event.preventDefault();
      go(link.getAttribute("href"));
      return;
    }
    const filter = event.target.closest("[data-filter]");
    if (filter) {
      state.filter = filter.dataset.filter;
      render();
      return;
    }
    const doc = event.target.closest("[data-document]");
    if (doc) {
      loadContext(Number(doc.dataset.document));
      return;
    }
    const target = event.target.closest("[data-action]");
    if (!target) return;
    const action = target.dataset.action;
    if (action === "theme") {
      setAppearance(
        state,
        "theme",
        document.documentElement.dataset.theme === "dark" ? "light" : "dark",
        persist,
      );
      render();
      app.querySelector(".theme-button")?.focus({ preventScroll: true });
      return;
    }
    if (action === "close-settings") {
      settings.close();
      return;
    }
    if (action === "context-source") {
      state.context.showSource = !state.context.showSource;
      render();
      app.querySelector('[data-action="context-source"]')?.focus({ preventScroll: true });
      return;
    }
    if (action === "go-decision") {
      go(href(state.work.status.ref, "decision"));
      if (target.dataset.intent === "reject")
        document
          .getElementById("decision-note")
          ?.focus({ preventScroll: true });
      return;
    }
    if (action === "settings") {
      openSettings();
      return;
    }
    if (action === "refresh") {
      await load();
      return;
    }
    if (action === "notifications") {
      showDialog("Needs your attention", `<div id="attention-content">${notificationsView()}</div>`);
      return;
    }
    if (action === "copy-link") {
      try {
        await navigator.clipboard.writeText(
          location.origin + href(state.work.status.ref, route().tab),
        );
        toast(t("Link copied."));
      } catch {
        toast(t("Clipboard unavailable. Copy the address from your browser."));
      }
      return;
    }
    if (action === "accept" || action === "request-changes") {
      confirmDecision(action === "accept" ? "approve" : "reject");
      return;
    }
    if (["update-check", "update-preview", "cache-preview"].includes(action)) {
      if (settingsBusy) return;
      settingsBusy = action;
      renderSettings();
      try {
        if (action === "update-check") await loadUpdate(true);
        if (action === "update-preview") await updatePreview();
        if (action === "cache-preview") await cachePreview();
      } finally {
        settingsBusy = "";
        renderSettings();
      }
    }
  }
  app.addEventListener("click", handleClick);
  settings.addEventListener("click", handleClick);
  app.addEventListener("input", (event) => {
    if (event.target.id === "search") {
      state.search = event.target.value;
      document.getElementById("queue-results").innerHTML = queueResults();
    }
    if (event.target.id === "decision-note") state.note = event.target.value;
  });
  window.addEventListener("popstate", () => {
    routeGate.start();
    load();
  });
  matchMedia("(prefers-color-scheme: dark)").addEventListener(
    "change",
    applyTheme,
  );
  setInterval(() => {
    if (!document.hidden && !state.loading && !state.error) {
      refreshQueue();
      loadUpdate(false);
    }
  }, 30000);
  load().then(() => {
    if (legacySettings.openSettings) openSettings();
  });
})();
