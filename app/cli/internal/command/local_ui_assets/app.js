/* Local inspection and human decisions. Work preparation stays in the CLI. */
(() => {
  "use strict";
  const {
    escape: e,
    maintenanceLocation,
    setAppearance,
    criteria,
    decisionPins,
    requestGate,
    needsHuman,
  } = SpecGateUI;
  const { icon, technicalView, contextView, drawerFrame, decisionConfirmation, updateReview, toolsView, updateLabel, rungs, criteriaList, selectedEvidence, reviewScreen, queueScreen, topBar, queueTitle, errorScreen, queueError, noticeItems, queueResults: renderQueueResults, cacheBody, withRequiredTests } = SpecGatePresentation;
  const app = document.getElementById("app");
  const dialog = document.getElementById("dialog");
  const settings = document.getElementById("settings-dialog");
  const scope = document.getElementById("scope-dialog");
  const legacySettings = maintenanceLocation(location.href);
  if (legacySettings.openSettings)
    history.replaceState(history.state, "", legacySettings.url);
  let settingsBusy = "";
  const gate = requestGate(),
    queueGate = requestGate(),
    contextGate = requestGate(),
    routeGate = requestGate(),
    dialogGate = requestGate();
  let updateTimer, loadedRef, enteredWork;
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
    selected: "",
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
  const reviewing = (r) => Boolean(r.ref && !state.error && !state.loading && state.work);
  const scopeWanted = () => reviewing(route()) && route().tab === "context";
  const focusScope = () => app.querySelector("[data-scope]")?.focus({ preventScroll: true });
  function render() {
    applyTheme();
    const r = route();
    const theme = document.documentElement.dataset.theme;
    const notifications = `<button type="button" class="ibtn notification" data-action="notifications" aria-label="${e(notificationsText())}" aria-haspopup="dialog">${bell()}</button>`;
    const settingsControl = `<button type="button" class="ibtn settings-button" data-action="settings" aria-label="${e(settingsLabel())}" aria-haspopup="dialog" aria-controls="settings-dialog">${gear()}</button>`;
    const bar = topBar({ t, href, lang: state.lang, theme, notifications, settings: settingsControl, version: state.meta?.version || "…", workspace: state.meta?.workspace?.name, ref: reviewing(r) ? r.ref : "", scopeHref: href(r.ref, "context"), waiting: attentionItems().map((item) => item.ref) });
    app.innerHTML = `${bar}<main id="main" class="${reviewing(r) ? "review-main" : "wrap"}" tabindex="-1"><div class="view">${state.error ? errorView(state.error) : state.loading ? `<p class="busy" role="status">${e(t("Connecting to SpecGate…"))}</p>` : r.ref ? workView(r) : queueView()}</div></main>`;
    updateDeliveryNotice();
    sourceReader.restore();
    renderScope();
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
  const errorView = (message) => errorScreen(message, t, button("refresh", "Retry connection"));
  const queueErrorView = (message) => queueError(message, t);
  const queueHeading = () => e(queueTitle(state.filter, t));
  const queueResults = () => renderQueueResults(state, t, href);
  const queueView = () => queueScreen(state, ["human", "agent", "all"].map((key) => queueCount(key)), queueResults(), t);
  function queueCount(filter) {
    if (state.queueError) return "—";
    if (filter === "human") return notificationsCount();
    return filter === "all" ? state.items.length : state.items.filter((item) => item.next_actor === "implementing_agent").length;
  }
  const rowId = (row) => row.criterion_id || row.id;
  const reviewRows = (d) => withRequiredTests(criteria(d.status, d.work.acceptance_criteria), d);
  function workView(r) {
    const d = state.work;
    if (!d)
      return `<p class="busy" role="status">${e(t("Connecting to SpecGate…"))}</p>`;
    const rows = reviewRows(d);
    const selected = rows.find((row) => rowId(row) === state.selected) || rows.find((row) => rungs(row).problem) || rows[0];
    state.selected = selected ? rowId(selected) : "";
    const enter = enteredWork !== d;
    enteredWork = d;
    return reviewScreen(d, rows, { selected: state.selected, enter, note: state.note, lang: state.lang, t, actor });
  }
  function select(id) {
    const d = state.work;
    const list = document.getElementById("crit-list"), pane = document.getElementById("evidence-pane");
    if (!d || !list || !pane || id === state.selected) return;
    const rows = reviewRows(d);
    if (!rows.some((row) => rowId(row) === id)) return;
    state.selected = id;
    list.innerHTML = criteriaList(rows, id, state.lang, t);
    pane.innerHTML = selectedEvidence(rows, d, id, state.lang, t);
    pane.scrollTop = 0;
    sourceReader.restore();
    const current = [...app.querySelectorAll("[data-criterion-select]")].find((node) => node.dataset.criterionSelect === id);
    current?.focus({ preventScroll: true });
    current?.scrollIntoView?.({ block: "nearest" });
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
    if (loadedRef !== r.ref) state.selected = "";
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
        if (motionRef || keepNote || r.tab === "decision") focusRoute(r);
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
    renderScope(true);
    try {
      const result = await api("/api/context?" + query);
      if (!contextGate.current(ticket) || state.work?.status.ref !== ref)
        return;
      state.context = { ...result, selected: index };
      renderScope();
    } catch (error) {
      if (contextGate.current(ticket)) {
        state.context = { error: error.message, selected: index };
        renderScope();
      }
    }
  }
  /* The drawer sits outside #app, so review re-renders never touch it. Keep its scroll and focus across its own updates. */
  function renderScope(reset = false) {
    if (!scopeWanted()) {
      if (scope.open) scope.close();
      return;
    }
    const at = document.activeElement;
    const kept = scope.contains(at) && at.closest("[data-document],[data-action],[data-close-scope]");
    const focus = kept && (kept.dataset.document !== undefined ? `[data-document="${kept.dataset.document}"]` : kept.dataset.action ? `[data-action="${kept.dataset.action}"]` : "[data-close-scope]");
    const scroll = reset ? 0 : scope.querySelector(".drawer-body")?.scrollTop || 0;
    const opening = !scope.open;
    scope.innerHTML = drawerFrame(t("Scope"), t("Close dialog"), contextView(state.work, state.context, t, SpecGateMarkdown.renderContext, button));
    if (opening) scope.showModal();
    scope.querySelector(".drawer-body").scrollTop = scroll;
    if (focus || opening) scope.querySelector(focus || "[data-close-scope]")?.focus({ preventScroll: true });
  }
  scope.addEventListener("click", (event) => {
    if (event.target === scope || event.target.closest("[data-close-scope]")) scope.close();
    else handleClick(event);
  });
  scope.addEventListener("close", () => {
    if (scope.open || !scopeWanted()) return;
    go(href(state.work.status.ref), false, false);
    focusScope();
  });
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
  const focusRoute = (r) =>
    ((r.tab === "decision" && document.getElementById("decision-note")) || document.getElementById("main"))?.focus({ preventScroll: true });
  function go(url, reload = false, animate = true) {
    routeGate.start();
    const before = route();
    history.pushState(null, "", url);
    navigate(before.ref, { reload, animate });
    if (before.ref !== route().ref) window.scrollTo(0, 0);
  }
  function navigate(beforeRef, { reload = false, animate = false } = {}) {
    const after = route();
    if (!reload && beforeRef === after.ref && state.work) {
      const closing = scope.open;
      renderScope();
      if (after.tab === "context" && !state.context) loadContext(-1);
      else if (after.tab !== "context") focusRoute(after);
      if (closing && after.tab === "evidence") focusScope();
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
      decisionConfirmation(state.work, pins, note, t),
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
        updateReview(state.meta.version, preview, t),
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
        cacheBody(preview, t),
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
    return noticeItems(attentionItems(), t, href, actor);
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
          .querySelector("[data-language]")
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
    const criterion = event.target.closest("[data-criterion-select]");
    if (criterion) {
      select(criterion.dataset.criterionSelect);
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
      renderScope();
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
  window.addEventListener("keydown", (event) => {
    const step = { j: 1, ArrowDown: 1, k: -1, ArrowUp: -1 }[event.key];
    if (!step || event.ctrlKey || event.metaKey || event.altKey || event.shiftKey) return;
    if (!state.work || !reviewing(route()) || dialog.open || settings.open || scope.open) return;
    const row = event.target?.closest?.("[data-criterion-select]")?.parentElement;
    const next = (step > 0 ? row?.nextElementSibling : row?.previousElementSibling)?.querySelector("[data-criterion-select]");
    if (!next) return;
    event.preventDefault();
    if (next.getAttribute("aria-current") === "true") next.focus();
    else select(next.dataset.criterionSelect);
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
