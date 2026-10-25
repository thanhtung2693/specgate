# Try a delivery evidence gap, then repair it

Run a small Local example where existing tests pass but one declared acceptance
criterion has no executed check. Repair the code, test, and completion report,
then see the same work become ready for human acceptance.

This example requires a source checkout, a built SpecGate CLI, Node.js 22 or
newer on `PATH`, and a POSIX `sh` (macOS, Linux, or WSL). Node runs this example's
tests; ordinary Local CLI usage does not require Node, Docker, a server, or
model credentials.

## Review the contract

The demo implements `normalizeName` with three independently named checks:

| Acceptance criterion | Command in the fresh demo directory |
| --- | --- |
| Trim surrounding whitespace | `node --test trim.test.mjs` |
| Lowercase names | `node --test lowercase.test.mjs` |
| Reject empty names after trimming | `node --test reject-empty.test.mjs` |

The driver prints this contract before setup. Review it before passing
`--ack-contract`, which acknowledges these commands and the creation of a
retained Local demo directory. It does not authorize delivery acceptance.

The first stage contains the implementation and tests for the first two
criteria. The third test is absent, its bound check is explicitly `skipped`,
and its claim is `not_done`. SpecGate sees that declared evidence gap; it does
not discover missing behavior by understanding arbitrary source code.

## Run the first stage

From the repository root, build the CLI and start the example:

```bash
make -C app/cli build
node app/cli/test/e2e/local-delivery-proof.mjs start "$PWD/app/cli/bin/specgate" --ack-contract
```

The driver uses existing `init`, `doctor`, `work create-quick`, `work context`,
`delivery report --init`, `change submit --run-checks`, and `change status`
commands. It takes work IDs, criterion IDs, the Context Pack digest, and the
completion path from those CLI responses and scaffold. It installs no plugins.

Every subprocess runs inside a newly created temporary directory outside your
checkout. The driver clears inherited `SPECGATE_*` settings and supplies its own
config, Local store, and workspace selection. `doctor` verifies the exact mode,
store directory, user, and workspace before delivery. Your normal CLI config and
checkout stay outside this demo.

The driver exits successfully only after checking that submission returned
exactly governance exit `1`, a success JSON envelope, and the intended skipped
third-criterion gap. Usage errors, unavailable commands, malformed envelopes,
and other failures stop the driver. Read the printed status: two criteria are
`met`, one is `unclear`, and the next actor is `implementing_agent`.

Keep the printed `Demo directory` and `Repair` command. The directory contains
the toy source/tests, public completion scaffold, `demo-state.json`, and
`transcript.txt` with actual command output and exit codes. The Local store is
retained there too; use the CLI to inspect its state.

## Repair and submit again

Run the exact printed repair command, using your directory:

```bash
node app/cli/test/e2e/local-delivery-proof.mjs repair /your/printed/demo/directory
```

Repair adds empty-name rejection and its test. It also changes the third check
from `skipped` to `pending` with a real command and updates the criterion claim
and citation. The CLI executes all three checks, returning a new completion and
review for the same work and Context Pack. The driver requires submission exit
`0`, three `met` criteria, and `next_actor=human_reviewer`.

Repair refuses to overwrite a modified `normalize.mjs`, an existing new-test
file, or a run already marked repaired. Keep your edits and start a fresh demo
to repeat it. The driver never deletes a run directory.

Before changing repair source or tests, the driver validates the completion
path, JSON, Context Pack digest, and approved check bindings, then prepares the
updated report in memory. If that validation fails, correct the completion and
retry; source, tests, and demo state are unchanged. The printed repair command
quotes both paths so it also works in directories with spaces or apostrophes.

The final status still says `Awaiting human acceptance`. To inspect or make an
explicit human decision later, first use the printed isolated follow-up shell
settings: directory, `SPECGATE_CONFIG_PATH`, `SPECGATE_LOCAL_DIR`, workspace, and
binary `PATH`. Then use the exact next command from your own latest status.
The driver never runs that acceptance command.

## Captured run

These excerpts came from an actual macOS run on 2026-10-25 using a development
CLI build (version `dev`, checkout `84a980e98` plus this change) and Node
`v24.8.0`. IDs and timings vary.
Lines between excerpts are omitted; this is not a simulated terminal recording.

First stage: two existing tests passed; submission exited `1`:

```text
Change: LOCAL-a9ea17d9 — Normalize names
State: implementation
Evidence: Evidence gaps found
Decision: Awaiting human acceptance
Context Pack: 47089f83071a1c05024a166cead1a4bb9037e7ccc62820bf993a95e69754c308
Completion: 9d7b21abd7094d198c52399aab4d4828; review: d3b31b16965781d90f6f810934b8e1c5; peer: not_run
```

The criterion details were:

```text
  [met] Trim surrounding whitespace from names — check "trim" passed (observed by the SpecGate CLI)
    Check: trim (pass); provenance: command_only (CLI observed command; no selected testcase observation)
    Claim: satisfied; citation grounding: grounded
  [met] Lowercase names — check "lowercase" passed (observed by the SpecGate CLI)
    Check: lowercase (pass); provenance: command_only (CLI observed command; no selected testcase observation)
    Claim: satisfied; citation grounding: grounded
  [unclear] Reject empty names after trimming — check "reject-empty" reported "skipped" — cannot verify deterministically
    Check: reject-empty (skipped); provenance: agent_attested (agent-reported)
    Claim: not_done; citation grounding: grounded
    gap: claim: not_done
    gap: bound check reject-empty: skipped
Next actor: implementing_agent
```

After repair: three tests passed; submission exited `0`:

```text
Change: LOCAL-a9ea17d9 — Normalize names
State: awaiting_acceptance
Evidence: Ready for human review
Decision: Awaiting human acceptance
Freshness: No stored receipt was checked against the current checkout because its Git metadata is unavailable.
Context Pack: 47089f83071a1c05024a166cead1a4bb9037e7ccc62820bf993a95e69754c308
Completion: 7b46f558887c5adf7e0d7680db7334c7; review: 5d78582beeb3b40633eb9a01db805f5b; peer: not_run
```

The repaired criterion and handoff were:

```text
  [met] Reject empty names after trimming — check "reject-empty" passed (observed by the SpecGate CLI)
    Check: reject-empty (pass); provenance: command_only (CLI observed command; no selected testcase observation)
    Claim: satisfied; citation grounding: grounded
Next actor: human_reviewer
Missing: Human acceptance
Stale: false
Next: specgate --yes change accept LOCAL-a9ea17d9 --review-id 5d78582beeb3b40633eb9a01db805f5b
```

## Limits and regression checks

The demo uses ordinary `@check` bindings, without a verification pin, JUnit
selectors, peer review, Git receipt, or acceptance decision. Its results are
`command_only`: they establish observed command outcomes, not selected-test
observations or semantic correctness beyond these declared checks. Grounded
citations resolve to actual file lines; they do not prove the criterion.
Four unknown observations remain in the captured repaired status: three
command-only checks and unavailable checkout freshness. Human acceptance
remains a separate decision.

To rerun the driver regression tests after building the CLI:

```bash
node --test app/cli/test/e2e/local-delivery-proof.test.mjs
```

They execute the real CLI for the gap/repair loop, verify isolation with
unrelated-file sentinels and inherited connection settings, reject unexpected
CLI failures, and preserve user edits and repaired runs. Test directories are
retained, just like manual runs.

See [Evidence](../reference/evidence.md), [CLI reference](../reference/cli.md),
and [coding-agent workflow](coding-agent-workflow.md) for normal project work.
