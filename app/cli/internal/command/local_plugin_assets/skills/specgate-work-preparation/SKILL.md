---
name: specgate-work-preparation
description: Use when preparing a request or existing source documents for SpecGate approval, creating quick SpecGate work, publishing an artifact version, or repairing artifact readiness before implementation.
---

# Preparing Work

Apply the [SpecGate operating contract](../specgate/SKILL.md#operating-contract).
This phase produces an approved implementation handoff; it never implements
product code.

## 1. Define the contract and route

Read the request, repository instructions, and human-selected sources. Show the
title, description, observable criteria, and non-goals; split independently
accepted work. Bind `@check:<name>` only after the human confirms that check and
show which criteria it enforces and which remain reviewed claims.

In Local mode, offer pinning only when the human wants reviewed commands stable
across agents or resumes; never make it a default quick-work step. Carry each
candidate binding and command/cwd proof to delivery.

Choose one route with the human:

- **Quick work** for a small change that does not need a governed source snapshot.
- **Artifact-backed work** when an existing spec, design, plan, verification
  document, or other source must be versioned and approved.

Do not create a durable record until the human approves the displayed contract.

Completion criterion: the human-approved preview names every slice's contract and route.

## 2A. Create quick work

Quick work is available in Local and Full mode. Create it with explicit criteria,
then read the approved contract back:

```bash
specgate work create-quick "$TITLE" --description "$DESCRIPTION" \
  --ac "$CONFIRMED_CRITERION_1" --json
specgate work show "$WORK_REF" --json
specgate work context "$WORK_REF" --json
```

Never derive criteria from source structure. Stop if the stored contract differs
from the preview.

Completion criterion: the returned Context Pack reproduces the approved quick
contract exactly. Quick work ends here; switch to `specgate-work-delivery`.

## 2B. Preview and publish an artifact

The originating framework owns source paths, names, lifecycle, and Git policy.
Never relocate, copy, rename, delete, commit, or change ignore rules; edit only
with authorization.

Roles are routing labels. Map each human-selected source explicitly; never
detect a framework or manufacture documents. Reuse a path under a second role
when policy requires it.

Keep the transient manifest at `.specgate/work/artifact.json`: unchanged
repository-relative `path`, explicit `role`, and exactly one source. Use
`content`, `repo_file` in the repository, `source_file` beneath the manifest,
or absolute `file_url` outside it.

### Local source inventory

For Local artifact-backed work, propose inventory only for several independent
slices, explicit deferrals, or a human request for source coverage; never for
quick work or by inferring entries from source structure. In the existing
preview, show each proposed ID, text, unchanged path, slice mapping, and
deferral. The human reviews completeness and confirms `source_criteria` entries
safe for exact `@source:<id>` mappings. State that inventory is Local-only and
any historical inventory blocks portable/v1 export to Full mode; Local backup remains.

Use the human-selected `feature_key`; `request_type` is `new_feature`,
`change_request`, `bugfix`, or `unknown`:

```json
{
  "feature_key": "<human-selected-key>",
  "request_type": "new_feature",
  "documents": [{
    "path": "docs/framework/spec.md",
    "role": "spec",
    "repo_file": "docs/framework/spec.md"
  }]
}
```

Never use `..` traversal or copy sources under `.specgate/work`. Preview without
a server write:

```bash
specgate artifact publish --file .specgate/work/artifact.json --preview --json
```

For an update, set the exact `base_version` and compare with the selected base:

```bash
specgate artifact publish --file .specgate/work/artifact.json \
  --preview --compare "$BASE_ARTIFACT_ID" --json
```

Show source mapping and exact policy projection, including added, removed, changed,
and unchanged paths. Ask for each omitted impact's exact `yes`, `no`, or
`unknown`; resolve feature identity from human selection or a named work item.

Completion criterion: every selected source appears once under its unchanged
path and role; source files and Git policy remain unchanged except for
authorized edits.

Publish only after explicit human confirmation of that preview:

```bash
specgate artifact publish --file .specgate/work/artifact.json --json
```

Completion criterion: publication succeeded and its artifact ID and immutable
version are recorded. On failure, stop before readiness.

## 3. Complete readiness

```bash
specgate gates check "$ARTIFACT_ID" --json --summary
```

When pending task IDs exist, complete every frozen task: list them, judge only
their `skill_content`, and write `.specgate/work/gate-<task-id>.json`:

```json
{
  "gate": "<gate_key>",
  "gate_digest": "<task gate_digest>",
  "input_digest": "<task artifact_digest>",
  "state": "pass|warn|fail|needs_human_review|not_applicable",
  "summary": "<deciding evidence>",
  "evaluator": {"executor": "ide_agent", "name": "<agent name>"}
}
```

```bash
specgate gates tasks list "$ARTIFACT_ID" --json
specgate gates tasks submit-result <task-id> \
  --file .specgate/work/gate-<task-id>.json --json
```

`tasks list` includes task content and both digests; do not refetch. Copy
digests exactly. `aggregate=not_run` is unfinished; stale digests need a fresh
task, while readiness errors preserve explicit blockers.

Completion criterion: every pending task has a submitted result for its exact
digests, and the final aggregate plus every remaining gap is recorded.

## 4. Repair without taking ownership

For authorized corrections, publish a new version with the same paths, exact
`base_version`, comparison, confirmed preview, and readiness. Ask about product
ambiguity; report out-of-scope gaps without editing their source.

Completion criterion: readiness is acceptable under the stored policy, or each
remaining gap has an explicit human owner and no unauthorized source edit.

## 5. Obtain the human decision

Show the exact immutable snapshot and readiness evidence:

```bash
specgate artifact show "$ARTIFACT_ID" --json
```

Reuse readiness rows from section 3; render every state and hint verbatim:

```text
SpecGate readiness — <artifact-id> <version> (<aggregate>)
[<state>] <gate> — <hint>
Not yet judged: <count>
```

Never call a `fail`, `warn`, `needs_human_review`, or `not_run` gate acceptable,
or hide its row behind the aggregate.

Stop for the human decision. After the human explicitly approves and authorizes
that exact snapshot, run the normal handoff with every confirmed criterion:

```bash
specgate --yes change approve "$ARTIFACT_ID" \
  --title "$TITLE" \
  --ac "$CONFIRMED_CRITERION_1" \
  --json
specgate work context "$WORK_REF" --json
```

Require the returned work item's `lead_artifact_id` to equal the approved
artifact and its Context Pack to reference the governed sources. A conflicting
existing work contract is a blocker, never a silent relink.

For each authorized slice, include its exact `@source:<id>` token. Run
`specgate artifact coverage "$ARTIFACT_ID" --json`; report unassigned entries
and deferrals without creating work or deciding delivery.

Completion criterion: either the human is reviewing the named immutable
artifact, or the approved work reference and matching Context Pack are recorded;
no implementation file was edited in this phase.
