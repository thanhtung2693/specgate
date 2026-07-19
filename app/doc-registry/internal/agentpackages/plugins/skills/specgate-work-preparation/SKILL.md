---
name: specgate-work-preparation
description: Preparation. Use when turning an ambiguous request, PRD, spec, or planning discussion into SpecGate-ready vertical-slice work, publishing or updating an artifact package, or checking whether an artifact is ready for human review or handoff.
---

# Preparing Work

## Invocation

Invocation mode: [lifecycle phase](../specgate-router/SKILL.md#invocation).
Use for shaping work, publishing artifacts, repairing readiness gaps, and
preparing a human handoff. This phase never implements product code.

## 1. Understand the request

Read the request, relevant PRD/spec/task notes, nearby repo docs, and only enough
code to learn domain language and integration boundaries. Capture the outcome,
constraints, known risks, and unresolved product decisions. Ask the human when
intent is missing; plausible prose is not a substitute for a decision.

Completion criterion: intent and open questions are explicit.

## IDE-agent quick route

For an eligible one-repository task, draft and show the exact title,
description, one to five observable acceptance criteria, and explicit non-goals.
Do not create work until the developer explicitly approves that preview. If the
contract changes, show a revised preview and wait again.

### Quick work

In either mode, approved small work may use `specgate work create-quick --ac`.
Local mode has no platform model to draft missing criteria, so it requires at least one explicit `--ac`.
Verify the persisted list with
`specgate work show --json`. Never infer criteria from headings, numbering,
filenames, or keywords.

## 2. Draft vertical slices

Each slice should deliver a narrow, independently reviewable path through the
needed layers. Include its title, implementation scope, acceptance criteria,
dependencies, and non-goals. Keep unrelated cleanup and horizontal prefactoring
separate.

Delivery review is atomic, so separate work that can pass independently.

### Acceptance criteria

Acceptance criteria must describe observable behavior. A human may bind one to
a deterministic check with a trailing `@check:<name>`:

```bash
specgate work create-quick "Fix login" \
  --ac "Valid credentials open the dashboard @check:integration"
```

Use a binding only when the human authored or confirmed that exact check. The
delivery report must later contain a matching `checks[].name`.

Completion criterion: every slice has testable criteria and clear non-goals.

## 3. Publish the artifact package

For an existing local specification package, map exact files and roles in
`artifact.json`, then preview before publication:

```json
{
  "feature_key": "checkout-loyalty-points",
  "request_type": "new_feature",
  "documents": [
    {
      "path": "docs/spec.md",
      "role": "spec",
      "source_file": "docs/spec.md"
    },
    {
      "path": "docs/verification.md",
      "role": "verification",
      "source_file": "docs/verification.md"
    }
  ]
}
```

```bash
specgate artifact publish --file artifact.json --preview --json
```

`source_file` is relative to the directory containing `artifact.json`; `path`
is the package path preserved in SpecGate and may differ from the source path.
Every document must set exactly one of `content`, `source_file`, or `file_url`.
Use `content` for deliberate inline text and `file_url` only for an explicit
absolute local file URL. Omitting all three is invalid; SpecGate never guesses a
source from `path`.

The preview must show exact repository-relative paths, explicit roles, target
work, update `base_version`, provenance, and non-goals. It never uploads or
calls the server. SpecGate does not detect frameworks or infer roles from
directory names; `source_kind` is optional provenance only.

If the preview lists `impact_declaration` under `omitted`, stop before
publication. Ask the developer for explicit `yes`, `no`, or `unknown` answers
for protected domains, data/schema change, external contract change,
irreversible/complex rollback, and broad blast radius, then add the declaration
to `artifact.json` and preview again. Never infer `no`; missing or `unknown`
signals may intentionally select stricter governance.

For an update, compare the prepared package with one explicitly selected base
artifact:

```bash
specgate artifact publish --file artifact.json --preview --compare "$BASE_ARTIFACT_ID" --json
```

Comparison makes read-only metadata calls for the base artifact and its stored
file hashes; it never downloads previous content or publishes. Report added,
removed, changed, and unchanged files. A comparison is local
preparation evidence, not synchronization. `artifact.json` must carry the exact
`base_version`; a mismatch stops before publication.

Only after explicit human confirmation should the agent invoke the existing
`artifact publish` path. Local files are not synchronized merely because a
framework generated them. A successful publication creates one immutable,
path-preserving artifact version with server-computed file/package digests and
source provenance. The quick route below remains valid for work with no source
specification. This shortcut works in Local and Full mode.

Inspect named work first with `specgate work show "$WORK_REF" --json`. Otherwise
publish the prepared package:

```bash
specgate artifact publish --file artifact.json --json
```

`artifact.json` is the publish body, not a dependency manifest. For feature-backed
work, choose `feature_key` deliberately:

- Continuing work: reuse the exact key from `work show`, or search with
  `specgate feature list --search <topic> --all` (`--all` includes archived keys).
- New feature: create a new key.
- Ambiguous ownership: show candidate keys and let the human choose.

Do not invent a feature for quick-route work. Record the returned artifact id,
version, and `missing_roles`. Missing roles are readiness gaps, not publish errors.
Present the artifact using the router's human-readable entity-link rule.

Completion criterion: publication is recorded, or its error is reported without
editing implementation code.

## 4. Run readiness

```bash
specgate gates check "$ARTIFACT_ID" --json --summary
```

In both Local and Full mode, if `dispatched_to_ide_agent` is returned, complete every frozen gate task:

1. List: `specgate gates tasks list "$ARTIFACT_ID" --json`.
2. Read each rubric: `specgate gates tasks show <task-id> --json`.
3. Judge against `skill_content`; reserve `pass` for the stated pass condition.
4. Write the result to `.specgate/work/gate-<task-id>.json`:

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
specgate gates tasks submit-result <task-id> \
  --file .specgate/work/gate-<task-id>.json --json
```

Completion criterion: every dispatched task is submitted and the aggregate plus
remaining gaps are known.

Keep gate-result scratch files under `.specgate/work`; never leave `result.json`
or another generated receipt in the repository root. They then appear in
`specgate cleanup --work --dry-run`.

For a normal terminal user, give the exact `gates tasks list`, `gates tasks
show`, and `gates tasks submit-result` commands. The CLI never invoked a model.

Use `specgate gates results "$ARTIFACT_ID" --json` only when stored detailed
evidence is needed; it does not rerun readiness.

## 5. Repair and recheck

Fix each gap in the document that owns it. Do not fill ambiguous product intent.
For a corrected artifact, set `base_version` to the version being replaced, then:

```bash
specgate artifact publish --file artifact.json --preview --compare "$BASE_ARTIFACT_ID" --json
# Wait for explicit human confirmation of the changed package.
specgate artifact publish --file artifact.json --json
specgate gates check "$ARTIFACT_ID" --json --summary
```

Judge any fresh IDE tasks again; stale digests are rejected. If readiness is
unavailable, preserve the artifact and report the service error.

Completion criterion: readiness is clean, or each remaining warning has a human
owner.

## 6. Stop for human approval

Readiness is not approval. Do not implement or record approval before the human
decides. Stop here. First ask the human to inspect the exact immutable snapshot,
then run or explicitly authorize the single normal-path decision:

```bash
specgate artifact show "$ARTIFACT_ID" --json
specgate --yes change approve "$ARTIFACT_ID" --json
```

This records approval of the exact snapshot and makes that version canonical
as one resumable transition. The expert `artifact approve` and
`artifact promote` commands remain available for diagnosis. For new work, bind
the exact canonical artifact through the feature-backed route:

```bash
specgate work create --feature "$FEATURE_KEY" --title "$TITLE" \
  --ac "$CONFIRMED_CRITERION_1" \
  --ac "$CONFIRMED_CRITERION_2" \
  --json
specgate work context "$WORK_REF" --json
```

Every `--ac` value must come from the agent's explicit review of the mapped
source documents and the developer-approved preview.

Record the returned work reference and require its `lead_artifact_id` to equal
the promoted artifact. Verify the Context Pack contains the governed
spec/design/plan/verification material or exact artifact references. If the
intended work item already exists, inspect it instead of creating a duplicate;
stop when it points at a different artifact because this flow has no silent
relink step. Report publication, approval, promotion, work linkage, and Context
Pack assembly as separate durable states, using linked titles plus stable IDs
per the router rule. Switch to `specgate-work-delivery` only after
approval and handoff readiness.

Completion criterion: no implementation files were edited in this phase.
