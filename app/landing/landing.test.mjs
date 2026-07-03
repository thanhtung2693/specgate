import { readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";

const here = new URL(".", import.meta.url);
const html = readFileSync(new URL("index.html", here), "utf8");
const css = readFileSync(new URL("styles.css", here), "utf8");
const js = readFileSync(new URL("script.js", here), "utf8");
const sitemap = readFileSync(new URL("sitemap.xml", here), "utf8");

test("navigation order matches the visible section order", () => {
  const navHtml = html.match(/<nav class="nav-links"[\s\S]*?<\/nav>/)?.[0] ?? "";
  const navOrder = [...navHtml.matchAll(/<a href="#([^"]+)">/g)]
    .map((match) => match[1])
    .filter((id) => ["how", "run", "compare", "faq"].includes(id));
  const sectionOrder = [...html.matchAll(/<section[^>]+id="([^"]+)"/g)]
    .map((match) => match[1])
    .filter((id) => navOrder.includes(id));

  assert.deepEqual(navOrder, sectionOrder);
});

test("Spec Kit naming is consistent across metadata and body copy", () => {
  assert.equal(html.includes("GitHub Spec Kit"), false);
});

test("terminal output lines have muted styling", () => {
  assert.match(css, /\.tl\[data-k="out"\]\s*\{\s*color:\s*var\(--ink-muted\);/s);
});

test("carousel builds one segment per demo and keeps autoplay running", () => {
  const carouselScript = js.match(/function initCastCarousel\(\) \{(?<body>[\s\S]*?)\n\}\)\(\);/)?.groups?.body ?? "";

  assert.match(html, /data-cast-segments/);
  assert.match(carouselScript, /segmentList\.replaceChildren/);
  assert.match(carouselScript, /\.\.\.CASTS\.map/);
  assert.match(carouselScript, /"Stats"/);
  assert.doesNotMatch(carouselScript, /mouseenter/);
  assert.doesNotMatch(carouselScript, /mouseleave/);
});

test("navbar GitHub action includes a recognizable icon", () => {
  const navActions = html.match(/<div class="nav-actions">(?<body>[\s\S]*?)<\/div>/)?.groups?.body ?? "";
  assert.match(html, /<symbol id="i-github"/);
  assert.match(navActions, /<use href="#i-github"/);
});

test("terminal demo shows the governed workflow with evidence and review detail", () => {
  assert.match(js, /specgate status --all-workspaces/);
  assert.match(js, /specgate work list --all-workspaces/);
  assert.match(js, /specgate work show CR-1D0256D8/);
  assert.match(js, /specgate work context CR-1D0256D8 > context-pack\.md/);
  assert.match(js, /specgate gates status CR-1D0256D8/);
  assert.match(js, /specgate gates run CR-1D0256D8 --json/);
  assert.match(js, /specgate delivery report CR-1D0256D8 --init/);
  assert.match(js, /specgate delivery submit CR-1D0256D8 --file completion\.json/);
  assert.match(js, /specgate delivery status CR-1D0256D8 --detail --json/);
  assert.match(js, /specgate stats/);
  assert.doesNotMatch(js, /work list --ready/);
  assert.doesNotMatch(js, /--yes/);
  assert.doesNotMatch(js, /saved-card/);
  assert.match(js, /report → gates → review → status/);
});

test("terminal demo lines are plain text because the carousel types with textContent", () => {
  assert.doesNotMatch(js, /<b>.*<\/b>/);
  assert.doesNotMatch(js, /<strong>.*<\/strong>/);
});

test("landing copy stays aligned with CLI-first alpha support", () => {
  assert.doesNotMatch(html, /over MCP/);
  assert.doesNotMatch(html, /IDE\/MCP handoff/);
  assert.doesNotMatch(html, /production-ready/i);
  assert.doesNotMatch(html, /Jira/);
  assert.match(html, /CLI \+ IDE handoff/);
  assert.match(html, /supported in the alpha vs experimental/i);
});

test("landing polish avoids repeated labels and decorative separators", () => {
  const visibleHtml = html
    .replace(/<script[\s\S]*?<\/script>/g, "")
    .replace(/<svg[\s\S]*?<\/svg>/g, "");
  const eyebrowCount = [...visibleHtml.matchAll(/class="eyebrow/g)].length;

  assert.ok(eyebrowCount <= 1);
  assert.doesNotMatch(visibleHtml, /[·—–]/);
  assert.doesNotMatch(js, /[·—–]/);
});

test("hero message is direct and action-oriented", () => {
  const hero = html.match(/<section class="hero">(?<body>[\s\S]*?)<\/section>/)?.groups?.body ?? "";
  const lede = hero.match(/<p class="lede[^"]*"[^>]*>(?<body>[\s\S]*?)<\/p>/)?.groups?.body ?? "";

  assert.match(lede, /AI coding agents/);
  assert.match(lede, /versioned spec package/);
  assert.match(lede, /delivery evidence/);
  assert.match(hero, /Run the local demo/);
  assert.doesNotMatch(hero, /Pre-alpha/);
});

test("governed loop copy does not overpromise format parsing", () => {
  const loop = html.match(/<section class="band loop" id="how">(?<body>[\s\S]*?)<\/section>/)?.groups?.body ?? "";

  assert.match(loop, /Bring any spec format/);
  assert.match(loop, /OpenSpec, Spec Kit, a quick change note/);
  assert.match(loop, /Approve one Context Pack/);
  assert.match(loop, /Review delivery evidence/);
  assert.doesNotMatch(loop, /can't misread/);
});

test("landing positions existing tools around the governed handoff", () => {
  assert.match(html, /Keep your stack\. Add the governed handoff/);
  assert.match(html, /SPEC TOOLS/);
  assert.match(html, /SPECGATE/);
  assert.match(html, /TRACKER \+ IDE/);
  assert.match(html, /Your tracker does not become a second spec source/);
  assert.doesNotMatch(html, /Manual checklist steps only/);
  assert.doesNotMatch(html, /SpecGate sits/);
});

test("landing keeps the public story compact", () => {
  assert.doesNotMatch(html, /class="cap-table"/);
  assert.doesNotMatch(html, /data-flow/);
  assert.doesNotMatch(html, /href="#verify"/);
});

test("social metadata uses the shipped logo asset", () => {
  assert.match(html, /property="og:image" content="https:\/\/thanhtung2693\.github\.io\/specgate\/logo\.svg"/);
  assert.match(html, /name="twitter:image" content="https:\/\/thanhtung2693\.github\.io\/specgate\/logo\.svg"/);
  assert.match(html, /property="og:image:alt" content="SpecGate logo"/);
  assert.match(html, /name="twitter:image:alt" content="SpecGate logo"/);
  assert.doesNotMatch(html, /\/images\/specgate-black\.svg/);
  assert.doesNotMatch(html, /specgate\.io/);
});

test("SEO metadata is canonical, crawlable, and structured", () => {
  assert.match(html, /<meta name="robots" content="index, follow, max-image-preview:large"/);
  assert.match(html, /<link rel="canonical" href="https:\/\/thanhtung2693\.github\.io\/specgate\/"/);
  assert.match(html, /<link rel="sitemap" type="application\/xml" href="https:\/\/thanhtung2693\.github\.io\/specgate\/sitemap\.xml"/);
  assert.match(html, /<main id="top" aria-labelledby="page-title" tabindex="-1">/);
  assert.match(html, /<h1 id="page-title"/);
  assert.match(css, /\.skip-link:focus/);
  assert.match(sitemap, /<lastmod>2026-07-03<\/lastmod>/);

  const jsonLd = html.match(/<script type="application\/ld\+json">\s*(?<json>[\s\S]*?)\s*<\/script>/)?.groups?.json;
  assert.ok(jsonLd);
  const graph = JSON.parse(jsonLd)["@graph"];
  const types = graph.map((item) => item["@type"]);
  assert.ok(types.includes("Organization"));
  assert.ok(types.includes("WebSite"));
  assert.ok(types.includes("WebPage"));
  assert.ok(types.includes("SoftwareApplication"));
  assert.ok(types.includes("FAQPage"));
  assert.equal(graph.find((item) => item["@type"] === "WebPage").about["@id"], "https://thanhtung2693.github.io/specgate/#software");
});
