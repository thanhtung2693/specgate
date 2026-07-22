import { existsSync, readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";

const here = new URL(".", import.meta.url);
const html = readFileSync(new URL("index.html", here), "utf8");
const css = readFileSync(new URL("styles.css", here), "utf8");
const js = readFileSync(new URL("script.js", here), "utf8");
const sitemap = readFileSync(new URL("sitemap.xml", here), "utf8");

function cssBlock(selector) {
  return css.match(new RegExp(`${selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\s*\\{(?<body>[\\s\\S]*?)\\n\\}`))?.groups?.body ?? "";
}

function cssVar(block, name) {
  return block.match(new RegExp(`${name}:\\s*(#[0-9a-fA-F]{6})`))?.[1] ?? "";
}

function relativeLuminance(hex) {
  const [r, g, b] = hex.match(/\w\w/g).map((part) => {
    const channel = Number.parseInt(part, 16) / 255;
    return channel <= 0.03928 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function contrastRatio(foreground, background) {
  const lighter = Math.max(relativeLuminance(foreground), relativeLuminance(background));
  const darker = Math.min(relativeLuminance(foreground), relativeLuminance(background));
  return (lighter + 0.05) / (darker + 0.05);
}

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

test("interactive landing controls keep accessible names and roles valid", () => {
  assert.match(html, /<button class="theme-toggle" type="button" aria-label="Light theme" aria-pressed="false">/);
  assert.match(html, /<pre class="term-body mono" id="cast-panel" data-cast-body role="tabpanel"/);
  assert.match(js, /themeToggle\.setAttribute\("aria-label", `\$\{nextLabel\} theme`\)/);
  assert.equal([...html.matchAll(/role="tab"/g)].length, 4);
  assert.match(js, /body\.setAttribute\("aria-labelledby", segs\[idx\]\.id\)/);
  assert.match(js, /ArrowRight/);
  assert.match(js, /ArrowLeft/);
  assert.equal([...html.matchAll(/id="console-replay"/g)].length, 1);
});

test("small muted text colors meet contrast requirements in both themes", () => {
  const root = cssBlock(":root");
  const light = cssBlock('html[data-theme="light"]');
  const darkCanvas = cssVar(root, "--canvas");
  const darkSurface = "#090a0b";
  const lightCanvas = cssVar(light, "--canvas");

  assert.ok(contrastRatio(cssVar(root, "--ink-faint"), darkCanvas) >= 4.5);
  assert.ok(contrastRatio(cssVar(root, "--ink-faint"), darkSurface) >= 4.5);
  assert.ok(contrastRatio(cssVar(light, "--ink-faint"), lightCanvas) >= 4.5);
  assert.doesNotMatch(css, /\.wm-group-muted\s*\{[^}]*opacity:/s);
});

test("navbar GitHub action includes a recognizable icon", () => {
  const navActions = html.match(/<div class="nav-actions">(?<body>[\s\S]*?)<\/div>/)?.groups?.body ?? "";
  assert.match(html, /<symbol id="i-github"/);
  assert.match(navActions, /<use href="#i-github"/);
});

test("terminal demo lines are plain text because the carousel types with textContent", () => {
  assert.doesNotMatch(js, /<b>.*<\/b>/);
  assert.doesNotMatch(js, /<strong>.*<\/strong>/);
  assert.doesNotMatch(html, /<b>.*<\/b>/);
  assert.doesNotMatch(html, /<strong>.*<\/strong>/);
});

test("stats copy calls unadjudicated events signals", () => {
  assert.match(js, /# example workspace data/i);
  assert.match(js, /pre-build signals/i);
  assert.match(js, /blocked-ambiguity reports/i);
  assert.match(js, /descriptive, not a quality score/i);
  assert.doesNotMatch(js, /ambiguity saves/i);
  assert.doesNotMatch(js, /caught by SpecGate/i);
});

test("landing copy stays aligned with current installation", () => {
  assert.doesNotMatch(html, /production-ready/i);
  assert.doesNotMatch(html, /Jira/);
  assert.match(html, /CLI and IDE handoff/);
  assert.match(html, /Install through the CLI/);
  assert.match(html, /Codex and Claude Code plugin managers/i);
  assert.match(html, /Repositories/);
  assert.match(html, /Work tracking/);
  assert.match(html, /GitHub/);
  assert.match(html, /GitLab/);
  assert.match(html, /Linear/);
  assert.doesNotMatch(html, /\bexperimental\s+(?:connectors?|integrations?|trackers?)\b/i);
  assert.doesNotMatch(html, /\b(?:connectors?|integrations?)\b[^.\n]{0,80}\bmirror(?:s|ed|ing)?\b[^.\n]{0,80}\btracker\b/i);
  assert.doesNotMatch(html, /official (?:Codex|Claude|plugin) marketplace/i);
  assert.doesNotMatch(html, /approved (?:by|for) (?:OpenAI|Anthropic|Codex|Claude)/i);
});

test("tool compatibility is compact and explains the direct and team routes", () => {
  const tools = html.match(/<section class="wordmarks"[^>]*>(?<body>[\s\S]*?)<\/section>/)?.groups?.body ?? "";

  assert.match(tools, /Your tools stay in the loop/);
  assert.equal([...tools.matchAll(/class="tool-route"/g)].length, 2);
  assert.match(tools, /Code where you already work/);
  assert.match(tools, /Repositories and optional work tracking/);
  assert.match(tools, /GitHub and GitLab confirm merged PRs and MRs at the submitted commit/);
  assert.match(tools, /Linear can receive approved work without replacing direct IDE pickup/);
  assert.doesNotMatch(tools, /class="wm-group/);
  assert.doesNotMatch(tools, /supported v0\.1 path is CLI-first/);
});

test("landing polish avoids repeated labels and decorative separators", () => {
  const eyebrowCount = [...html.matchAll(/class="eyebrow/g)].length;

  assert.ok(eyebrowCount <= 1);
  assert.doesNotMatch(html, /[·—–]/);
  assert.doesNotMatch(js, /[·—–]/);
});

test("governed loop copy does not overpromise format parsing", () => {
  const loop = html.match(/<section class="band band-rail loop" id="how">(?<body>[\s\S]*?)<\/section>/)?.groups?.body ?? "";

  assert.match(loop, /Bring any spec format/);
  assert.match(loop, /OpenSpec, Spec Kit, Superpowers, a quick change note/);
  assert.match(loop, /Approve one Context Pack/);
  assert.match(loop, /Review delivery evidence/);
  assert.doesNotMatch(loop, /can't misread/);
});

test("landing positions existing tools around the governed handoff", () => {
  assert.match(html, /Keep your stack\. Add the governed handoff/);
  assert.match(html, /SPEC TOOLS/);
  assert.match(html, /SPECGATE/);
  assert.match(html, /TRACKER \+ IDE/);
  assert.match(html, /Own the governed handoff/);
  assert.match(html, /GitHub and GitLab confirm merged PRs and MRs at the submitted commit/);
  assert.match(html, /Linear can receive approved work/);
  assert.match(html, /without replacing direct IDE pickup/);
  assert.doesNotMatch(html, /Own the contract/);
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

test("polished hero uses the approved concise message and CTA labels", () => {
  const hero = html.match(/<section class="hero">(?<body>[\s\S]*?)<\/section>/)?.groups?.body ?? "";
  const lede = hero.match(/<p class="lede[^\"]*"[^>]*>(?<body>[\s\S]*?)<\/p>/)?.groups?.body ?? "";
  const desktopHeadlineCap = Number(css.match(/\.hero-copy h1\s*\{[\s\S]*?font-size:\s*clamp\([^,]+,[^,]+,\s*(?<max>[\d.]+)rem\)/)?.groups?.max);

  assert.match(hero, /Approve one version\./);
  assert.match(hero, /Verify the build\./);
  assert.match(hero, />Run locally/);
  assert.match(hero, />View GitHub/);
  assert.ok(lede.trim().split(/\s+/).length <= 20);
  assert.ok(desktopHeadlineCap <= 3.7, `desktop headline cap ${desktopHeadlineCap}rem is too large for the fixed-width hero`);
  assert.doesNotMatch(hero, /Run the local demo|Watch the CLI/);
});

test("primary CTA explains the offline Local default", () => {
  const cta = html.match(/<section class="cta" id="cta">(?<body>[\s\S]*?)<\/section>/)?.groups?.body ?? "";

  assert.match(cta, /local-first/);
  assert.match(cta, /no Docker or server/);
  assert.doesNotMatch(cta, /one container/);
});

test("FAQ is concise and JSON-LD matches the visible questions", () => {
  const faq = html.match(/<section class="band faq" id="faq">(?<body>[\s\S]*?)<\/section>/)?.groups?.body ?? "";
  const visibleQuestions = [...faq.matchAll(/<summary><span>(?<question>[^<]+)<\/span><\/summary>/g)]
    .map((match) => match.groups.question);
  const jsonLd = html.match(/<script type="application\/ld\+json">\s*(?<json>[\s\S]*?)\s*<\/script>/)?.groups?.json;
  const faqPage = JSON.parse(jsonLd)["@graph"].find((item) => item["@type"] === "FAQPage");
  const structuredQuestions = faqPage.mainEntity.map((item) => item.name);

  assert.deepEqual(visibleQuestions, [
    "How is this different from spec tools?",
    "What spec formats do you accept?",
    "Do we have to leave our coding agent?",
    "Do I need an LLM API key?",
    "What does the delivery review check?",
  ]);
  assert.deepEqual(structuredQuestions, visibleQuestions);
});

test("final CTA reuses approved intents without decorative steps", () => {
  const cta = html.match(/<section class="cta" id="cta">(?<body>[\s\S]*?)<\/section>/)?.groups?.body ?? "";

  assert.match(cta, />Run locally/);
  assert.match(cta, />View GitHub/);
  assert.doesNotMatch(cta, /cta-badge|cta-steps|VERIFIED/);
});

test("only How it works carries the governance rail", () => {
  assert.match(html, /<section class="band band-rail loop" id="how">/);
  assert.doesNotMatch(html, /<section class="band band-rail (?:run|fit|faq)"/);
  assert.equal([...html.matchAll(/class="gate-node"/g)].length, 1);
  assert.doesNotMatch(html, /class="scanlines"|class="beam"/);
});

test("landing fonts are self-hosted and preloaded without a remote stylesheet", () => {
  assert.ok(existsSync(new URL("fonts/host-grotesk-latin.woff2", here)));
  assert.ok(existsSync(new URL("fonts/commit-mono.woff2", here)));
  assert.ok(existsSync(new URL("fonts/OFL-host-grotesk.txt", here)));
  assert.ok(existsSync(new URL("fonts/OFL-commit-mono.txt", here)));
  assert.match(css, /@font-face[\s\S]*host-grotesk-latin\.woff2/);
  assert.match(css, /@font-face[\s\S]*commit-mono\.woff2/);
  assert.match(css, /--font-display:\s*"Host Grotesk"/);
  assert.match(css, /--font-body:\s*"Host Grotesk"/);
  assert.match(css, /--font-mono:\s*"Commit Mono"/);
  assert.doesNotMatch(css, /Bricolage Grotesque|Geist/);
  assert.doesNotMatch(html, /fonts\.googleapis\.com|fonts\.gstatic\.com/);
});

test("visual system keeps only approved atmospheric layers", () => {
  assert.match(html, /class="grid-bg"/);
  assert.doesNotMatch(html, /class="scanlines"|class="beam"/);
  assert.doesNotMatch(css, /\.scanlines\s*\{|\.beam\s*\{|\.cta::before\s*\{/);
  assert.doesNotMatch(css, /\.hero::before|\.hero::after/);
});

test("mobile layout explicitly collapses terminal tabs and asymmetric sections", () => {
  assert.match(css, /@media \(max-width: 768px\)[\s\S]*\.cast-tabs[\s\S]*grid-template-columns:\s*repeat\(2, minmax\(0, 1fr\)\)/);
  assert.match(css, /@media \(max-width: 900px\)[\s\S]*\.hero[\s\S]*grid-template-columns:\s*1fr/);
  assert.match(css, /@media \(max-width: 860px\)[\s\S]*\.fit-grid[\s\S]*grid-template-columns:\s*1fr/);
});

test("terminal presents the current four-part value path", () => {
  const tabs = [...html.matchAll(/<button[^>]+data-cast-tab[^>]*>(?<label>[^<]+)<\/button>/g)]
    .map((match) => match.groups.label.trim());

  assert.deepEqual(tabs, ["Publish", "Context", "Verify", "Measure"]);
  assert.match(js, /specgate artifact publish --file artifact\.json --preview/);
  assert.match(js, /specgate artifact publish --file artifact\.json/);
  assert.match(js, /specgate gates check <artifact-id>/);
  assert.match(js, /specgate --yes artifact approve <artifact-id>/);
  assert.match(js, /specgate --yes artifact promote <artifact-id>/);
  assert.match(js, /specgate work create --feature local-healthcheck/);
  assert.match(js, /specgate work list --phase ready/);
  assert.match(js, /specgate work context <work-ref>/);
  assert.match(js, /specgate delivery report <work-ref> --init/);
  assert.match(js, /specgate delivery submit <work-ref> --file \.specgate\/completion-<ref>\.json --run-checks/);
  assert.match(js, /specgate delivery peer-review <work-ref> --init/);
  assert.match(js, /specgate delivery status <work-ref> --detail/);
  assert.match(js, /specgate --yes delivery approve <work-ref>/);
  assert.match(js, /Human approval remains required/);
  assert.doesNotMatch(js, /Delivery verdict: PASS/);
  assert.match(js, /specgate stats --days 30/);
  assert.doesNotMatch(js, /work create-quick|gates history|governance profile|Custom gates/);
});

test("terminal has useful no-JavaScript content and accessible tab ownership", () => {
  const terminal = html.match(/<div class="cast-carousel[^>]*>(?<body>[\s\S]*?)<\/div>\s*<\/section>/)?.groups?.body ?? "";

  assert.match(terminal, /role="tablist"/);
  assert.match(terminal, /role="tabpanel"/);
  assert.match(terminal, /specgate artifact publish --file artifact\.json --preview/);
  assert.match(terminal, /id="cast-tab-publish"/);
  assert.match(terminal, /aria-controls="cast-panel"/);
  assert.match(terminal, /aria-labelledby="cast-tab-publish"/);
  assert.match(js, /ArrowRight/);
  assert.match(js, /ArrowLeft/);
});
