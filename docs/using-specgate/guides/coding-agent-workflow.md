# Use SpecGate with a coding agent

Use this guide when you want a coding agent to implement a SpecGate work item.
The agent will start from approved context, stay within the agreed scope, and
return evidence you can review.

## IDE readiness tasks in Local and Full

For the default Local CLI mode, install the project-local plugin for Codex,
Claude Code, or Cursor. In either topology, the coding agent completes semantic readiness through the same
frozen task loop:

```bash
specgate plugins doctor --agent codex --project-local
specgate gates check <artifact-id> --json --summary
specgate gates tasks list <artifact-id> --json
specgate gates tasks show <task-id> --json
specgate gates tasks submit-result <task-id> \
  --file .specgate/work/gate-<task-id>.json --json
specgate gates results <artifact-id> --json
specgate work list --phase ready --json
specgate work context <work-ref> --json
# Implement only the Context Pack, then:
specgate delivery report <work-ref> --init
specgate change submit <work-ref> --file .specgate/completion-<ref>.json --json
specgate change status <work-ref> --json
# Optional independent evidence review; the completion and peer agents differ:
specgate delivery peer-review <work-ref> --init
specgate delivery peer-review <work-ref> --file .specgate/peer-review-<ref>.json --json
```

`aggregate=not_run` after a successful command means a required semantic task
has not been submitted; it never means readiness passed. The
`dispatched_to_ide_agent.pending_task_ids` receipt is authoritative in both
modes. `created_task_ids` identifies only tasks created by that invocation and
can be empty when a repeated dispatch finds the same pending tasks.

The human approves artifacts and delivery; the coding agent never does. Local
peer review binds to the exact Git receipt submitted with the latest completion.
It remains evidence for the human reviewer—not permission to approve delivery.
Because Local has no browser UI, an agent hands off the work title and stable
ID with an exact CLI action such as
`specgate --yes change accept <work-ref>`; it does not call `specgate open`
or construct a localhost URL.

When deciding whether an exact published spec version has corresponding work
and delivery, use `specgate artifact coverage <artifact-id>`. Artifact coverage
is exact-version evidence, not an inference from filenames or chat history.

## Full appliance workflow

The remaining steps use Full appliance capabilities or an existing SpecGate
server.

## Before the agent starts

Confirm:

- the `specgate` CLI is installed;
- `specgate doctor` passes;
- the correct local user and workspace are selected;
- IDE plugins are installed, or the agent has access to this workflow.

```bash
specgate doctor
specgate user current
specgate workspace current
specgate plugins doctor
```

## 1. Resolve the work item

```bash
specgate work show <work-ref>
```

Use a change-request ID, SpecGate key, tracker key, or supported issue URL.
If the work item is ambiguous, stop and ask for the correct reference.

## 2. Check policy and gate state

```bash
specgate work policy <work-ref>
specgate gates status <work-ref>
```

Policy shows which level of review applies. Gate status shows anything that
still needs attention before implementation or delivery.

## 3. Read the Context Pack

```bash
specgate work context <work-ref>
```

The Context Pack is the agent's brief. Full-route work includes approved
artifact context; quick work uses the persisted intent and acceptance criteria.
It can also contain scope limits, risks, design references, and applicable
skills.

If the Context Pack disagrees with memory, tracker text, or a chat summary, the
Context Pack wins. Stop and ask when that difference looks wrong.

## 4. Fetch extra artifact files only when needed

Use the Context Pack first. Fetch file bodies only when implementation needs the
exact source artifact text:

```bash
specgate artifact files <artifact-id> spec.md verification.md --content
```

Without `--content`, the command prints file references and metadata.

## 5. Implement within scope

Follow the repository's own agent rules. Common project rules include:

- keep docs updated with code changes;
- write or update relevant tests;
- avoid unrelated refactors;
- do not commit secrets;
- do not bypass hooks.

If required context is missing or contradictory, report the blocker instead of
guessing.

## 6. Report documentation updates or ambiguity

When code changes include docs, report that signal:

```bash
cat > /tmp/specgate-docs-updated.json <<JSON
{
  "change_request_id": "<work-ref>",
  "event_type": "coding_agent.docs_updated",
  "severity": "info",
  "summary": "Updated repository documentation to match shipped behavior."
}
JSON

specgate delivery report <work-ref> --file /tmp/specgate-docs-updated.json --json
```

When scope is blocked:

```bash
cat > /tmp/specgate-blocked.json <<JSON
{
  "change_request_id": "<work-ref>",
  "event_type": "coding_agent.blocked_ambiguity",
  "severity": "blocking",
  "summary": "Implementation contract needs clarification."
}
JSON

specgate delivery report <work-ref> --file /tmp/specgate-blocked.json --json
```

## 7. Prepare completion evidence

Scaffold a completion report:

```bash
specgate delivery report <work-ref> --init
```

Fill the `.specgate/completion-<ref>.json` file (`--init` prints its path) with:

- the coding agent's stable name in `agent.name`;
- summary of what changed;
- affected files;
- checks run and their results (`name` is a label; set `command` to the shell
  command when using `--run-checks`);
- evidence for each acceptance criterion.

Make the evidence easy for another person or agent to check: include command
output, test names, file paths, observed UI behavior, API responses, PR links,
or screenshots when relevant.

## 8. Submit delivery

```bash
specgate change submit <work-ref> --file .specgate/completion-<ref>.json
specgate change status <work-ref>
```

`change submit` runs the complete tail and returns the compact actionable
Change status:

1. report completion evidence;
2. run gates;
3. trigger delivery review;
4. return status.

When Change status is `awaiting_review` with **Agent-reported** assurance, do
not treat it as a pass. If the IDE supports subagents, ask a fresh review-only
agent to inspect the Context Pack, checkout, checks, and completion receipt. It
must use the bound scaffold and must not approve work:

```bash
specgate delivery peer-review <work-ref> --init
specgate delivery peer-review <work-ref> --file .specgate/peer-review-<ref>.json
```

The peer review adds useful evidence, but it does not replace human authority.
If a person still needs to act, Full mode may include the URL returned by
`specgate open <work-ref> --print --json`; Local mode instead includes the
stable work ID and the exact CLI action. Never construct a `localhost` link.

## 9. Rework failed review

If review fails:

1. read the failed criterion or gate hint;
2. make the smallest focused fix;
3. update tests and docs if needed;
4. update evidence;
5. run `change submit` again.

Do not mark work complete while delivery review still names unresolved gaps.

## Related

- [Use the SpecGate CLI](cli-workflow.md)
- [Evidence reference](../reference/evidence.md)
- [Artifacts and Context Packs](../concepts/artifacts-and-context-packs.md)
