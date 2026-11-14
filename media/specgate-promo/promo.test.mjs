import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const here = new URL(".", import.meta.url);
const html = readFileSync(new URL("index.html", here), "utf8");
const styles = readFileSync(new URL("styles.css", here), "utf8");
const timeline = readFileSync(new URL("timeline.js", here), "utf8");
const readme = readFileSync(new URL("../../README.md", here), "utf8");
const packageJSON = JSON.parse(readFileSync(new URL("package.json", here), "utf8"));

test("media commands share one stable tool pin documented for contributors", () => {
  const versions = Object.values(packageJSON.scripts)
    .filter(command => command.includes("hyperframes@"))
    .map(command => command.match(/hyperframes@(\d+\.\d+\.\d+)\s/)[1]);
  assert.equal(versions.length, 4);
  assert.equal(new Set(versions).size, 1);
  assert.ok(readFileSync(new URL("README.md", here), "utf8").includes(`HyperFrames ${versions[0]}`));
});

test("the evidence rail aligns its endpoints with the claim and human markers", () => {
  const [, viewWidth, viewHeight] = html.match(/viewBox="0 0 (\d+) (\d+)"/);
  const [, claimX, railY, decisionX] = html.match(/class="lane-base" d="M([\d.]+) (\d+) H([\d.]+)"/);
  const [, nodeSize] = styles.match(/\.proof-node \.node-dot\s*\{[^}]*width: (\d+)px;/);
  const [, claimLeft] = styles.match(/\.node-claim \{ left: (\d+)px;/);
  const [, decisionLeft] = styles.match(/\.node-human \{ left: (\d+)px;/);
  assert.equal(Number(claimX), Number(claimLeft) + Number(nodeSize) / 2);
  assert.equal(Number(decisionX), Number(decisionLeft) + Number(nodeSize) / 2);
  assert.ok(Number(decisionX) < Number(viewWidth) && Number(railY) < Number(viewHeight));
  assert.match(styles, new RegExp(`--lane-center-y: ${railY}px;`));
  assert.ok(html.includes(`class="lane-progress" d="M${claimX} ${railY} H${decisionX}"`));
  assert.ok(html.includes('window.__timelines["specgate-promo"] = window.__specgatePromoTimeline'));
  assert.ok(timeline.includes('gsap.timeline({ paused: true })'));
});

test("README uses the concise landing-player label", () => {
  assert.match(
    readme,
    /<a href="https:\/\/thanhtung2693\.github\.io\/specgate\/#delivery-proof"><strong>Watch 15s video<\/strong><\/a>/,
  );
});
