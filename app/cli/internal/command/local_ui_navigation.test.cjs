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
  assert.match(browser.app.innerHTML, /data-document="0" class=""[^>]*>.*?shared\.md.*?spec/);
  assert.match(browser.app.innerHTML, /data-document="1" class="active"[^>]*>.*?shared\.md.*?plan/);
  assert.match(browser.app.innerHTML, /Role: plan/);
  assert.match(browser.app.innerHTML, /Content for plan/);
  await browser.click("document", "0");
  assert.match(browser.app.innerHTML, /data-document="0" class="active"/);
  assert.match(browser.app.innerHTML, /data-document="1" class=""/);
  assert.match(browser.app.innerHTML, /Role: spec/);
  assert.match(browser.app.innerHTML, /Content for spec/);
});

test("browser history across tabs preserves the same work's draft", async () => {
  const browser = localReviewBrowser("/?ref=LOCAL-a&tab=decision");
  await browser.flush();
  browser.enterNote("Keep this note while reviewing scope");
  await browser.navigate("/?ref=LOCAL-a&tab=context");
  await browser.navigate("/?ref=LOCAL-a&tab=decision");
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
  const settingsRenders = browser.settingsRenders();
  const sourceActivity = browser.sourceActivity();
  assert.match(before, /class="context-raw"/);
  browser.publish("LOCAL-b", "-new");
  await browser.poll();
  assert.equal(browser.deliveryHTML(), "");
  browser.publish("LOCAL-a", "-new", { completionOnly: true });
  await browser.poll();
  assert.match(browser.deliveryHTML(), /Có bản giao mới/);
  assert.match(browser.deliveryHTML(), /Xem bản mới/);
  assert.equal(browser.app.innerHTML, before);
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
  const app = element(), dialog = element(), settings = element(), notice = element(), window = element();
  const notification = element(), delivery = element();
  let bellAnimated = false;
  Object.defineProperty(notification, "innerHTML", { set() { bellAnimated = false; } });
  Object.defineProperty(delivery, "firstElementChild", { get: () => delivery.innerHTML || null });
  let confirm, error, status, decision, finish, finishWork, deferredRead, finishList, listOptions, tick, failWorkRead;
  let sourceClears = 0, sourceRestores = 0;
  let settingsRenders = 0;
  const nodes = { app, dialog, "settings-dialog": settings, notice };
  Object.defineProperty(app, "innerHTML", {
    set(html) {
      this.html = html;
      delivery.innerHTML = html.match(/<div id="delivery-notice"[^>]*><\/div>/) ? "" : null;
    },
    get() { return this.html; },
  });
  app.querySelector = (selector) => selector === ".notification" ? notification
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
  const versions = new Map();
  const work = (ref) => ({
    can_decide: true,
    status: {
      ref, state: "awaiting_acceptance", next_actor: "human_reviewer",
      review_id: `review-${ref}${versions.get(ref)?.review || ""}`,
      completion_id: `completion-${ref}${versions.get(ref)?.completion || ""}`,
      basis_digest: `basis-${ref}${versions.get(ref)?.basis || ""}`,
    },
    work: { title: `Work ${ref}` },
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
    matchMedia: (query) => ({ ...element(), matches: query === "(prefers-reduced-motion: reduce)" && reducedMotion }),
    localStorage: { getItem: (key) => key.endsWith("language") ? language : null },
    setInterval(callback, ms) { if (ms === 30000) tick = callback; },
    setTimeout() {}, clearTimeout() {},
    fetch: async (url, options) => {
      const { pathname, searchParams } = new URL(url, location);
      switch (pathname) {
        case "/api/session": return reply({ csrf_token: "test" });
        case "/api/meta": return reply({ version: "fixture" });
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
  return {
    app, dialog, settings, notice, click, flush, confirm: () => confirm, confirmStatus: () => status,
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
