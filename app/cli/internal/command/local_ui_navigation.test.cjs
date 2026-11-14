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
  assert.match(browser.app.innerHTML, /<h1>Work LOCAL-b<\/h1>/);
  await browser.action("accept");
  const confirmationB = browser.dialog.innerHTML;
  browser.resolveDecision();
  await pending;

  assert.match(browser.app.innerHTML, /<h1>Work LOCAL-b<\/h1>/);
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
  assert.match(browser.app.innerHTML, /<h1>Work LOCAL-b<\/h1>/);
});

test("an active confirmation still applies the exact response on its own route", async () => {
  const browser = localReviewBrowser("/?ref=LOCAL-a&tab=decision");
  await browser.flush();
  await browser.action("accept");
  const pending = browser.confirm().onclick();
  browser.resolveDecision();
  await pending;
  assert.equal(browser.dialog.open, false, browser.dialogError().textContent);
  assert.match(browser.app.innerHTML, /Recorded decision/);
  assert.equal(browser.posted().completion_id, "completion-LOCAL-a");
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

function localReviewBrowser(path) {
  const element = () => ({
    innerHTML: "", textContent: "", style: {}, open: false,
    addEventListener(event, callback) { this[`${event}Listener`] = callback; },
    querySelector() { return element(); }, querySelectorAll() { return []; },
    setAttribute() {}, focus() {}, contains() { return false; },
    showModal() { this.open = true; },
    close() { this.open = false; this.closeListener?.(); },
  });
  const app = element(), dialog = element(), settings = element(), notice = element(), window = element();
  let confirm, error, decision, finish, finishWork, deferredRead;
  const nodes = { app, dialog, "settings-dialog": settings, notice };
  Object.defineProperty(dialog, "innerHTML", {
    set(html) { this.html = html; confirm = element(); error = element(); },
    get() { return this.html; },
  });
  dialog.querySelector = (selector) => {
    if (selector === "#dialog-confirm") return confirm;
    if (selector === "#dialog-error") return error;
    if (selector === "#attention-content") return null;
    return element();
  };
  const location = new URL(path, "http://127.0.0.1:1234");
  const reply = (data) => ({ ok: true, json: async () => ({ ok: true, data }) });
  const work = (ref) => ({
    can_decide: true,
    status: { ref, state: "awaiting_acceptance", next_actor: "human_reviewer", review_id: `review-${ref}`, completion_id: `completion-${ref}` },
    work: { title: `Work ${ref}` },
    documents: [{ path: "shared.md", role: "spec" }, { path: "shared.md", role: "plan" }],
  });
  const browser = {
    SpecGateUI: ui, SpecGatePresentation: presentation,
    SpecGateSource: { mount: () => ({ clear() {}, restore() {} }) },
    SpecGateMarkdown: { renderContext: (content) => ({ html: content }) },
    document: { getElementById: (id) => nodes[id], querySelector: element, documentElement: { dataset: {} }, activeElement: element() },
    location, URLSearchParams, window, matchMedia: element,
    localStorage: { getItem: (key) => key.endsWith("language") ? "en" : null },
    setInterval() {}, setTimeout() {}, clearTimeout() {},
    fetch: async (url, options) => {
      const { pathname, searchParams } = new URL(url, location);
      switch (pathname) {
        case "/api/session": return reply({ csrf_token: "test" });
        case "/api/meta": return reply({ version: "fixture" });
        case "/api/works": return reply({ items: [] });
        case "/api/update": return reply({ status: "current" });
        case "/api/work": {
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
    app, dialog, click, flush, confirm: () => confirm, dialogError: () => error, posted: () => decision,
    action: (value) => click("action", value),
    async navigate(url, { delayRead = false } = {}) {
      deferredRead = delayRead;
      location.href = new URL(url, location).href;
      window.popstateListener();
      await flush();
    },
    resolveWork() { finishWork(); },
    resolveDecision(failure) {
      finish(failure ? { ok: false, json: async () => ({ ok: false, error: { message: failure } }) }
        : reply({ ...work(decision.ref), can_decide: false, review: { human_decision: "approve" } }));
    },
  };
}
