---
name: specgate-work-preparation
description: Use when preparing a request or existing source documents for SpecGate approval, creating quick SpecGate work, publishing an artifact version, or repairing artifact readiness before implementation.
---

# Preparing Work

Apply the [router operating contract](../specgate-router/SKILL.md#operating-contract).
This phase produces an approved implementation handoff; it never implements
product code.

## 1. Define the contract and route

Read the request, governing repository instructions, and author-selected source
documents. Show the human the exact title, description, observable acceptance
criteria, and non-goals. Split work that can be accepted independently. Use an
`@check:<name>` binding only when the human confirms that exact deterministic
check.

Choose one route with the human:

- **Quick work** for a small change that does not need a governed source snapshot.
- **Artifact-backed work** when an existing spec, design, plan, verification
  document, or other source must be versioned and approved.

Do not create a durable record until the human approves the displayed contract.

Completion criterion: the human-approved preview contains every slice's title,
scope, criteria, and non-goals, and names one route.

## 2A. Create quick work

Quick work is available in Local and Full mode. Persist the approved contract
with explicit criteria, then read it back:

```bash
specgate work create-quick "$TITLE" --description "$DESCRIPTION" \
  --ac "$CONFIRMED_CRITERION_1" --json
specgate work show "$WORK_REF" --json
specgate work context "$WORK_REF" --json
```

Never derive criteria from filenames, headings, numbering, or keywords. If the
persisted title, description, or ordered criteria differ from the preview, stop
instead of implementing.

Completion criterion: the returned Context Pack reproduces the approved quick
contract exactly. Quick work ends here; switch to `specgate-work-delivery`.

## 2B. Preview and publish an artifact

The originating framework owns source paths, names, lifecycle, and Git policy.
Never relocate, copy, rename, delete, commit, or change ignore rules for source
documents to fit SpecGate. Do not detect frameworks from directory names. Edit
source content only when the user's preparation or readiness-repair request
authorizes that edit.

SpecGate does not detect frameworks or infer roles; the agent maps each selected
source explicitly.

Keep the transient manifest at `.specgate/work/artifact.json`. For every mapped
document:

- set `path` to its unchanged repository-relative POSIX path;
- set its explicit governance `role`;
- use `source_file` only when the source is contained by the manifest directory;
- use an explicit absolute local `file_url` when the source is outside the
  manifest directory;
- set exactly one of `content`, `source_file`, or `file_url`.

Start from this minimum shape. The human selects `feature_key`; `request_type`
must be `new_feature`, `change_request`, `bugfix`, or `unknown`:

```json
{
  "feature_key": "<human-selected-key>",
  "request_type": "new_feature",
  "documents": [{
    "path": "docs/framework/spec.md",
    "role": "spec",
    "file_url": "file:///absolute/path/to/docs/framework/spec.md"
  }]
}
```

Do not use `..` traversal or copy sources under `.specgate/work`. Preview the
package without a server write:

```bash
specgate artifact publish --file .specgate/work/artifact.json --preview --json
```

For an update, set the exact `base_version` and compare with the selected base:

```bash
specgate artifact publish --file .specgate/work/artifact.json \
  --preview --compare "$BASE_ARTIFACT_ID" --json
```

Report added, removed, changed, and unchanged paths. If preview lists an omitted
impact declaration, ask for the exact `yes`, `no`, or `unknown` answers it
requires; never infer `no`. Resolve feature identity from an explicitly named
work item or human selection rather than similarity.

Completion criterion: every selected source appears exactly once in preview
under its unchanged repository-relative path and explicit role; source files
and Git policy are unchanged except for authorized content edits.

Only after explicit human confirmation of that preview may the agent publish:

```bash
specgate artifact publish --file .specgate/work/artifact.json --json
```

Completion criterion: publication succeeded and its artifact ID and immutable
version are recorded. On failure, stop; do not run readiness.

## 3. Complete readiness

```bash
specgate gates check "$ARTIFACT_ID" --json --summary
```

When `dispatched_to_ide_agent.pending_task_ids` is non-empty, complete every
frozen task. List tasks, read each task's `skill_content`, judge only against
that rubric, and write `.specgate/work/gate-<task-id>.json`:

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
specgate gates tasks show <task-id> --json
specgate gates tasks submit-result <task-id> \
  --file .specgate/work/gate-<task-id>.json --json
specgate gates results "$ARTIFACT_ID" --json
```

`aggregate=not_run` means work remains; it is never a pass. Stale digests require
a fresh task. Readiness errors preserve the artifact and become explicit
blockers.

Completion criterion: every pending task has a submitted result for its exact
digests, and the final aggregate plus every remaining gap is recorded.

## 4. Repair without taking ownership

For an authorized content correction, publish a new version using the same
path-preserving manifest, exact `base_version`, comparison, human-confirmed
preview, and readiness loop. Ask the human about ambiguous product intent.
Report out-of-scope gaps without editing their source.

Completion criterion: readiness is acceptable under the stored policy, or each
remaining gap has an explicit human owner and no unauthorized source edit.

## 5. Obtain the human decision

Show the exact immutable snapshot and readiness evidence:

```bash
specgate artifact show "$ARTIFACT_ID" --json
specgate gates results "$ARTIFACT_ID" --json
```

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

Completion criterion: either the human is reviewing the named immutable
artifact, or the approved work reference and matching Context Pack are recorded;
no implementation file was edited in this phase.
