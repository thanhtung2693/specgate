/* Local inspection and human decisions. Work preparation stays in the CLI. */
(() => {
  "use strict";
  const {
    escape: e,
    appearanceControls,
    maintenanceLocation,
    setAppearance,
    criteria,
    decisionPins,
    requestGate,
    needsHuman,
    workAction,
    staleNotice,
  } = SpecGateUI;
  const { icon, statusPresentation, evidenceReview, technicalView, decisionView, contextView, toolsView, updateLabel, kv } = SpecGatePresentation;
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
  let updateTimer, loadedRef;
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
    newDelivery: false,
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
  const settingsLabel = () => t("Settings") + (updateAvailable() ? ` · ${t("Update available")}` : "");
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
    const notifications = `<button type="button" class="icon-button notification" data-action="notifications" aria-label="${e(notificationsText())}" aria-haspopup="dialog">${bell()}</button>`;
    const settingsControl = `<button type="button" class="icon-button settings-button" data-action="settings" aria-label="${e(settingsLabel())}" aria-haspopup="dialog" aria-controls="settings-dialog">${gear()}</button>`;
    app.innerHTML = `<header class="navbar"><div class="navbar-inner wrap"><a class="brand" href="/" data-nav><img src="/assets/logo.svg" alt="" width="26" height="26">SpecGate <span class="version" title="${e(t("Running version"))}">${e(state.meta?.version || "…")}</span></a><div class="nav-controls">${appearanceControls({ lang: state.lang, theme }, { language: t("Language"), dark: t("Switch to dark theme"), light: t("Switch to light theme") }, icon(theme === "dark" ? "sun" : "moon"), notifications, settingsControl)}</div></div></header><main id="main" class="wrap" tabindex="-1"><div class="view">${state.error ? errorView(state.error) : state.loading ? `<p class="busy" role="status">${e(t("Connecting to SpecGate…"))}</p>` : r.ref ? workView(r) : queueView()}</div></main>`;
    updateDeliveryNotice();
    sourceReader.restore();
  }
  function updateDeliveryNotice() {
    const target = document.getElementById("delivery-notice");
    if (!target || Boolean(target.firstElementChild) === state.newDelivery) return;
    target.innerHTML = state.newDelivery
      ? `<div class="banner"><span>${icon("bell")}<span><strong>${e(t("New delivery available"))}</strong> ${e(t("Your draft and current evidence stay in place."))}</span></span><button class="button accent" type="button" data-action="view-latest">${e(t("View latest"))}</button></div>`
      : "";
    if (state.newDelivery && !matchMedia("(prefers-reduced-motion: reduce)").matches)
      app.querySelector('[data-action="notifications"] .icon')?.animate?.(
        [{ rotate: "0deg" }, { rotate: "16deg" }, { rotate: "-13deg" }, { rotate: "8deg" }, { rotate: "0deg" }],
        { duration: 650 },
      );
  }
  function transition(change, ref) {
    if (!document.startViewTransition || matchMedia("(prefers-reduced-motion: reduce)").matches) {
      change();
      return;
    }
    const highlight = () => {
      const node = ref && [...app.querySelectorAll("[data-title-ref]")].find((title) => title.dataset.titleRef === ref);
      if (node) node.style.viewTransitionName = "work-title";
      return node;
    };
    const oldTitle = highlight();
    document.startViewTransition(() => {
      if (oldTitle) oldTitle.style.viewTransitionName = "";
      change();
      highlight();
    });
  }
  function errorView(message) {
    return `<section class="error" role="alert"><h1>${e(t("The Local session is unavailable."))}</h1><p>${e(message)}</p><p>${e(t("Refresh to read the workspace again. Decisions require current workspace data."))}</p>${button("refresh", "Retry connection")}</section>`;
  }
  const queueErrorView = (message) =>
    `<p class="warning" role="alert">${e(t("Work list unavailable"))} · ${e(message)}</p>`;
  const queueTitle = () => t({
    human: "Waiting on you",
    agent: "Agent’s next step",
    all: "All work",
  }[state.filter]);
  const workspace = () => `<span class="workspace-name">${icon("folder")}<span>${e(state.meta?.workspace?.name || t("Local workspace"))} · Local</span></span>`;
  const queueHeading = () => e(queueTitle()) + (state.filter === "human" ? `<span class="queue-number">${queueCount("human")}</span>` : "");
  function queueView() {
    return `<header class="queue-head"><h1>${queueHeading()}</h1><div class="queue-tools"><div class="filters seg" role="group" aria-label="${e(t("Filter work"))}">${[
      ["human", "Waiting on you"],
      ["agent", "Agent’s next step"],
      ["all", "All work"],
    ].map(([key, label]) =>
      `<button type="button" data-filter="${key}" aria-pressed="${state.filter === key}">${e(t(label))}<span>${queueCount(key)}</span></button>`,
    ).join("")}</div><input class="search" id="search" type="search" value="${e(state.search)}" aria-label="${e(t("Search work or reference"))}" placeholder="${e(t("Search work or reference"))}"></div></header><div class="queue-meta">${workspace()}${refreshButton()}</div><div id="queue-results">${queueResults()}</div>`;
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
    const count = `<p class="list-count">${e(t("Showing {shown} of {total} work items").replace("{shown}", items.length).replace("{total}", state.items.length))}</p>`;
    return items.length
      ? `<ul class="queue">${items.map((item) => {
          const status = statusPresentation(item);
          const why = item.stale ? staleNotice(item, t("Review freshness")) : t(workAction(item));
          return `<li><a class="work-row ${item.completion_id ? "delivered" : ""}" href="${e(href(item.ref))}" data-nav><span class="tile ${status.tone}">${icon(status.icon)}</span><span><strong class="work-title" data-title-ref="${e(item.ref)}">${e(item.title)}</strong><span class="work-reason">${e(why)} · <code>${e(item.ref)}</code></span></span><span class="work-state">${e(t(status.label))}</span>${icon("arrow", "row-arrow")}</a></li>`;
        }).join("")}</ul>${count}`
      : `<section class="empty">${icon("inbox")}<h2>${e(t(!state.search && state.filter === "human" ? "No decisions waiting." : "No work matches this view."))}</h2><p>${e(t(!state.items.length ? "Work preparation and approval start with your agent or CLI." : "Try another filter or search."))}</p></section>${count}`;
  }
  const refreshButton = () =>
    `<button data-action="refresh" class="button quiet">${icon("refresh")}${e(t("Refresh"))}</button>`;
  function workView(r) {
    const d = state.work;
    if (!d)
      return `<p class="busy" role="status">${e(t("Connecting to SpecGate…"))}</p>`;
    const s = d.status,
      rows = criteria(s, d.work.acceptance_criteria);
    const status = statusPresentation(s);
    const gaps = rows.filter((row) => row.gaps?.length);
    const shortDate = d.review?.created_at && new Date(d.review.created_at).toLocaleDateString(state.lang, { dateStyle: "medium" });
    const action = needsHuman(s) && gaps.length
      ? t("Review the evidence gaps before deciding.")
      : state.work.can_decide
        ? t("Inspect evidence, compare approved requirements, then decide.")
        : t(workAction(s));
    return `<a class="back" href="/" data-nav>${icon("back")}${e(t("Back to decisions"))}</a><header class="page-head"><span class="tile ${status.tone}">${icon(status.icon)}</span><div><h1 data-title-ref="${e(s.ref)}">${e(d.work.title)}</h1><div class="work-meta"><code>${e(s.ref)}</code><span>${e(t(status.label))}${shortDate ? ` · ${e(shortDate)}` : ""}${d.completion?.agent?.name ? ` · ${e(d.completion.agent.name)}` : ""}</span>${technicalView(s, t)}</div></div><p class="work-need">${e(action)}</p></header><div id="delivery-notice" role="status" aria-live="polite"></div><nav class="tabs" aria-label="${e(t("Review details"))}">${[
      ["evidence", "Evidence", "evidence"],
      ["context", "Scope", "file"],
      ["decision", "Decision", "person"],
    ].map(([tab, label, glyph]) =>
      `<a href="${e(href(s.ref, tab))}" data-nav ${r.tab === tab ? 'aria-current="page"' : ""}>${icon(glyph)}${e(t(label))}</a>`,
    ).join("")}</nav><div class="tab-content">${s.historical_verification ? `<p class="warning">${e(t("Recorded verification comes from another Local store generation."))}</p>` : ""}${s.stale ? `<p class="warning">${e(staleNotice(s, t("Review freshness")))}</p>` : ""}${r.tab === "context" ? contextView(d, state.context, t, SpecGateMarkdown.renderContext, button) : r.tab === "decision" ? decisionView(d, state.note, actor, t, rows, state.lang) : evidenceView(rows)}</div>`;
  }
  function evidenceView(rows) {
    const d = state.work;
    return `${evidenceReview(rows, d, state.lang, t)}${decisionPins(d) ? `<div class="evidence-footer"><a class="button primary" href="${e(href(d.status.ref, "decision"))}" data-nav>${e(t("Review decision"))}${icon("arrow")}</a></div>` : ""}`;
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
        { cause: response.status },
      );
    return result.data;
  }
  async function load({ keepNote = false, motionRef = "" } = {}) {
    const r = route();
    sourceReader.clear();
    const ticket = gate.start();
    const queueTicket = queueGate.start();
    contextGate.start();
    state.loading = true;
    state.error = "";
    state.work = null;
    state.context = null;
    state.newDelivery = false;
    if (!keepNote || loadedRef !== r.ref) state.note = "";
    loadedRef = r.ref;
    if (!motionRef) render();
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
      if (r.ref) {
        const d = await api("/api/work?" + new URLSearchParams({ ref: r.ref }));
        if (!gate.current(ticket)) return;
        state.work = d;
      }
      state.loading = false;
      const updatePage = () => {
        if (!gate.current(ticket)) return;
        render();
        updateAttentionControls();
        if (r.tab === "context" && r.ref) loadContext(-1);
        if (motionRef || keepNote) document.getElementById("main")?.focus({ preventScroll: true });
      };
      if (motionRef) transition(updatePage, motionRef);
      else updatePage();
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
    settings.innerHTML = `<header><h2 id="settings-title">${icon("gear")}${e(t("Settings"))}</h2><button class="icon-button" data-close-settings aria-label="${e(t("Close dialog"))}">${icon("close")}</button></header>${toolsView(state, settingsBusy, t, button)}`;
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
      const update = await api(
        check ? "/api/update/check" : "/api/update",
        check ? {} : undefined,
      );
      const changed = state.maintenanceError || JSON.stringify(state.update) !== JSON.stringify(update);
      state.update = update;
      state.maintenanceError = "";
      if (changed) {
        updateSettingsIndicator();
        renderSettings();
      }
      if (state.update.status === "running") watchUpdate();
    } catch (error) {
      if ((check || settings.open) && state.maintenanceError !== error.message) {
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
              : state.update.reason || updateLabel(state.update, t),
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
    navigate(before.ref, { reload, animate: true });
    window.scrollTo(0, 0);
  }
  function navigate(beforeRef, { reload = false, animate = false } = {}) {
    const after = route();
    if (!reload && beforeRef === after.ref && state.work) {
      const updateTab = () => {
        render();
        if (after.tab === "context" && !state.context) loadContext(-1);
        document.getElementById("main")?.focus({ preventScroll: true });
      };
      if (animate) transition(updateTab);
      else updateTab();
      return;
    }
    const motionRef = animate && (!beforeRef || !after.ref) ? beforeRef || after.ref : "";
    load({ keepNote: beforeRef === after.ref, motionRef });
    if (!motionRef) document.getElementById("main")?.focus({ preventScroll: true });
  }
  function showDialog(title, content, onConfirm, pendingLabel = "Working…", onConflict) {
    const ticket = dialogGate.start(), active = () => dialog.open && dialogGate.current(ticket);
    const confirmLabel = title === "Review the update" ? "Confirm update" : title === "Preview cache cleanup" ? "Remove this cache" : title === "Confirm acceptance" ? "Accept delivery" : title === "Confirm request for changes" ? "Ask for changes" : "Confirm";
    dialog.className = title === "Needs your attention" ? "attention-dialog" : "";
    dialog.innerHTML = `<header><h2 id="dialog-title">${icon(title === "Needs your attention" ? "bell" : title === "Preview cache cleanup" ? "folder" : title === "Review the update" ? "download" : "person")}${e(t(title === "Needs your attention" ? "Notifications" : title))}</h2><button class="icon-button" data-close aria-label="${e(t("Close dialog"))}">${icon("close")}</button></header>${content}<p class="dialog-error" role="alert" id="dialog-error"></p>${onConfirm ? `<p id="confirm-status" class="field-hint" role="status"></p><div class="decision-actions">${button("dialog-cancel", "Cancel")}<button class="button primary" id="dialog-confirm">${e(t(confirmLabel))}</button></div>` : ""}`;
    dialog.querySelector("[data-close]").onclick = () => dialog.close();
    dialog
      .querySelector('[data-action="dialog-cancel"]')
      ?.addEventListener("click", () => dialog.close());
    if (onConfirm)
      dialog.querySelector("#dialog-confirm").onclick = async () => {
        const buttons = [...dialog.querySelectorAll("button")];
        buttons.forEach((b) => (b.disabled = true));
        const pending = dialog.querySelector("#confirm-status");
        dialog.querySelector("#dialog-error").textContent = "";
        pending.textContent = t(pendingLabel);
        try {
          await onConfirm(active);
          if (active()) dialog.close();
        } catch (error) {
          if (!active()) return;
          dialog.querySelector("#dialog-error").textContent = error.message;
          buttons.forEach((b) => (b.disabled = false));
          pending.textContent = "";
          if (error.cause === 409 && onConflict) {
            const confirm = dialog.querySelector("#dialog-confirm");
            confirm.textContent = t("Refresh review");
            confirm.onclick = () => { dialog.close(); return onConflict(); };
            confirm.focus();
          }
        }
      };
    dialog.showModal();
  }
  function confirmDecision(decision) {
    const pins = decisionPins(state.work);
    if (!pins) return;
    if (state.newDelivery) {
      showDialog("New delivery available", `<p>${e(t("Inspect the new evidence before confirming a decision."))}</p><div class="decision-actions">${button("view-latest", "View latest", true)}</div>`);
      return;
    }
    const note = state.note.trim();
    if (decision === "reject" && !note) {
      document.getElementById("note-error").textContent = t(
        "A note is required when requesting changes.",
      );
      const field = document.getElementById("decision-note");
      field.setAttribute("aria-invalid", "true");
      field.focus();
      if (!matchMedia("(prefers-reduced-motion: reduce)").matches)
        field.animate?.([{ translate: "-5px" }, { translate: "4px" }, { translate: "0" }], 230);
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
        ], t,
      )}<p>${e(t("If the review or evidence changed, SpecGate will refuse this decision. Refresh and inspect it again."))}</p>`,
      async (active) => {
        const result = await api("/api/decision", { ...pins, decision, note });
        if (state.work === inspected && routeGate.current(ticket) && active()) {
          contextGate.start();
          sourceReader.clear();
          state.work = result;
          state.newDelivery = false;
          state.items = state.items.map((item) => item.ref === result.status.ref ? { ...item, ...result.status } : item);
          state.note = "";
          state.context = null;
          render();
          toast(t("Decision recorded."), "success");
        }
        refreshQueue();
      },
      "Saving decision…",
      () => state.work === inspected && routeGate.current(ticket) && load({ keepNote: true }),
    );
  }
  async function updatePreview() {
    try {
      const preview = await api("/api/update");
      state.update = preview;
      updateSettingsIndicator();
      renderSettings();
      if (preview.status !== "available" || !preview.confirmation_token) {
        toast(preview.reason || updateLabel(state.update, t));
        return;
      }
      showDialog(
        "Review the update",
        `${kv([
          ["Running version", state.meta.version],
          ["Available version", preview.latest_version],
          ["Install target", preview.target],
        ], t)}<ul>${(preview.scope || []).map((line) => `<li>${e(t(line))}</li>`).join("")}</ul><p>${e(t("The running UI keeps its current version until restarted. Finish any open decision before updating."))}</p>`,
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
    if (event.target === dialog && dialog.className === "attention-dialog") dialog.close();
    if (event.target.closest('[data-action="view-latest"]')) return handleClick(event);
    const link = event.target.closest("[data-dialog-nav]");
    if (!link || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey)
      return;
    event.preventDefault();
    dialog.close();
    if (settings.open) settings.close();
    go(link.getAttribute("href"), true);
  });
  function toast(message, tone) {
    const notice = document.getElementById("notice");
    notice.innerHTML = `${tone === "success" ? icon("check-circle") : ""}${e(message)}`;
    notice.style.display = "block";
    clearTimeout(toast.timer);
    toast.timer = setTimeout(() => (notice.style.display = "none"), 6000);
  }
  async function refreshQueue() {
    if (!state.csrf || document.hidden) return;
    const ticket = queueGate.start();
    const inspected = state.work;
    const ref = route().ref;
    try {
      const list = await api("/api/works");
      if (!queueGate.current(ticket)) return;
      state.items = list.items || [];
      state.queueError = "";
      if (inspected && state.work === inspected && route().ref === ref) {
        const status = inspected.status;
        const latest = state.items.find((item) => item.ref === status.ref);
        state.newDelivery = !!(
          (latest?.review_id && latest.review_id !== status.review_id) ||
          (latest?.completion_id && latest.completion_id !== status.completion_id)
        );
      }
    } catch (error) {
      if (!queueGate.current(ticket)) return;
      state.queueError = error.message || t("Request failed.");
    }
    const results = document.getElementById("queue-results");
    const activeElement = document.activeElement;
    if (results && !dialog.open && !settings.open && !app.contains(activeElement)) {
      render();
    } else if (results) {
      app.querySelector(".queue-head h1").innerHTML = queueHeading();
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
    updateDeliveryNotice();
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
      app.querySelector(`[data-filter="${state.filter}"]`)?.focus({ preventScroll: true });
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
    if (action === "context-source") {
      state.context.showSource = !state.context.showSource;
      render();
      app.querySelector('[data-action="context-source"]')?.focus({ preventScroll: true });
      return;
    }
    if (action === "settings") {
      openSettings();
      return;
    }
    if (action === "refresh") {
      await load({ keepNote: Boolean(state.error) });
      return;
    }
    if (action === "view-latest" && state.newDelivery) {
      if (dialog.open) dialog.close();
      await load({ keepNote: true });
      return;
    }
    if (action === "notifications") {
      showDialog("Needs your attention", `<div id="attention-content">${notificationsView()}</div>`);
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
    if (event.target.id === "decision-note") {
      state.note = event.target.value;
      if (state.note.trim()) {
        event.target.removeAttribute?.("aria-invalid");
        const error = document.getElementById("note-error");
        if (error) error.textContent = "";
      }
    }
  });
  window.addEventListener("popstate", () => {
    routeGate.start();
    navigate(loadedRef);
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
