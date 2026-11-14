import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { test } from "node:test";
import { sample, beginDecision, recordDecision, reset } from "./sample.mjs";

test("the built page uses the shared presentation without starting the production app", async () => {
  const html = await readFile(new URL("index.html", import.meta.url), "utf8");
  const assets = [...html.matchAll(/(?:src|href)="(\.\/[^\"]+)"/g)].map(([, path]) => path);
  assert.ok(assets.includes("./shared/presentation.js"));
  assert.ok(assets.includes("./shared/source-view.js"));
  assert.ok(assets.includes("./demo.mjs"));
  assert.ok(!assets.some((path) => path.endsWith("/app.js")));
  const built = await Promise.all(assets.map((path) => readFile(new URL(path, import.meta.url), "utf8")));
  assert.ok(built.every((content) => content.length > 0));
  assert.match(built[assets.indexOf("./shared/style.css")], /url\("\.\/assets\/geist\.woff2"\)/);
});

test("sample opening matches the landing story and includes real example source and diff", () => {
  const delivery = sample(1);
  assert.deepEqual(delivery.criteria.map((item) => item.outcome), ["passed", "missing", "claim"]);
  assert.match(delivery.source.diff.text, /^\+.*503/m);
  assert.ok(delivery.source.source.lines.some((line) => line.text.includes("503")));
  assert.match(delivery.source.diff.text, /--- a\/health\.go\n\+\+\+ b\/health\.go/);
  assert.deepEqual(sample(2).criteria.map((item) => item.outcome), ["passed", "passed", "claim"]);
});

test("request changes needs a note and the exact sample confirmation", () => {
  const opening = reset();
  assert.equal(beginDecision(opening, "reject", "   "), null);
  const held = beginDecision(opening, "reject", " Add the 503 test. ");
  assert.equal(held.review, "sample-review-1");
  assert.equal(held.completion, "sample-delivery-1");
  assert.equal(recordDecision(opening, { ...held, completion: "sample-delivery-2" }), opening);
  const requested = recordDecision(opening, held);
  assert.deepEqual(requested.decision, { verdict: "reject", note: "Add the 503 test." });
  assert.equal(beginDecision(requested, "approve", ""), null);
  assert.equal(recordDecision(requested, held), requested);
});

test("the revised sample, acceptance and reset remain reversible and local", () => {
  const revised = reset(2);
  const next = recordDecision(revised, beginDecision(revised, "approve", ""));
  assert.equal(next.decision.verdict, "approve");
  assert.equal(next.version, 2);
  assert.deepEqual(reset(), { version: 1, decision: null });
});
