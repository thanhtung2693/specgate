# Use SpecGate with a coding agent

Use this guide when a coding agent is ready to implement a SpecGate work item.
The goal is to keep implementation scoped to approved context and return
evidence that delivery review can verify.

## Before the agent edits code

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

Policy tells the agent what governance level applies and what must be true
before implementation or delivery. Gate status shows unresolved quality checks.

## 3. Read the Context Pack

```bash
specgate work context <work-ref>
```

The Context Pack is the implementation contract. It contains the approved
intent, acceptance criteria, scope limits, risks, design references, and
applicable skills.

Do not implement from memory, tracker text, or chat summary when the Context
Pack disagrees.

## 4. Fetch extra artifact files only when needed

Use the Context Pack first. Fetch file bodies only when implementation needs the
exact source artifact text:

```bash
specgate artifact files <artifact-id> spec.md verification.md --content
```

Without `--content`, the command prints file references and metadata.

## 5. Implement within scope

Follow the repository’s own agent rules. In this repo, that means:

- keep docs updated with code changes;
- write or update relevant tests;
- avoid unrelated refactors;
- do not commit secrets;
- do not bypass hooks.

If required context is missing or contradictory, record the blocker instead of
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

Fill `completion.json` with:

- summary of what changed;
- affected files;
- checks run and their results;
- evidence for each acceptance criterion.

Evidence should be concrete: command output, test names, file paths, UI behavior,
API responses, PR links, or screenshots when relevant.

## 8. Submit delivery

```bash
specgate delivery submit <work-ref> --file completion.json
specgate delivery status <work-ref> --detail
```

`delivery submit` runs the complete tail:

1. report completion evidence;
2. run gates;
3. trigger delivery review;
4. return status.

## 9. Rework failed review

If review fails:

1. read the failed criterion or gate hint;
2. make the smallest focused fix;
3. update tests and docs if needed;
4. update evidence;
5. run `delivery submit` again.

Do not mark work complete while delivery review still names unresolved gaps.

## MCP boundary

Doc Registry exposes MCP tools for embedded IDE integrations. For the normal
coding-agent workflow, prefer the CLI commands in this guide. The CLI gives
stable output modes, workspace selection, and delivery-report scaffolding.

## Related

- [Use the SpecGate CLI](cli-workflow.md)
- [Evidence reference](../reference/evidence.md)
- [Artifacts and Context Packs](../concepts/artifacts-and-context-packs.md)
