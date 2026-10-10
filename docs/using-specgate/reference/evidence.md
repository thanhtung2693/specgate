# Evidence reference

Delivery evidence tells SpecGate what changed and why the acceptance criteria
should be considered satisfied.

Git receipts preserve committed filenames literally; a file named `[]` is a
normal changed file, not an empty-result marker.

When reading stored Git receipts, missing or null string metadata remains
absent; non-string values are not coerced into Git identities. A receipt without
a HEAD cannot establish checkout freshness. Legacy receipts without an explicit
freshness scope retain the shared-repository comparison behavior, using only
the fields actually present. This does not supply missing evidence or upgrade
an incomplete receipt into a checkpoint baseline.

For an originless `local_checkout` receipt, the recorded `checkout_id` must
match the current checkout before comparing branch, HEAD, and working-tree
digest. Equal endpoints in two different checkouts do not establish a match.
Missing or different checkout identity leaves freshness unverified rather
than declaring the evidence stale or changing historical test outcomes.

New Git receipts use credential-free repository coordinates: URL userinfo,
query and fragment data are removed; SSH usernames and scp-style coordinates
remain supported, but SSH passwords are removed. Origins that cannot be safely
projected, such as opaque remote helpers, retain only local-checkout provenance.
Handoff and portable exports also redact repository credentials in legacy
completion/peer receipt copies. Stored history and opaque `diff_digest` values
are not rewritten. A pre-fix credential-bearing receipt can therefore become
stale against a fresh checkout observation; submit fresh completion evidence
instead of treating that old receipt as newly verified.

Peer-review scaffolding and submission refuse credential-bearing legacy
completion bindings. Submit a fresh completion first; SpecGate does not rewrite
the receipt that a peer review must match exactly. Portable import verifies the
original bundle checksum before projecting both completion and peer receipt
copies for the destination. The source bundle is not modified, and imported
peer reviews retain their exact receipt binding to the imported completion.

## Completion report

Create a scaffold:

```bash
specgate delivery report <work-ref> --init
```

The scaffold includes one `criteria[]` entry per acceptance criterion.

Typical shape:

```json
{
  "change_request_id": "CR-123",
  "event_type": "coding_agent.completed",
  "severity": "info",
  "summary": "Implemented the healthcheck endpoint.",
  "agent": {
    "name": "codex"
  },
  "checks": [
    {
      "name": "tests",
      "status": "pass",
      "command": "go test ./internal/health -count=1",
      "detail": "go test ./internal/health -count=1"
    }
  ],
  "criteria": [
    {
      "criterion_id": "ac-1",
      "text": "GET /healthz returns 200 when the service is up",
      "claim": "satisfied",
      "evidence": {
        "kind": "test",
        "path": "internal/health/handler_test.go"
      }
    }
  ],
  "affected_files": [
    "internal/health/handler.go",
    "internal/health/handler_test.go"
  ]
}
```

## Field notes

| Field | Purpose |
|---|---|
| `change_request_id` | durable work identity populated by the scaffold command; verify it before reusing an existing scaffold |
| `agent.name` | required stable name of the coding agent submitting this completion |
| `summary` | concise delivery summary |
| `checks[]` | tests, builds, lint, type checks, manual checks |
| `criteria[]` | per-acceptance-criterion claim and evidence |
| `affected_files[]` | files changed by the implementation |
| `severity` | signal severity for feedback events |

`checks[].status` values are:

- `pass`
- `fail`
- `skipped`

Values must use this exact wire contract; aliases such as `passed`, `failed`,
and `skip` are rejected.

`claim` values are:

- `satisfied`
- `partial`
- `not_done`

Claims must use these exact values.

Each `criteria[].evidence` value is an object. Use `kind` plus a local `path`
when the proof is in the checkout; optional `line`, `heading`, `url`, and
`file_key` make a citation more precise. The CLI verifies local evidence paths
and records a digest plus a `grounding.status` unless `--skip-evidence-check` is
explicitly used. An excerpt accompanies the digest only when `line` or a
`heading` actually present in the file anchors the citation; otherwise the
status is `heading_not_found`, `line_out_of_range`, `empty_at_anchor`, or
`unanchored`, and a reviewer can see that the citation did not resolve to
content.

Those field names are the whole contract: `kind`, `path`, `line`, `heading`,
`url`, `file_key`, and the CLI-stamped `grounding`. Any other field is rejected
before submission in both modes, so a completion accepted in Local mode is not
refused later by a Full appliance.

## Bound criteria

Local acceptance bases retain current-checkout `freshness` and `peer_review`
state alongside criterion results, identities, and gaps. `matching_endpoints`
means the observed endpoints match, not continuous immutability. `stale`,
`noncomparable`, and `unavailable` stay inspectable in the historical basis
even when a human accepts residual risk. The original test outcomes do not
change with that decision.
Compact Local acceptance risk counts show `unavailable` and `noncomparable`
current freshness as unknown even when all historical selected tests passed
and watched files remain unchanged.
Legacy records without that basis state still report an unknown risk when the
current checkout comparison cannot run or its local identity is noncomparable.

Full-mode `delivery report` and `delivery submit` reject Local-only
`test_report`, `test_observation`, and `test_run` check fields before network
calls. Supply ordinary command-level completion evidence in Full mode.

An acceptance criterion carrying `@check:<name>` is verified from the named
`checks[]` row rather than from its prose claim, in both Local and Full mode.
The stored acceptance criterion is the authority for the binding, so a
completion cannot escape enforcement by omitting `verification_binding`.

The binding may sit anywhere in the criterion, is matched case-insensitively,
and may carry trailing punctuation — `@check:unit.`, `@CHECK:unit`, and a
leading `@check:unit` all bind to `unit`. What SpecGate will not do is guess.
A criterion is rejected when it is written if its binding cannot be resolved:
`@check:` with a space before the name, two bindings on one criterion, `@check`
without the colon, or a binding with no criterion text left. Silently treating
those as unbound would leave a criterion its author believes is enforced by a
deterministic check while review judged it on the agent's own claim.

| Bound check | Result |
|---|---|
| `pass` | criterion met |
| `fail` | criterion unmet |
| `skipped` or absent from `checks[]` | not verifiable — Full mode returns `needs_human_review`, Local mode fails the review and names the criterion and check |

Enforcement covers a missing or skipped check. It does not detect a falsely
claimed `pass`: `checks[].status` is agent-reported unless the submission ran
with `change submit --run-checks`, which re-executes each non-skipped command
and records the superseded value in `checks[].claimed_status`.

## Evidence quality

Good evidence is concrete:

- command output;
- test names;
- API response details;
- UI behavior;
- file paths;
- PR, commit, or CI links;
- screenshot or recording references when visual behavior matters.

Weak evidence is vague:

- "done";
- "looks good";
- "tests pass" without naming the command;
- a summary that does not mention acceptance criteria.

## Local observed JUnit evidence

Local verification pins may opt in to a `test_report` for a check. The only
supported format is a bounded JUnit subset with exact `(classname, name)`
selectors mapped to existing criterion IDs. `change submit --run-checks` gives
the reviewed command a fresh, ignored `SPECGATE_TEST_REPORT` path and stores
only normalized case outcomes and digests. It does not install a runner, select
tests, retain XML or command output, or prove that a runner did not fabricate a
report.

An exit code of zero is not enough for a report-enabled check. Missing,
malformed, duplicate, selected skipped, or any failing/error case makes the
check fail. Observed passed/failed/skipped totals retain unselected skips
without treating them as a failure. Before/after checkout and explicitly watched-file observations show
endpoint drift; equal endpoints do not prove that nothing changed during a run.
For a criterion bound to that check, `change submit` and detailed status include
the CLI-observed JUnit failure reason (for example, that a required testcase is
missing), rather than only the generic failed-check summary.
Human acceptance remains a separate decision and does not turn failed evidence
into a pass. See [the CLI reference](cli.md#local-resume-and-verification-contracts)
for the report limits, cleanup rules, and supported recovery.

## Rework loop

If delivery review fails:

1. read the failed criterion or gate hint;
2. fix the smallest named gap;
3. rerun relevant checks;
4. update the completion report;
5. run `specgate delivery submit` again.

## Related

- [Use SpecGate with a coding agent](../guides/coding-agent-workflow.md)
- [Governance and gates](../concepts/governance-and-gates.md)
- [CLI reference](cli.md)
