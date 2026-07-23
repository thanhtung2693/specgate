import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const here = new URL(".", import.meta.url);
const html = readFileSync(new URL("index.html", here), "utf8");
const styles = readFileSync(new URL("styles.css", here), "utf8");
const timeline = readFileSync(new URL("timeline.js", here), "utf8");
const readme = readFileSync(new URL("../../README.md", here), "utf8");

test("peer connector reaches the peer note instead of dangling in the proof lane", () => {
  const path = html.match(/id="peer-connector"[^>]+d="M710 \d+ V(?<vertical>\d+) H(?<horizontal>\d+)"/);

  assert.ok(path, "peer connector path is missing");
  assert.ok(Number(path.groups.vertical) >= 299, "connector stops before the peer-note marker");
  assert.ok(Number(path.groups.horizontal) >= 820, "connector does not tuck under the peer-note marker");
});

test("the evidence runner and proof dots share the lane center", () => {
  assert.match(html, /viewBox="0 0 1500 258"/);
  assert.match(html, /class="lane-base" d="M47 128 H1043"/);
  assert.match(html, /id="lane-progress" class="lane-progress" d="M47 128 H1043"/);
  assert.match(html, /id="peer-connector" class="peer-connector" d="M710 128 V299 H820"/);
  assert.match(styles, /--lane-center-y: 128px;/);
  assert.match(styles, /top: calc\(var\(--lane-center-y\) - 7\.5px\);/);
  assert.match(styles, /top: calc\(var\(--lane-center-y\) - var\(--node-top\) - 8\.5px\);/);
});

test("revision copy describes readiness and retained evidence", () => {
  assert.match(html, />REVISION READY</);
  assert.match(html, />Evidence reproduced · peer affirmed</);
  assert.match(timeline, /textContent: "Evidence reproduced · peer affirmed"/);
  assert.doesNotMatch(html, />REVISION VERIFIED</);
  assert.doesNotMatch(html, />AC-02 reproduced · peer affirmed</);
  assert.doesNotMatch(timeline, /textContent: "AC-02 reproduced · peer affirmed"/);
});

test("README uses the concise landing-player label", () => {
  assert.match(
    readme,
    /<a href="https:\/\/thanhtung2693\.github\.io\/specgate\/#delivery-proof"><strong>Watch 15s video<\/strong><\/a>/,
  );
});
