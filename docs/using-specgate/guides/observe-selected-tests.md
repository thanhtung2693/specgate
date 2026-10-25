# Observe selected tests in Local mode

A command can exit zero while omitting the test you agreed to run. This
walkthrough makes that gap visible, adds the missing test, and inspects the
new evidence without accepting it automatically.

Use SpecGate v0.2.6 or newer, Git, `sh`, and Node.js with the built-in JUnit
reporter. The example was exercised with Node.js v24.8.0. It needs no npm
packages, server, Docker, model key, or Git remote. These are observations of
your local runner, not proof of test quality or correct code.

## 1. Create an isolated scratch repository

Open a fresh terminal for this walkthrough. The config and store below belong
only to its temporary directory. Close that terminal when finished to discard
its environment overrides; keep the directory if you want the evidence later.

```sh
TUTORIAL_DIR=$(mktemp -d)
cd "$TUTORIAL_DIR"
unset SPECGATE_SERVER SPECGATE_LOCAL_DIR SPECGATE_WORKSPACE
export SPECGATE_CONFIG_PATH="$TUTORIAL_DIR/.specgate/tutorial-config.json"
git init -q
```

Create `health.mjs`:

```js
export function status(healthy) {
  return healthy ? 200 : 503;
}
```

Create `health.test.mjs` with only the healthy control for now:

```js
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { status } from "./health.mjs";

describe("health", () => {
  it("healthy returns 200", () => {
    assert.equal(status(true), 200);
  });
});
```

The `describe` wrapper produces a `testsuite` containing `testcase` elements,
which is part of SpecGate's supported JUnit subset. Inspect the actual XML;
do not infer selector identities from test filenames:

```sh
node --test --test-reporter=junit health.test.mjs
git add health.mjs health.test.mjs
git -c user.name="Tutorial fixture" -c user.email=fixture@example.test \
  -c commit.gpgsign=false commit -qm "tutorial baseline"
specgate init --mode local --workspace-name "Selected test tutorial" \
  --username tutorial-user --display-name "Tutorial user" \
  --local-dir "$TUTORIAL_DIR/.specgate/local" --json
specgate doctor --json
```

The XML contains `classname="test"`, `name="healthy returns 200"`. Use the
actual classname emitted by your Node version if it differs. Doctor should
report `mode=local`, the tutorial workspace and scratch store, and a bound
repository. Optional IDE plugin diagnostics are separate from this exercise.

## 2. Create work and review an exact verification pin

```sh
specgate work create-quick "Verify unhealthy status" \
  --ac "Unhealthy requests return 503 @check:unit" --json
```

Copy `data.key` into `WORK_REF` and `data.context_digest` into the JSON below:

```sh
WORK_REF="<exact data.key>"
mkdir -p .specgate/work
```

Create `.specgate/work/checks.json`, replacing the digest placeholder:

```json
{
  "context_digest": "<exact data.context_digest>",
  "shell": "sh",
  "watched_paths": ["health.mjs", "health.test.mjs"],
  "checks": [{
    "name": "unit",
    "command": "node --test --test-reporter=junit --test-reporter-destination=\"$SPECGATE_TEST_REPORT\" health.test.mjs",
    "cwd": ".",
    "test_report": {
      "format": "junit",
      "selectors": {
        "local-1": [{"classname": "test", "name": "unhealthy returns 503"}]
      }
    }
  }]
}
```

`local-1` is this work's first persisted criterion. Its requested testcase is
deliberately absent. The literal command must retain `$SPECGATE_TEST_REPORT`;
SpecGate supplies a fresh ignored path only when it executes the check.

```sh
specgate work verification "$WORK_REF" --file .specgate/work/checks.json \
  --dry-run --json
```

Review the command, directory, selector, watched hashes, and `store_upgrade`.
The first enhanced write backs up and upgrades the **entire scratch store**;
old CLIs cannot write it afterward, and v1 portable/handoff exports cannot
carry these records. Commands run through an unsandboxed shell. If you agree
to this tutorial pin, confirm it and record a pickup point:

```sh
specgate --yes work verification "$WORK_REF" \
  --file .specgate/work/checks.json --json
specgate --yes work checkpoint "$WORK_REF" \
  --note "Before adding the missing test" --json
```

Copy the checkpoint's `data.id` into `CHECKPOINT_ID`. Pins cannot be edited
after confirmation; this exercise will change the tests, not the pinned command.

## 3. Observe the missing selected case

```sh
CHECKPOINT_ID="<exact checkpoint data.id>"
specgate delivery report "$WORK_REF" --init --json
COMPLETION_PATH="<exact scaffold data.path>"
```

Edit that scaffold: set `agent.name` to `tutorial-agent`, provide a summary,
and keep its work/context/pin IDs and generated `checks` unchanged. Keep the
check `pending`; execution will replace it with the observed outcome. For its
first criterion, keep `claim: "not_done"` and set `evidence` to:

```json
{"kind":"code","path":"health.mjs","line":1}
```

```sh
specgate change submit "$WORK_REF" --file "$COMPLETION_PATH" \
  --run-checks --yes --json
```

Expect exit **1** with recorded evidence, `state=implementation`, a failed
bound check, and an unmet criterion whose reason includes
`selected testcase is missing`. The Node command itself exited zero. The
report lacked the exact required case, so command success was insufficient.

## 4. Add the missing case and resume

Add this test **inside the existing `describe` block**:

```js
it("unhealthy returns 503", () => {
  assert.equal(status(false), 503);
});
```

```sh
specgate work resume "$WORK_REF" --since "$CHECKPOINT_ID" --json
```

Expect `delta.state=changed` with `health.test.mjs` modified. The work's
criterion and Context Pack digest are unchanged. The prior failed evidence
is now stale against the edited checkout; it has not become a pass.

Update the same completion draft's summary, set its criterion claim to
`satisfied`, and set `affected_files` to `["health.test.mjs"]`. Keep the
original IDs, pinned command, and evidence anchor. Then rerun:

```sh
specgate change submit "$WORK_REF" --file "$COMPLETION_PATH" \
  --run-checks --yes --json
```

Expect a passing bound check, `check_provenance=selected_test_observed`, and
the exact selected case with `outcome=passed`. This creates a new completion
and review; the old missing-case result remains historical evidence.

## 5. Review the basis before any human decision

```sh
specgate change status "$WORK_REF" --checkpoint "$CHECKPOINT_ID" --detail --json
```

Inspect the criterion, selected test, checkpoint delta, freshness, and risks.
`health.test.mjs` is still **changed against the pin** because you added the
test after pinning. Inspect that legitimate change; a new passing run does
not erase watched-input drift. The snapshot endpoints can match without
proving continuous immutability or an honest runner.

In this fixture, status should show `awaiting_acceptance` and
`next_actor=human_reviewer`. The returned `data.next_command` includes the
exact `--review-id`, `--basis-digest`, and `--checkpoint`. Only the human who
reviewed this material should choose whether to run it. Copy the whole command;
do not construct a new digest or reuse a token after changing evidence.
Selecting a checkpoint for resume alone did not select it for acceptance.

Acceptance can record a human's deliberate risk decision, including retained
evidence gaps. It cannot turn the old failed result into passing tests. This
walkthrough stops before that human decision.

## Use this in your project

After reviewing your own runner's XML, choose exact selectors for each bound
criterion and watch only files whose changes you want surfaced. Skip pinning
when the work does not need that reviewed agreement. See the
[CLI workflow](cli-workflow.md), [evidence reference](../reference/evidence.md),
and [Local backup guidance](operate-specgate.md#operate-local-cli).
