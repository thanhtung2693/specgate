import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { test } from "node:test";
import { runInNewContext } from "node:vm";

const here = new URL(".", import.meta.url);
const read = (path) => readFileSync(new URL(path, here), "utf8");
const html = read("index.html");
const css = read("styles.css");
const js = read("script.js");

test("the published stage stands alone with three sheets and a readable review", () => {
  assert.equal([...html.matchAll(/class="layer\b/g)].length, 3);
  assert.equal([...html.matchAll(/class="shard\b/g)].length, 3);
  assert.match(html, /id="scene" data-phase="2"/);
  assert.match(html, /DB down → 503[\s\S]*?Test result missing/);
  assert.match(html, /href="#main"[^>]*>Skip to content/);
  assert.match(html, /<main id="main" tabindex="-1">/);
  assert.match(html, /class="ctl-bar" hidden/);
  assert.match(html, /class="reveal"/);
  assert.match(css, /\.motion \.reveal:not\(\.in\)/);
  assert.match(css, /prefers-reduced-motion: reduce/);
  assert.doesNotMatch(html, /<iframe\b|ui\.html|ui-fixtures\.js|127\.0\.0\.1/);
  assert.doesNotMatch(css, /body\s*\{[^}]*overflow-x:/);
  const steps = [...html.matchAll(/<button class="step"[^>]*>/g)];
  assert.equal(steps.length, 3);
  for (const [i, [step]] of steps.entries()) {
    assert.match(step, new RegExp(`role="tab" id="step-${i}" aria-label="[^"]+" data-i18n-attr="aria-label:st\\.${i}" aria-controls="scene"`));
  }
});

test("install panels match the documented Local and IDE setup", () => {
  const guide = read("../../docs/using-specgate/quickstart.md");
  const ide = read("../../docs/using-specgate/guides/install-ide-plugins.md");
  for (const [id, doc] of [["cli", guide], ["codex", ide], ["claude", ide], ["cursor", ide]]) {
    const block = html.match(new RegExp(`id="code-${id}"[^>]*>(.*?)</pre>`, "s"))?.[1];
    assert.ok(block);
    for (const line of block.split("\n")) assert.ok(doc.includes(line), line);
    assert.match(html, new RegExp(`data-copy="code-${id}"`));
  }
  for (const href of html.matchAll(/href="#([^"]+)"/g)) {
    if (href[1].startsWith("i-")) continue;
    assert.ok(html.includes(`id="${href[1]}"`), href[0]);
  }
  assert.equal([...html.matchAll(/class="btn lg primary[^"]*" href="#install"/g)].length, 2);
  assert.match(html, /guides\/local-review\.md/);
  const choices = [...html.matchAll(/<button type="button" data-ins="([^"]+)"([^>]+)>(.*?)<\/button>/g)];
  const names = { cli: "CLI", codex: "Codex", claude: "Claude Code", cursor: "Cursor", prompt: "Ask your agent" };
  const icons = {
    cli: '<svg class="ic" aria-hidden="true"><use href="#i-agent" /></svg>',
    codex: '<img src="assets/brands/codex.png" alt="" width="22" height="22" loading="lazy" decoding="async" />',
    claude: '<img src="assets/brands/claude.svg" alt="" width="22" height="22" />',
    cursor: '<img class="theme-light" src="assets/brands/cursor-light.svg" alt="" width="22" height="22" />'
      + '<img class="theme-dark" src="assets/brands/cursor-dark.svg" alt="" width="22" height="22" />',
    prompt: '<svg class="ic" aria-hidden="true"><use href="#i-word" /></svg>',
  };
  assert.deepEqual(choices.map(([, id]) => id), Object.keys(names));
  for (const [, id, attrs, icon] of choices) {
    assert.ok(attrs.includes(`aria-label="${names[id]}"`), id);
    assert.ok(attrs.includes(`title="${names[id]}"`), id);
    assert.equal(icon.trim(), icons[id], id);
  }
  assert.match(choices.at(-1)[2], /data-i18n-attr="aria-label,title:ins.tabPrompt"/);
  assert.match(css, /#ins-tabs button:focus-visible::after \{[^}]*content: attr\(title\)/);
  assert.match(html, /data-install-note="codex"[^>]*>.*?<code>\/plugins<\/code>.*?<code>specgate@specgate<\/code>/);
  assert.doesNotMatch(html.match(/id="code-codex"[^>]*>(.*?)<\/pre>/s)[1], /\/plugins/);
  assert.match(html, /guides\/install-ide-plugins\.md/);
  assert.match(html, /quickstart\.md/);
  assert.doesNotMatch(html + js, /specgate stats|full mode|production-ready|trusted by|guarantee|automatically detects|official (?:Codex|Claude|plugin) marketplace|approved (?:by|for) (?:OpenAI|Anthropic|Codex|Claude)/i);
});

test("every localizable label has a Vietnamese translation", () => {
  const strings = runInNewContext(js.slice(0, js.indexOf("const root =")) + "strings");
  for (const [, key] of html.matchAll(/data-i18n(?:-attr)?="(?:[\w,-]+:)?([\w.]+)"/g)) {
    assert.ok(strings.vi[key], key);
  }
  for (const key of Object.keys(strings.en)) assert.ok(strings.vi[key], key);
});

test("the landing and README open the sample UI demo and keep real setup separate", () => {
  const rootReadme = read("../../README.md");
  assert.match(html, /<a href="demo\/" data-i18n="cta.demo">Try the UI demo<\/a>/);
  assert.ok(existsSync(new URL("demo/index.html", here)));
  assert.match(rootReadme, /href="https:\/\/thanhtung2693\.github\.io\/specgate\/demo\/"[^>]*><strong>Try the UI demo<\/strong>/);
  const details = html.match(/<details id="delivery-proof">([\s\S]*?)<\/details>/)?.[1];
  assert.ok(details);
  assert.match(details, /href="demo\/"[^>]+>Try the UI demo/);
  assert.match(details, /guides\/local-review\.md/);
  assert.match(rootReadme, /try-delivery-proof\.md/);
  assert.match(read("README.md"), /node scripts\/build-landing-demo\.mjs/);
  assert.doesNotMatch(html + js + css + rootReadme + read("README.md"), /<video\b|Watch 15s|specgate-promo|HyperFrames/i);
});

test("metadata keeps the deployed canonical URL and structured public copy", () => {
  assert.match(html, /<meta name="robots" content="index, follow, max-image-preview:large"/);
  assert.match(html, /<link rel="canonical" href="https:\/\/thanhtung2693\.github\.io\/specgate\/"/);
  assert.match(html, /property="og:image" content="https:\/\/thanhtung2693\.github\.io\/specgate\/logo\.svg"/);
  assert.match(html, /name="twitter:image" content="https:\/\/thanhtung2693\.github\.io\/specgate\/logo\.svg"/);
  assert.match(read("sitemap.xml"), /<lastmod>\d{4}-\d{2}-\d{2}<\/lastmod>/);
  const graph = JSON.parse(html.match(/<script type="application\/ld\+json">([\s\S]*?)<\/script>/)[1])["@graph"];
  for (const type of ["Organization", "WebSite", "WebPage", "SoftwareApplication", "FAQPage"]) assert.ok(graph.some((item) => item["@type"] === type));
  const faq = graph.find((item) => item["@type"] === "FAQPage").mainEntity;
  const section = html.slice(html.indexOf('id="faq"'), html.indexOf('<section class="final'));
  assert.equal((section.match(/<details\b/g) ?? []).length, 3);
  assert.equal(faq.length, 3);
  for (const question of faq) {
    assert.ok(section.includes(question.name));
    assert.ok(section.replace(/<\/?code>/g, "").includes(question.acceptedAnswer.text));
  }
});

test("fonts and icons are bundled with no runtime network dependency", () => {
  assert.doesNotMatch(html, /<(script|img|video|source|iframe)\b[^>]*src="https?:/);
  assert.doesNotMatch(css + js, /@import|url\(\s*["']?https?:|\bfetch\(|XMLHttpRequest|WebSocket|sendBeacon/);
  for (const [, ref] of html.matchAll(/(?:href|src|poster)="([^"#][^"]*)"/g)) {
    if (!ref.startsWith("https:")) assert.ok(existsSync(new URL(ref, here)), ref);
  }
  for (const [, ref] of css.matchAll(/url\("([^"]+)"\)/g)) assert.ok(existsSync(new URL(ref, here)), ref);
  for (const [, icon] of (html + js).matchAll(/#(i-[\w-]+)/g)) assert.ok(html.includes(`<symbol id="${icon}"`), icon);
  for (const font of ["host-grotesk-latin", "geist", "geist-vietnamese", "commit-mono"]) assert.match(css, new RegExp(`fonts/${font}\\.woff2`));
});

test("muted, warning and success text meet contrast requirements in both themes", () => {
  const luminance = (hex) => hex.match(/\w\w/g).map((v) => {
    const channel = parseInt(v, 16) / 255;
    return channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4;
  }).reduce((sum, channel, i) => sum + channel * [0.2126, 0.7152, 0.0722][i], 0);

  for (const theme of [":root {", ':root[data-theme="dark"], .ink {']) {
    const block = css.slice(css.indexOf(theme)).split("}", 1)[0];
    const color = (name) => luminance(block.match(new RegExp(`--${name}: #([\\da-f]{6})`))[1]);
    for (const [text, surface] of [["muted", "bg"], ["muted", "surface"], ["ok", "surface"], ["gap", "gap-soft"], ["accent", "accent-ink"]]) {
      const values = [color(text), color(surface)].sort((a, b) => a - b);
      assert.ok((values[1] + 0.05) / (values[0] + 0.05) >= 4.5, `${theme}: ${text} on ${surface}`);
    }
  }
});

function playerHarness({ reduce = false, lang = null, hash = "", watchWorkflow = false } = {}) {
  const listeners = new Map();
  const attributes = new Map();
  const classes = new Set();
  let active;
  function node(id, dataset = {}) {
    return {
      id, dataset, hidden: true,
      addEventListener(event, fn) { listeners.set(`${id}:${event}`, fn); },
      setAttribute(name, value) { attributes.set(`${id}:${name}`, value); },
      getAttribute(name) { return attributes.get(`${id}:${name}`); },
      focus() { active = id; },
      classList: { add: (...names) => names.forEach((name) => classes.add(name)), remove: (name) => classes.delete(name), toggle: () => {} },
    };
  }

  const root = node("root");
  const scene = node("scene", { phase: "2" });
  const animation = {
    playState: "running",
    pause() { this.playState = "paused"; },
    play() { this.playState = "running"; },
    finish() { this.playState = "finished"; },
  };
  scene.getAnimations = () => [animation];
  const steps = [0, 1, 2].map((i) => node(`step-${i}`, { step: String(i) }));
  const selectors = {
    "#scene": scene, "#steps": node("steps"), "#cap": node("cap"), "#pp": node("pp"),
    "#pp use": node("pp-icon"), "#replay": node("replay"), ".ctl-bar": node("controls"),
    "#ins-tabs": node("install"), '[data-action="theme"]': node("theme"),
    '[data-i18n-attr="aria-label:lang.aria"]': node("language"),
    "#delivery-proof": node("delivery-proof"),
    "#copy-status": node("copy-status"),
    ".workflow-block": node("workflow"),
  };
  const installOptions = [...html.matchAll(/data-ins="([^"]+)"/g)].map(([, ins]) => node(ins, { ins }));
  const prompt = installOptions.find(({ id }) => id === "prompt");
  const promptMarkup = html.match(/<button type="button" data-ins="prompt"([^>]+)>(.*?)<\/button>/);
  prompt.innerHTML = promptMarkup[2];
  prompt.dataset.i18nAttr = promptMarkup[1].match(/data-i18n-attr="([^"]+)"/)?.[1];
  for (const [, name, value] of promptMarkup[1].matchAll(/(aria-label|title)="([^"]+)"/g)) prompt.setAttribute(name, value);
  const installPanels = installOptions.map(({ id }) => node(`panel-${id}`, { panel: id }));
  const installNotes = [...html.matchAll(/data-install-note="([^"]+)"/g)].map(([, installNote]) => node(installNote, { installNote }));
  const copyLabels = new Map(installOptions.map(({ id }) => [id, node(`copy-label-${id}`)]));
  const copyButtons = installOptions.map(({ id }) => ({
    ...node(`copy-${id}`, { copy: `code-${id}` }),
    querySelector: () => copyLabels.get(id),
  }));
  let copied;
  const timers = new Map();
  let now = 0;
  let nextTimer = 0;
  const observed = new Set();
  let intersection;
  const viewportObserver = class {
    constructor(fn) { intersection = fn; }
    observe(target) { observed.add(target); }
    unobserve(target) { observed.delete(target); }
  };
  runInNewContext(js, {
    ...(watchWorkflow ? { IntersectionObserver: viewportObserver } : {}),
    document: {
      documentElement: root,
      querySelector: (selector) => selectors[selector],
      querySelectorAll: (selector) => ({
        "[data-step]": steps, "[data-ins]": installOptions, "[data-panel]": installPanels,
        "[data-install-note]": installNotes, "[data-copy]": copyButtons,
        "[data-i18n], [data-i18n-attr]": [prompt],
        ".reveal, .workflow-block": [selectors[".workflow-block"]],
      })[selector] ?? [],
      getElementById: (id) => ({ textContent: html.match(new RegExp(`id="${id}"[^>]*>(.*?)</pre>`, "s"))?.[1] }),
    },
    window: { ...node("window"), ...(watchWorkflow ? { IntersectionObserver: viewportObserver } : {}) },
    matchMedia: () => ({ ...node("media"), matches: reduce }),
    localStorage: { getItem: () => lang },
    location: { hash },
    navigator: { clipboard: { writeText: async (text) => { copied = text; } } },
    performance: { now: () => now },
    setTimeout(fn, delay) { timers.set(++nextTimer, { fn, due: now + delay }); return nextTimer; },
    clearTimeout: (id) => timers.delete(id),
  });
  return {
    scene, steps, animation, selectors, installOptions, installPanels, installNotes, copyLabels,
    get copied() { return copied; },
    observed,
    intersect(target, isIntersecting) { intersection([{ target, isIntersecting }]); },
    get active() { return active; },
    fire: (id, event, value) => listeners.get(`${id}:${event}`)(value),
    advance(duration) {
      const until = now + duration;
      for (const [id, timer] of timers) {
        if (timer.due > until) continue;
        now = timer.due;
        timers.delete(id);
        timer.fn();
      }
      now = until;
    },
  };
}

test("pause freezes the stage and resumes its timer, replay restarts the story", () => {
  const { scene, animation, selectors, fire, advance } = playerHarness();
  assert.equal(scene.dataset.phase, "0");
  advance(1000);
  fire("pp", "click");
  assert.equal(animation.playState, "paused");
  assert.equal(selectors["#pp"].getAttribute("aria-label"), "Play");
  advance(4000);
  assert.equal(scene.dataset.phase, "0");
  fire("pp", "click");
  assert.equal(animation.playState, "running");
  advance(2599);
  assert.equal(scene.dataset.phase, "0");
  advance(1);
  assert.equal(scene.dataset.phase, "1");
  assert.match(selectors["#cap"].textContent, /suite passed.*503 case still has no result/i);
  advance(4400);
  assert.equal(scene.dataset.phase, "2");
  assert.equal(scene.getAttribute("aria-labelledby"), "step-2");
  advance(1600);
  assert.equal(selectors["#pp"].getAttribute("aria-label"), "Play");
  fire("replay", "click");
  assert.equal(scene.dataset.phase, "0");
});

test("reduced motion stays on the review until explicit navigation, including keyboard", () => {
  const harness = playerHarness({ reduce: true, lang: "vi", hash: "#delivery-proof" });
  const { scene, steps, selectors, fire, advance } = harness;
  advance(10000);
  assert.equal(scene.dataset.phase, "2");
  assert.equal(selectors["#pp"].getAttribute("aria-label"), "Chạy");
  assert.equal(selectors["#delivery-proof"].open, true);
  let prevented = false;
  fire("steps", "keydown", { key: "ArrowLeft", preventDefault: () => { prevented = true; } });
  assert.ok(prevented);
  assert.equal(scene.dataset.phase, "1");
  assert.equal(harness.active, "step-1");
  assert.equal(steps[0].tabIndex, -1);
  assert.equal(steps[1].tabIndex, 0);
  assert.equal(steps[1].getAttribute("aria-selected"), "true");
  assert.equal(steps[2].getAttribute("aria-selected"), "false");
  assert.equal(scene.getAttribute("aria-labelledby"), "step-1");
  advance(10000);
  assert.equal(scene.dataset.phase, "1");
});

test("install options switch the panel and the copy button reports the copied command", async () => {
  const harness = playerHarness({ reduce: true });
  const { installOptions, installPanels, installNotes, selectors, copyLabels, fire, advance } = harness;
  const expected = {
    cli: /specgate doctor$/,
    codex: /^codex plugin marketplace add thanhtung2693\/specgate$/,
    claude: /claude plugin install specgate@specgate$/,
    cursor: /^specgate plugins install --agent cursor --project-local\nspecgate plugins doctor --agent cursor --project-local$/,
    prompt: /^Prepare this change in SpecGate\./,
  };
  assert.equal(installOptions.length, 5);
  for (const option of installOptions) {
    const { id } = option;
    fire("install", "click", { target: { closest: () => option } });
    assert.deepEqual(installPanels.filter(({ hidden }) => !hidden).map(({ id }) => id), [`panel-${id}`]);
    assert.deepEqual(installOptions.filter((tab) => tab.getAttribute("aria-pressed") === "true").map(({ id }) => id), [id]);
    const visibleNotes = installNotes.filter(({ hidden }) => !hidden).map(({ dataset }) => dataset.installNote);
    assert.deepEqual(visibleNotes, id === "codex" ? ["codex", "codex claude cursor"]
      : id === "cursor" ? ["cursor", "codex claude cursor"] : id === "claude" ? ["codex claude cursor"] : []);
    await fire(`copy-${id}`, "click");
    assert.match(harness.copied, expected[id]);
    assert.equal(selectors["#copy-status"].textContent, "Copied");
    advance(1500);
    assert.equal(copyLabels.get(id).textContent, "Copy");
  }
});

test("install prompt icon keeps its Vietnamese accessible name and tooltip", () => {
  const page = playerHarness({ reduce: true, lang: "vi" });
  const prompt = page.installOptions.find(({ id }) => id === "prompt");
  assert.equal(prompt.getAttribute("aria-label"), "Nhờ agent");
  assert.equal(prompt.getAttribute("title"), "Nhờ agent");
  assert.match(prompt.innerHTML, /<svg/);
});

test("inspect turns the full-size evidence toward the reader before the separate decision view", () => {
  assert.match(css, /\.stack \{[^}]*rotateX\(57deg\) rotateZ\(-41deg\)/);
  assert.match(css, /\.scene3d\[data-phase="1"\] \.stack \{[^}]*transform: scale\(var\(--inspect\)\)/);
  assert.match(css, /\.scene3d\[data-phase="1"\] \.layer:not\(\.top\) \{[^}]*translate3d\([^}]*rotateZ/);
  assert.match(css, /\.layer\.top \.lr \.mono \{[^}]*font-size: 16px; white-space: normal/);
  assert.match(css, /\.layer\.top \.lr \.lab \{[^}]*font-size: 16px/);
  assert.match(css, /\.scene3d\[data-phase="2"\] \.lact \{[^}]*opacity: 1/);
  const sheet = html.slice(html.indexOf('class="layer top"'), html.indexOf('class="scanp"'));
  for (const label of ["Evidence by criterion", "Test passed", "Test result missing", "No test attached"]) {
    assert.ok(sheet.includes(label), label);
  }
});

test("the public workflow offers four ordered steps with documented Local continuations", () => {
  const flow = html.match(/<ol class="workflow"[^>]*>([\s\S]*?)<\/ol>/)?.[1];
  assert.ok(flow);
  const titles = [...flow.matchAll(/<h4[^>]*>(.*?)<\/h4>/g)].map(([, title]) => title);
  assert.deepEqual(titles, ["Agree on scope", "Let your agent work", "Review the evidence", "Decide and hand off"]);
  assert.match(flow, /spec version you approved.*project rubrics for four readiness gates.*Context Pack/s);
  assert.match(flow, /Context Pack to .*resume the same work.*agreed scope/s);
  assert.match(flow, /exact delivery you reviewed.*move the entire Local store/is);
  const links = [...flow.matchAll(/href="https:\/\/github\.com\/thanhtung2693\/specgate\/blob\/main\/(docs\/using-specgate\/[^"#]+)#([^"]+)"/g)];
  assert.equal(links.length, 4);
  for (const [, file, anchor] of links) {
    const headings = [...read("../../" + file).matchAll(/^#+ (.+)$/gm)]
      .map(([, heading]) => heading.toLowerCase().replace(/[^\w\s-]/g, "").replaceAll(" ", "-"));
    assert.ok(headings.includes(anchor), file + "#" + anchor);
  }
});

test("phone player keeps the story steps on one line with named touch targets", () => {
  assert.match(css, /#steps \{[^}]*display: flex/);
  assert.match(css, /@media \(max-width: 420px\) \{\s*\.step\[aria-selected="true"\] \.txt \{ display: none; \}/);
  assert.match(css, /@media \(pointer: coarse\) \{ \.step \{ min-width: 44px; min-height: 44px/);
  assert.match(html, /id="step-2" aria-label="You decide"/);
});

test("workflow motion starts once on entry and leaves the copy accessible", () => {
  const page = playerHarness({ watchWorkflow: true });
  const workflow = page.selectors[".workflow-block"];
  assert.ok(page.observed.has(workflow));
  page.intersect(workflow, false);
  assert.ok(page.observed.has(workflow));
  page.intersect(workflow, true);
  assert.ok(!page.observed.has(workflow));
  assert.equal(playerHarness({ reduce: true, watchWorkflow: true }).observed.size, 0);
  assert.match(html, /<div class="workflow-block">/);
  assert.match(css, /\.motion \.workflow-block:not\(\.in\):not\(:focus-within\) \.workflow li::after/);
  assert.match(css, /prefers-reduced-motion: reduce[\s\S]*?\.workflow li::after[^}]*transform: none/);
});
