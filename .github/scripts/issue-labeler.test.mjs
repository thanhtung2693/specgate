import { test } from "node:test";
import assert from "node:assert/strict";

import { classifyIssue } from "./issue-labeler.mjs";

test("labels doctor fix feature requests as enhancement and cli", () => {
  assert.deepEqual(
    classifyIssue({
      title: "Feature: Add specgate doctor --fix",
      body: "Automatically repair common environment issues during onboarding.",
      labels: ["enhancement"],
    }),
    ["cli"],
  );
});

test("labels UI bug reports by type and scope", () => {
  assert.deepEqual(
    classifyIssue({
      title: "UI crashes when opening artifact library",
      body: "The dashboard fails after clicking an artifact.",
    }),
    ["bug", "doc-registry", "ui"],
  );
});

test("labels documentation requests", () => {
  assert.deepEqual(
    classifyIssue({
      title: "Docs: clarify quickstart install steps",
      body: "README needs a better setup example.",
    }),
    ["docs", "documentation"],
  );
});

test("labels workflow failures as GitHub Actions", () => {
  assert.deepEqual(
    classifyIssue({
      title: "Release workflow failing in CI",
      body: "GitHub Actions fails while publishing images.",
    }),
    ["bug", "github-actions", "release"],
  );
});

test("falls back to needs-triage when no rule matches", () => {
  assert.deepEqual(
    classifyIssue({
      title: "Unexpected behavior",
      body: "I am not sure where this belongs.",
    }),
    ["needs-triage"],
  );
});

test("does not duplicate existing labels", () => {
  assert.deepEqual(
    classifyIssue({
      title: "Question about model setup",
      body: "How do I configure OpenRouter?",
      labels: ["question", "agents"],
    }),
    [],
  );
});
