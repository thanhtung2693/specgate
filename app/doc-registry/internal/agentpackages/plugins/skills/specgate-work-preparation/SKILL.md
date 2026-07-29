---
name: specgate-work-preparation
description: Use when preparing a request or existing source documents for SpecGate approval, creating quick SpecGate work, publishing an artifact version, or repairing artifact readiness before implementation.
---

# Preparing Work

Apply the [SpecGate operating contract](../specgate/SKILL.md#operating-contract).
This phase produces an approved implementation handoff; it never implements
product code.

## 1. Define the contract and route

Read the request, governing repository instructions, and author-selected source
documents. Show the human the exact title, description, observable acceptance
criteria, and non-goals. Split work that can be accepted independently. Use an
`@check:<name>` binding only when the human confirms that exact check, and name
in the preview which criteria a check enforces and which rest on your claim.

Choose one route with the human:

- **Quick work** for a small change that does not need a governed source snapshot.
- **Artifact-backed work** when an existing spec, design, plan, verification
  document, or other source must be versioned and approved.

Do not create a durable record until the human approves the displayed contract.

Completion criterion: the human-approved preview names every slice's contract and route.

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
Never relocate, copy, rename, delete, commit, or change their ignore rules. Edit
source content only when the request authorizes it.

Roles are routing labels, not one-file-per-concern requirements. Map each
human-selected source explicitly; never detect a framework or manufacture
documents. When policy requires a role one source already covers,
map that same `path` again under the second `role` — one spec is often both spec
and plan.

Keep the transient manifest at `.specgate/work/artifact.json`. For each mapped
document:

- set `path` to its unchanged repository-relative POSIX path;
- set its explicit governance `role`;
- use `repo_file` for a repository source;
- use `source_file` only inside the manifest directory, and an absolute
  `file_url` outside it;
- set exactly one of `content`, `repo_file`, `source_file`, or `file_url`.

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

Show the source mapping and exact policy projection together, reporting added,
removed, changed, and unchanged paths. If preview lists an omitted impact
declaration, ask for its exact `yes`, `no`, or `unknown` answers; never infer
`no`. Resolve feature identity from an explicitly named work item or human selection,
never from similarity.

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
specgate gates tasks submit-result <task-id> \
  --file .specgate/work/gate-<task-id>.json --json
```

`tasks list` already returns each task's `skill_content`, `gate_digest`, and
`artifact_digest`; do not call `tasks show` for a task it listed. Copy both
digests exactly — a mismatched digest leaves the gate `not_run`.

`aggregate=not_run` means work remains; it is never a pass. Stale digests need a
fresh task. Readiness errors preserve the artifact and become explicit
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
```

Reuse the readiness rows already read in section 3; do not refetch them. Render
every gate before asking, preserving each state and hint verbatim:

```text
SpecGate readiness — <artifact-id> <version> (<aggregate>)
[<state>] <gate> — <hint>
Not yet judged: <count>
```

Never summarize a `fail`, `warn`, `needs_human_review`, or `not_run` gate as
acceptable, and never present an aggregate without its per-gate lines.

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
