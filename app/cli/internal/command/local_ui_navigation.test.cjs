const { test } = require("node:test");
const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { runInNewContext } = require("node:vm");
const ui = require("./local_ui_assets/core.js");
const presentation = require("./local_ui_assets/presentation.js");

test("a decision reply cannot take over another route or confirmation", async () => {
  const browser = localReviewBrowser("/?ref=LOCAL-a&tab=decision");
  await browser.flush();
  await browser.action("accept");
  const pending = browser.confirm().onclick();
  browser.dialog.close();
  const workB = browser.navigate("/?ref=LOCAL-b&tab=decision");
  await workB;
  assert.match(browser.app.innerHTML, /<h1[^>]*>Work LOCAL-b<\/h1>/);
  await browser.action("accept");
  const confirmationB = browser.dialog.innerHTML;
  browser.resolveDecision();
  await pending;

  assert.match(browser.app.innerHTML, /<h1[^>]*>Work LOCAL-b<\/h1>/);
  assert.equal(browser.dialog.open, true);
  assert.equal(browser.dialog.innerHTML, confirmationB);
  assert.equal(browser.posted().ref, "LOCAL-a");
  assert.equal(browser.posted().review_id, "review-LOCAL-a");
});

test("an error from an old confirmation does not replace the newer dialog error", async () => {
  const browser = localReviewBrowser("/?ref=LOCAL-a&tab=decision");
  await browser.flush();
  await browser.action("accept");
  const pending = browser.confirm().onclick();
  browser.dialog.close();
  await browser.navigate("/?ref=LOCAL-b&tab=decision");
  await browser.action("accept");
  browser.resolveDecision("A review changed");
  await pending;
  assert.equal(browser.dialog.open, true);
  assert.equal(browser.dialogError().textContent, "");
});

test("a decision reply does not invalidate an in-flight read for the next work", async () => {
  const browser = localReviewBrowser("/?ref=LOCAL-a&tab=decision");
  await browser.flush();
  await browser.action("accept");
  const pending = browser.confirm().onclick();
  browser.dialog.close();
  await browser.navigate("/?ref=LOCAL-b", { delayRead: true });
  assert.match(browser.app.innerHTML, /Connecting to SpecGate/);
  browser.resolveDecision();
  await pending;
  browser.resolveWork();
  await browser.flush();
  assert.match(browser.app.innerHTML, /<h1[^>]*>Work LOCAL-b<\/h1>/);
});

test("an active confirmation still applies the exact response on its own route", async () => {
  const browser = localReviewBrowser("/?ref=LOCAL-a&tab=decision");
  await browser.flush();
  await browser.action("accept");
  const pending = browser.confirm().onclick();
  assert.equal(browser.confirmStatus().textContent, "Saving decision…");
  assert.match(browser.confirm().textContent, /Accept delivery/);
  assert.doesNotMatch(browser.app.innerHTML, /Recorded decision/);
  browser.resolveDecision();
  await pending;
  assert.equal(browser.dialog.open, false, browser.dialogError().textContent);
  assert.match(browser.app.innerHTML, /Recorded decision/);
  assert.match(browser.notice.innerHTML, /Decision recorded\./);
  assert.equal(browser.posted().completion_id, "completion-LOCAL-a");
});

test("a refused decision clears pending feedback and keeps the review available", async () => {
  const browser = localReviewBrowser("/?ref=LOCAL-a&tab=decision");
  await browser.flush();
  await browser.action("accept");
  const pending = browser.confirm().onclick();
  assert.equal(browser.confirmStatus().textContent, "Saving decision…");
  browser.resolveDecision("A review changed");
  await pending;
  assert.equal(browser.confirmStatus().textContent, "");
  assert.equal(browser.dialogError().textContent, "A review changed");
  assert.equal(browser.dialog.open, true);
  assert.doesNotMatch(browser.app.innerHTML, /Recorded decision/);
});

test("refreshing after a basis-only conflict keeps the draft and requires a new confirmation", async () => {
  const browser = localReviewBrowser("/?ref=LOCAL-a&tab=decision");
  await browser.flush();
  browser.enterNote("Keep this draft through a checkout change");
  await browser.action("request-changes");
  const pending = browser.confirm().onclick();
  const original = browser.posted();
  browser.publish("LOCAL-a", "-new", { basisOnly: true });
  browser.resolveDecision("Review changed", 409);
  await pending;
  assert.equal(browser.confirm().textContent, "Refresh review");
  assert.equal(browser.dialogError().textContent, "Review changed");
  await browser.confirm().onclick();
  assert.equal(browser.dialog.open, false);
  assert.match(browser.app.innerHTML, /id="decision-note"[^>]*>Keep this draft through a checkout change<\/textarea>/);
  assert.equal(browser.posted(), original);
  await browser.action("request-changes");
  assert.match(browser.dialog.innerHTML, /basis-LOCAL-a-new/);
  assert.equal(browser.posted(), original);
  const confirmed = browser.confirm().onclick();
  assert.deepEqual(browser.posted(), { ...original, basis_digest: "basis-LOCAL-a-new" });
  browser.resolveDecision();
  await confirmed;
});

test("documents sharing a path show their exact roles and only the selected one is active", async () => {
  const browser = localReviewBrowser("/?ref=LOCAL-a&tab=context");
  await browser.flush();
  await browser.click("document", "1");
  const drawer = () => browser.scope.innerHTML;
  assert.equal(browser.scope.open, true);
  assert.match(drawer(), /data-document="0" class=""[^>]*>.*?shared\.md.*?spec/);
  assert.match(drawer(), /data-document="1" class="active"[^>]*>.*?shared\.md.*?plan/);
  assert.match(drawer(), /Role: plan/);
  assert.match(drawer(), /Content for plan/);
  await browser.click("document", "0");
  assert.match(drawer(), /data-document="0" class="active"/);
  assert.match(drawer(), /data-document="1" class=""/);
  assert.match(drawer(), /Role: spec/);
  assert.match(drawer(), /Content for spec/);
  assert.doesNotMatch(browser.app.innerHTML, /data-document|Role: spec/);
});

test("browser history across tabs preserves the same work's draft", async () => {
  const browser = localReviewBrowser("/?ref=LOCAL-a&tab=decision");
  await browser.flush();
  browser.enterNote("Keep this note while reviewing scope");
  await browser.navigate("/?ref=LOCAL-a&tab=context");
  assert.equal(browser.scope.open, true);
  await browser.navigate("/?ref=LOCAL-a&tab=decision");
  assert.equal(browser.scope.open, false);
  await browser.click("language", "vi");
  assert.match(browser.app.innerHTML, /id="decision-note"[^>]*>Keep this note while reviewing scope<\/textarea>/);
});

test("a known new delivery asks the reviewer to reload before opening a new confirmation", async () => {
  const browser = localReviewBrowser("/?ref=LOCAL-a&tab=decision");
  await browser.flush();
  browser.enterNote("Keep this request");
  browser.publish("LOCAL-a", "-new");
  await browser.poll();
  assert.equal(browser.bellAnimated(), true);
  await browser.action("request-changes");
  assert.match(browser.dialog.innerHTML, /New delivery available/);
  assert.match(browser.dialog.innerHTML, /data-action="view-latest"/);
  assert.doesNotMatch(browser.dialog.innerHTML, /id="dialog-confirm"/);
  assert.equal(browser.posted(), undefined);
});

test("new delivery does not animate the bell with reduced motion", async () => {
  const browser = localReviewBrowser("/?ref=LOCAL-a", "en", true);
  await browser.flush();
  browser.publish("LOCAL-a", "-new");
  await browser.poll();
  assert.match(browser.deliveryHTML(), /View latest/);
  assert.equal(browser.bellAnimated(), false);
});

test("polling announces a newer delivery without replacing the draft or open confirmation", async () => {
  const browser = localReviewBrowser("/?ref=LOCAL-a&tab=decision");
  await browser.flush();
  browser.enterNote("  Check this before merge.\n  ");
  await browser.action("request-changes");
  const before = browser.app.innerHTML;
  const confirmation = browser.dialog.innerHTML;
  browser.publish("LOCAL-a", "-new");
  await browser.poll();
  assert.match(browser.deliveryHTML(), /New delivery available/);
  assert.match(browser.deliveryHTML(), /View latest/);
  assert.equal(browser.app.innerHTML, before);
  assert.equal(browser.dialog.innerHTML, confirmation);
  assert.equal(browser.dialog.open, true);
  assert.equal(browser.posted(), undefined);

  await browser.poll({ error: true });
  assert.match(browser.notificationLabel(), /Work list unavailable/);
  assert.match(browser.deliveryHTML(), /View latest/);
  assert.equal(browser.dialog.innerHTML, confirmation);
  browser.dialog.close();
  await browser.action("view-latest");
  assert.equal(browser.deliveryHTML(), "");
  assert.match(browser.app.innerHTML, /id="decision-note"[^>]*>  Check this before merge\.\n  <\/textarea>/);
  assert.equal(browser.posted(), undefined);
  await browser.action("request-changes");
  assert.match(browser.dialog.innerHTML, /review-LOCAL-a-new/);
  assert.match(browser.dialog.innerHTML, /completion-LOCAL-a-new/);
  assert.match(browser.dialog.innerHTML, /basis-LOCAL-a-new/);
});

test("retrying a failed view-latest read keeps the note on the same work only", async () => {
  const browser = localReviewBrowser("/?ref=LOCAL-a&tab=decision");
  await browser.flush();
  browser.enterNote("Draft before the reload");
  browser.publish("LOCAL-a", "-new");
  await browser.poll();
  assert.match(browser.deliveryHTML(), /View latest/);
  browser.failWorkRead();
  await browser.action("view-latest");
  assert.match(browser.app.innerHTML, /Review read failed/);
  await browser.action("refresh");
  assert.match(browser.app.innerHTML, /id="decision-note"[^>]*>Draft before the reload<\/textarea>/);
  await browser.navigate("/?ref=LOCAL-b&tab=decision");
  assert.doesNotMatch(browser.app.innerHTML, /Draft before the reload/);
});

test("a completion-only change updates the Vietnamese notice without replacing source or settings", async () => {
  const browser = localReviewBrowser("/?ref=LOCAL-a&tab=context", "vi");
  await browser.flush();
  await browser.click("document", "1");
  await browser.action("context-source");
  await browser.action("settings");
  await browser.flush();
  const before = browser.app.innerHTML;
  const drawer = browser.scope.innerHTML;
  const settingsRenders = browser.settingsRenders();
  const sourceActivity = browser.sourceActivity();
  assert.match(drawer, /class="context-raw"/);
  browser.publish("LOCAL-b", "-new");
  await browser.poll();
  assert.equal(browser.deliveryHTML(), "");
  browser.publish("LOCAL-a", "-new", { completionOnly: true });
  await browser.poll();
  assert.match(browser.deliveryHTML(), /Có bản giao mới/);
  assert.match(browser.deliveryHTML(), /Xem bản mới/);
  assert.equal(browser.app.innerHTML, before);
  assert.equal(browser.scope.innerHTML, drawer);
  assert.equal(browser.scope.open, true);
  assert.deepEqual(browser.sourceActivity(), sourceActivity);
  assert.equal(browser.settings.open, true);
  assert.equal(browser.settingsRenders(), settingsRenders);
});

test("late queue observations cannot signal a stale delivery after a route change or explicit reload", async () => {
  const browser = localReviewBrowser("/?ref=LOCAL-a");
  await browser.flush();
  browser.publish("LOCAL-a", "-new");
  await browser.poll({ delay: true });
  await browser.navigate("/?ref=LOCAL-b");
  browser.resolveList();
  await browser.flush();
  assert.equal(browser.deliveryHTML(), "");
  assert.match(browser.app.innerHTML, /<h1[^>]*>Work LOCAL-b<\/h1>/);

  browser.publish("LOCAL-b", "-pending");
  await browser.poll();
  assert.match(browser.deliveryHTML(), /View latest/);
  await browser.poll({ delay: true });
  browser.publish("LOCAL-b", "-new");
  await browser.action("view-latest");
  browser.resolveList();
  await browser.flush();
  assert.equal(browser.deliveryHTML(), "");
});

test("the review shows criteria, evidence and the decision dock together without a tab switch", async () => {
  const browser = localReviewBrowser("/?ref=LOCAL-a");
  await browser.flush();
  const html = browser.app.innerHTML;
  assert.match(html, /class="review enter"/);
  assert.match(html, /<code class="ref">LOCAL-a<\/code>/);
  assert.match(html, /id="crit-list"><ol class="crit-list"/);
  assert.match(html, /id="evidence-pane"><div class="evidence"[^>]*data-criterion="local-1"/);
  assert.match(html, /id="decision-note" maxlength="20000"/);
  assert.match(html, /data-action="request-changes"/);
  assert.match(html, /data-action="accept"/);
  assert.match(html, /id="note-error"/);
  assert.doesNotMatch(html, /class="tabs"|scope-reader|scope-view|Back to review/);
  assert.match(html, /href="\/\?ref=LOCAL-a&amp;tab=context" data-nav data-scope/);
  assert.equal(browser.scope.open, false);
  assert.equal(browser.noteFocused(), false);
});

test("deep links open the review with the scope drawer or focus the decision note", async () => {
  const scope = localReviewBrowser("/?ref=LOCAL-a&tab=context");
  await scope.flush();
  assert.match(scope.app.innerHTML, /id="decision-note"/);
  assert.match(scope.app.innerHTML, /id="crit-list"/);
  assert.equal(scope.scope.open, true);
  assert.match(scope.scope.innerHTML, /<header>[\s\S]*data-close-scope[\s\S]*Approved documents[\s\S]*Content for context/);
  assert.doesNotMatch(scope.scope.innerHTML, /Back to review/);
  const decision = localReviewBrowser("/?ref=LOCAL-a&tab=decision");
  await decision.flush();
  assert.match(decision.app.innerHTML, /class="review/);
  assert.equal(decision.noteFocused(), true);
  assert.equal(decision.scope.open, false);
});

test("the Scope control opens a drawer over the review and every close path returns to the review", async () => {
  const browser = localReviewBrowser("/?ref=LOCAL-a");
  await browser.flush();
  browser.enterNote("Draft kept behind the drawer");
  const review = browser.app.innerHTML;
  await browser.clickLink("/?ref=LOCAL-a&tab=context");
  assert.equal(browser.scope.open, true);
  assert.equal(browser.location.search, "?ref=LOCAL-a&tab=context");
  assert.equal(browser.app.innerHTML, review, "the review stays rendered behind the drawer");
  assert.match(browser.scope.innerHTML, /Content for context/);

  for (const close of [() => browser.closeScope(), () => browser.clickBackdrop(), () => browser.pressEscape()]) {
    const focuses = browser.scopeLinkFocuses();
    await browser.clickLink("/?ref=LOCAL-a&tab=context");
    assert.equal(browser.scope.open, true);
    close();
    await browser.flush();
    assert.equal(browser.scope.open, false);
    assert.equal(browser.location.search, "?ref=LOCAL-a");
    assert.equal(browser.scopeLinkFocuses(), focuses + 1, "focus returns to the Scope control");
    assert.equal(browser.app.innerHTML, review);
  }
  await browser.click("language", "vi");
  assert.match(browser.app.innerHTML, /id="decision-note"[^>]*>Draft kept behind the drawer<\/textarea>/);
});

test("browser Back closes the drawer and focuses the Scope control", async () => {
  const browser = localReviewBrowser("/?ref=LOCAL-a");
  await browser.flush();
  await browser.clickLink("/?ref=LOCAL-a&tab=context");
  const focuses = browser.scopeLinkFocuses();
  await browser.navigate("/?ref=LOCAL-a");
  assert.equal(browser.scope.open, false);
  assert.equal(browser.scopeLinkFocuses(), focuses + 1);
});

test("background updates keep the drawer's scroll position and switching documents resets it", async () => {
  const browser = localReviewBrowser("/?ref=LOCAL-a&tab=context");
  await browser.flush();
  browser.drawerBody.scrollTop = 120;
  await browser.action("context-source");
  assert.equal(browser.drawerBody.scrollTop, 120);
  assert.match(browser.scope.innerHTML, /class="context-raw"/);
  browser.publish("LOCAL-a", "-new");
  await browser.poll();
  assert.equal(browser.scope.open, true);
  assert.equal(browser.drawerBody.scrollTop, 120);
  await browser.click("document", "1");
  assert.equal(browser.drawerBody.scrollTop, 0);
  assert.match(browser.scope.innerHTML, /Content for plan/);
});

test("criterion shortcuts only consume keys that move from the focused criterion", async () => {
  const browser = localReviewBrowser("/?ref=LOCAL-a");
  await browser.flush();
  const selected = () => browser.critList.innerHTML.match(/data-criterion-select="([^"]+)"[^>]*aria-current/)[1];
  const rows = ["local-1", "local-2"].map((criterionSelect) => {
    const button = { tagName: "BUTTON", dataset: { criterionSelect }, focused: false, focus() { this.focused = true; } };
    button.closest = (selector) => selector === "[data-criterion-select]" ? button : null;
    button.getAttribute = () => browser.critList.innerHTML.includes(`data-criterion-select="${criterionSelect}" aria-current`) ? "true" : null;
    button.parentElement = { querySelector: () => button };
    return button;
  });
  rows[0].parentElement.nextElementSibling = rows[1].parentElement;
  rows[1].parentElement.previousElementSibling = rows[0].parentElement;
  assert.equal(browser.key("ArrowDown"), false, "body keeps native scrolling");
  assert.equal(browser.key("j", { tagName: "BUTTON", closest: () => null }), false, "Scope and other controls keep their keys");
  assert.equal(browser.key("ArrowDown", rows[1]), false, "navigation starts at focus, even on an unselected row");
  assert.equal(browser.key("j", rows[0]), true);
  assert.equal(selected(), "local-2");
  assert.match(browser.pane.innerHTML, /data-criterion="local-2"[\s\S]*Second criterion/);
  assert.equal(browser.key("j", rows[0]), true);
  assert.equal(rows[1].focused, true, "focus moves when the adjacent criterion already has selection");
  assert.equal(browser.key("j", rows[1]), false, "the last criterion keeps native scrolling");
  assert.equal(browser.key("ArrowUp", rows[1]), true);
  assert.equal(selected(), "local-1");
  assert.equal(browser.key("k", rows[0]), false);
  assert.equal(browser.key("j", { tagName: "TEXTAREA" }), false);
  browser.dialog.open = true;
  assert.equal(browser.key("j", rows[0]), false);
  browser.dialog.open = false;
  browser.scope.open = true;
  assert.equal(browser.key("j", rows[0]), false);
  browser.scope.open = false;
  assert.equal(selected(), "local-1");
  await browser.app.clickListener({ target: rows[1] });
  assert.equal(selected(), "local-2");
});

test("the queue shows one full-width bar with the workspace, refresh and controls and no second row", async () => {
  const browser = localReviewBrowser("/");
  await browser.flush();
  const html = browser.app.innerHTML;
  assert.equal((html.match(/<header class="navbar"/g) || []).length, 1);
  assert.match(html, /^<header class="navbar"><a class="brand" href="\/" data-nav aria-label="SpecGate">[\s\S]*class="version"[^>]*>fixture<\/span>/);
  assert.match(html, /<span class="workspace-name">Alpha<\/span>/);
  for (const control of ['data-action="refresh"', "data-language=", 'data-action="notifications"', 'data-action="theme"', 'data-action="settings"'])
    assert.ok(html.includes(control), control);
  assert.doesNotMatch(html, /navbar-inner|nav-controls|review-bar|queue-meta|class="seg|class="back"/);
  assert.match(html, /<\/header><main id="main" class="wrap"/);
  assert.match(html, /<div class="q"><header class="queue-head">/);
});

test("the review shows exactly one bar with back, ref, position, neighbours, Scope and controls", async () => {
  const first = localReviewBrowser("/?ref=LOCAL-a");
  await first.flush();
  const html = first.app.innerHTML;
  assert.equal((html.match(/<header class="navbar"/g) || []).length, 1);
  assert.doesNotMatch(html, /review-bar|data-action="refresh"|workspace-name/);
  assert.match(html, /<a class="brand" href="\/" data-nav aria-label="SpecGate">/);
  assert.match(html, /<a class="back" href="\/" data-nav>[\s\S]*Queue<\/a><code class="ref">LOCAL-a<\/code>/);
  assert.match(html, /<span class="pos">1 \/ 2<\/span><a class="ibtn" aria-disabled="true" aria-label="Previous work"[\s\S]*href="\/\?ref=LOCAL-b" data-nav aria-label="Next work"/);
  assert.match(html, /data-scope aria-haspopup="dialog"[\s\S]*data-language=/);
  assert.match(html, /<\/header><main id="main" class="review-main"/);
  const last = localReviewBrowser("/?ref=LOCAL-b");
  await last.flush();
  assert.match(last.app.innerHTML, /<span class="pos">2 \/ 2<\/span><a class="ibtn" href="\/\?ref=LOCAL-a" data-nav aria-label="Previous work">[\s\S]*<a class="ibtn" aria-disabled="true" aria-label="Next work">/);
  const vi = localReviewBrowser("/?ref=LOCAL-a", "vi");
  await vi.flush();
  assert.match(vi.app.innerHTML, /Hàng chờ<\/a>[\s\S]*aria-label="Công việc sau"/);
  const outside = localReviewBrowser("/?ref=LOCAL-z");
  await outside.flush();
  assert.match(outside.app.innerHTML, /<code class="ref">LOCAL-z<\/code>/);
  assert.doesNotMatch(outside.app.innerHTML, /class="pos"|Previous work|Next work/);
});

test("the language toggle switches between English and Vietnamese and persists the choice", async () => {
  const browser = localReviewBrowser("/");
  await browser.flush();
  assert.match(browser.app.innerHTML, /data-language="vi"[^>]*aria-label="Switch to Vietnamese">EN</);
  await browser.click("language", "vi");
  assert.deepEqual(browser.stored, [["specgate-local-language", "vi"]]);
  assert.match(browser.app.innerHTML, /data-language="en"[^>]*aria-label="Chuyển sang tiếng Anh">VI</);
  await browser.click("language", "en");
  assert.deepEqual(browser.stored.at(-1), ["specgate-local-language", "en"]);
  assert.match(browser.app.innerHTML, /data-language="vi"[^>]*>EN</);
});

test("the theme toggle still switches and persists the theme", async () => {
  const browser = localReviewBrowser("/");
  await browser.flush();
  await browser.action("theme");
  assert.deepEqual(browser.stored, [["specgate-local-theme", "dark"]]);
  assert.match(browser.app.innerHTML, /data-action="theme"[^>]*aria-label="Switch to light theme" aria-pressed="true"/);
});

test("queue rows are one line without evidence marks and keep an exact creation time", async () => {
  const browser = localReviewBrowser("/");
  await browser.flush();
  const html = browser.app.innerHTML;
  assert.match(html, /<a class="row" href="\/\?ref=LOCAL-a"/);
  assert.match(html, /<strong class="t" data-title-ref="LOCAL-a">/);
  assert.match(html, /<time class="age" datetime="2026-11-01T00:00:00Z" title="Created /);
  assert.doesNotMatch(html, /ladder|class="why"|tally|crit/);
  const t = (text) => text;
  const rows = presentation.queueList([
    { ref: "A", title: "Plain", state: "awaiting_acceptance", next_actor: "human_reviewer" },
    { ref: "B", title: "Stale", state: "awaiting_acceptance", next_actor: "human_reviewer", stale: true, stale_reason: "Context changed" },
    { ref: "C", title: "Rework", state: "rework_requested", next_actor: "implementing_agent" },
    { ref: "D", title: "Blocked", state: "blocked", next_actor: "maintainer" },
  ], { items: [], filter: "all", search: "", lang: "en" }, t, (ref) => `/?ref=${ref}`);
  assert.deepEqual([...rows.matchAll(/class="why">([^<]*)</g)].map((m) => m[1]), ["Review freshness · Context changed", "Changes requested", "Needs maintainer"]);
});

function localReviewBrowser(path, language = "en", reducedMotion = false) {
  const element = () => ({
    innerHTML: "", textContent: "", style: {}, open: false,
    addEventListener(event, callback) { this[`${event}Listener`] = callback; },
    querySelector() { return element(); }, querySelectorAll() { return []; },
    setAttribute(name, value) { this[name] = value; },
    focus() {}, contains() { return false; },
    showModal() { this.open = true; },
    close() { this.open = false; this.closeListener?.(); },
  });
  const app = element(), dialog = element(), settings = element(), scope = element(), notice = element(), window = element();
  window.scrollTo = () => {};
  const drawerBody = element(), scopeLink = element();
  let scopeLinkFocuses = 0;
  scopeLink.focus = () => { scopeLinkFocuses++; };
  scope.querySelector = (selector) => selector === ".drawer-body" ? drawerBody : element();
  const notification = element(), delivery = element();
  let bellAnimated = false;
  Object.defineProperty(notification, "innerHTML", { set() { bellAnimated = false; } });
  Object.defineProperty(delivery, "firstElementChild", { get: () => delivery.innerHTML || null });
  let confirm, error, status, decision, finish, finishWork, deferredRead, finishList, listOptions, tick, failWorkRead;
  let sourceClears = 0, sourceRestores = 0;
  let settingsRenders = 0;
  const critList = element(), pane = element(), note = element();
  let noteFocused = false;
  note.focus = () => { noteFocused = true; };
  const nodes = { app, dialog, "settings-dialog": settings, "scope-dialog": scope, notice, "crit-list": critList, "evidence-pane": pane, "decision-note": note };
  Object.defineProperty(app, "innerHTML", {
    set(html) {
      this.html = html;
      delivery.innerHTML = html.match(/<div id="delivery-notice"[^>]*><\/div>/) ? "" : null;
    },
    get() { return this.html; },
  });
  app.querySelector = (selector) => selector === ".notification" ? notification
    : selector === "[data-scope]" ? scopeLink
    : selector === '[data-action="notifications"] .icon' ? { animate() { bellAnimated = true; } }
    : element();
  Object.defineProperty(settings, "innerHTML", {
    set(html) { this.html = html; settingsRenders++; },
    get() { return this.html; },
  });
  Object.defineProperty(dialog, "innerHTML", {
    set(html) { this.html = html; confirm = element(); status = element(); error = element(); confirm.textContent = html.match(/id="dialog-confirm">([^<]+)/)?.[1] || ""; },
    get() { return this.html; },
  });
  dialog.querySelector = (selector) => {
    if (selector === "#dialog-confirm") return confirm;
    if (selector === "#confirm-status") return status;
    if (selector === "#dialog-error") return error;
    if (selector === "#attention-content") return null;
    return element();
  };
  const location = new URL(path, "http://127.0.0.1:1234");
  const reply = (data) => ({ ok: true, json: async () => ({ ok: true, data }) });
  const failure = (message) => ({ ok: false, json: async () => ({ ok: false, error: { message } }) });
  const versions = new Map(), stored = [];
  const work = (ref) => ({
    can_decide: true,
    status: {
      ref, state: "awaiting_acceptance", next_actor: "human_reviewer", created_at: "2026-11-01T00:00:00Z",
      review_id: `review-${ref}${versions.get(ref)?.review || ""}`,
      completion_id: `completion-${ref}${versions.get(ref)?.completion || ""}`,
      basis_digest: `basis-${ref}${versions.get(ref)?.basis || ""}`,
    },
    work: { title: `Work ${ref}`, acceptance_criteria: ["First criterion", "Second criterion"] },
    documents: [{ path: "shared.md", role: "spec" }, { path: "shared.md", role: "plan" }],
  });
  const browser = {
    SpecGateUI: ui, SpecGatePresentation: presentation,
    SpecGateSource: { mount: () => ({ clear() { sourceClears++; }, restore() { sourceRestores++; } }) },
    SpecGateMarkdown: { renderContext: (content) => ({ html: content }) },
    document: {
      getElementById: (id) => id === "delivery-notice" && delivery.innerHTML !== null ? delivery : nodes[id],
      querySelector: element, documentElement: { dataset: {} }, activeElement: element(),
    },
    location, URLSearchParams, window,
    history: { pushState(_, __, url) { location.href = new URL(url, location).href; }, replaceState() {} },
    matchMedia: (query) => ({ ...element(), matches: query === "(prefers-reduced-motion: reduce)" && reducedMotion }),
    localStorage: { getItem: (key) => key.endsWith("language") ? language : null, setItem: (key, value) => stored.push([key, value]) },
    setInterval(callback, ms) { if (ms === 30000) tick = callback; },
    setTimeout() {}, clearTimeout() {},
    fetch: async (url, options) => {
      const { pathname, searchParams } = new URL(url, location);
      switch (pathname) {
        case "/api/session": return reply({ csrf_token: "test" });
        case "/api/meta": return reply({ version: "fixture", workspace: { name: "Alpha" } });
        case "/api/works": {
          const { error, delay } = listOptions || {};
          listOptions = null;
          const list = error ? failure("Work list unavailable") : reply({ items: ["LOCAL-a", "LOCAL-b"].map((ref) => work(ref).status) });
          return delay ? await new Promise((resolve) => { finishList = () => resolve(list); }) : list;
        }
        case "/api/update": return reply({ status: "current" });
        case "/api/work": {
          if (failWorkRead) {
            failWorkRead = false;
            return failure("Review read failed");
          }
          const requested = reply(work(searchParams.get("ref")));
          if (!deferredRead) return requested;
          return await new Promise((resolve) => { finishWork = () => resolve(requested); });
        }
        case "/api/context": {
          const role = searchParams.get("role") || "context";
          return reply({ path: searchParams.get("path") || "context.md", role, content: `Content for ${role}` });
        }
        case "/api/decision":
          decision = JSON.parse(options.body);
          return await new Promise((resolve) => { finish = resolve; });
        default: assert.fail(`Unexpected request: ${url}`);
      }
    },
  };
  for (const asset of ["locale.js", "app.js"])
    runInNewContext(readFileSync(`${__dirname}/local_ui_assets/${asset}`, "utf8"), browser);
  const flush = () => new Promise(setImmediate);
  const click = async (kind, value) => {
    await app.clickListener({ target: { closest: (selector) => selector === `[data-${kind}]` ? { dataset: { [kind]: value } } : null } });
    await flush();
  };
  const clickLink = async (href) => {
    await app.clickListener({ ctrlKey: false, preventDefault() {}, target: { closest: (selector) => selector === "a[data-nav]" ? { getAttribute: () => href } : null } });
    await flush();
  };
  return {
    stored, scope, drawerBody, clickLink, scopeLinkFocuses: () => scopeLinkFocuses, critList, pane,
    closeScope: () => scope.clickListener({ target: { closest: (selector) => selector === "[data-close-scope]" ? {} : null } }),
    clickBackdrop: () => scope.clickListener({ target: scope }),
    pressEscape: () => scope.close(), noteFocused: () => noteFocused,
    key: (key, target = { tagName: "BODY" }) => { let prevented = false; window.keydownListener({ key, target, preventDefault() { prevented = true; } }); return prevented; },
    app, dialog, settings, notice, location, click, flush, confirm: () => confirm, confirmStatus: () => status,
    dialogError: () => error, posted: () => decision,
    deliveryHTML: () => delivery.innerHTML || "",
    notificationLabel: () => notification["aria-label"],
    bellAnimated: () => bellAnimated,
    sourceActivity: () => ({ sourceClears, sourceRestores }),
    settingsRenders: () => settingsRenders,
    failWorkRead() { failWorkRead = true; },
    enterNote(value) { app.inputListener({ target: { id: "decision-note", value } }); },
    publish(ref, version, { completionOnly = false, basisOnly = false } = {}) {
      versions.set(ref, { review: completionOnly || basisOnly ? "" : version, completion: basisOnly ? "" : version, basis: version });
    },
    async poll(options) {
      listOptions = options;
      tick();
      await flush();
    },
    resolveList() { finishList(); },
    action: (value) => click("action", value),
    async navigate(url, { delayRead = false } = {}) {
      deferredRead = delayRead;
      location.href = new URL(url, location).href;
      window.popstateListener();
      await flush();
    },
    resolveWork() { finishWork(); },
    resolveDecision(failure, status) {
      finish(failure ? { ok: false, status, json: async () => ({ ok: false, error: { message: failure } }) }
        : reply({ ...work(decision.ref), can_decide: false, review: { human_decision: "approve" } }));
    },
  };
}
