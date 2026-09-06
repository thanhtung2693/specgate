# Who SpecGate is for

Your coding agent is brilliant, fast, tireless — and grades its own homework.
SpecGate is the part of the workflow that says: *lovely summary, now show me
each criterion.*

That's the whole product. Solo, it plays the reviewer you don't have. In a
team, it ends the "wait, which version did we approve?" argument before it
starts. The core delivery loop runs in Local mode: one binary, no Docker, no
server, and no model API key. Shared gateway credentials and hash-linked event
verification require Full mode.

## The problem: diffs don't show what's missing

Hand an agent eight acceptance criteria. It implements six, writes tests for
those six, and announces success with total confidence.

Now you do the responsible thing. You read the diff — looks great. You run the
tests — all green. Ship it?

The two missing criteria are *absent code*. There is no red line in a diff for
code that was never written. You just reviewed what the agent did, and the
agent — who wrote both the code *and* the summary you checked it against — did
not bring up what it skipped. Funny how that works.

What SpecGate does about it:

- Review walks your criteria **one by one**. No single "looks good ✅" is
  accepted on behalf of eight separate promises.
- A criterion tied to a check (`@check:<name>`) takes its verdict from the
  check result. The agent's opinion of its own test run is noted, then
  re-verified.
- On submit, SpecGate **reruns the commands the agent claims it ran** — the
  installed skills always submit this way. Claims that don't reproduce get
  corrected, on the record. Every verdict says
  whether a human-visible result was *observed* or merely *reported*.
- Citing a test that doesn't exist gets flagged. (Yes, agents do this. With
  citations. It's very convincing.)
- And if you typo a check name, SpecGate refuses right there instead of
  quietly skipping the check and letting you find out in production.

## The pin: "whatever the file says right now" is not an approval

You had an idea at 1pm. The agent started at noon. The spec file changed under
it, the agent re-read it, and now you're reviewing an implementation of one
and a half specs.

SpecGate freezes the version you approve. The context the agent receives is
generated *from that frozen copy*, and review is keyed to the criteria saved
*at approval*. Editing the file mid-flight can't move the goalposts, because
the goalposts were confiscated at approval time.

Nice side effect: the spec file in your repo can stay uncommitted, get
mangled, or be deleted in a fit of tidiness — the approved snapshot survives.
SpecGate never touches your files. Not even to help.

## For solo developers: several specs, one explicit work reference

For a solo developer juggling several specs or plans, hand the agent one work
reference and start with `specgate work resume <ref> --json`. It returns the
scope, criteria, pinned document index, and next action. The agent reads the
relevant approved documents on demand; each work item remains separate even
when several share a spec. This reduces repeated context loading without
relying on the agent to remember a previous conversation.

## For teams: names on decisions

Somewhere out there, a spec is "approved" as a 👍 on a Slack link to a Notion
page that has changed twice since. Three weeks later: "I approved v2." "You
approved v3." Nobody can prove anything, and the rebuild costs a sprint.

SpecGate records a **named approval against one frozen version** and a
**named acceptance against one exact completion**, and `specgate audit` replays
that trail in order: who decided what, against which version, when. In Full
mode each event hash-links to the one before it, so `audit --verify` can tell
you the record was not rewritten underneath you — and names the first event that
was, if it happens.

Fair's fair, though — if your team already reviews specs as pull requests with
branch protection, GitHub does named approvals and no-self-approval very well
for anything that becomes a PR. SpecGate covers the part that never does: the
intent you approved *before* code existed, and the agent's account of itself
in between.

## Things your existing tools already do well

We're not here to reinvent your stack. Keep all of it:

- **Git** freezes files. It's rather famous for it. Commit the specs you love.
- **Branch protection + PR review** handle approval of *code*.
- **CI** reruns your suite on the exact commit, every push, without being asked.
- **"Fixes #123"** links PRs to issues just fine.
- **CLAUDE.md, plan files, session resume** carry context between chats well
  enough for most days.
- **TDD** deserves special mention: a failing test *is* a red line for absent
  code, which closes half of the missing-criteria problem. The other half
  moves up a floor — the same agent writes the tests, so given eight criteria
  it can write six tests, implement six, and hand you a suite that is 100%
  green and 75% complete. A green checklist written by the party under review
  is still the same checklist. SpecGate pins the criteria list at approval
  (the agent can't edit what it's graded against) and shows you which
  criteria have no test standing behind them at all. Best of both: TDD gives
  each criterion its own test, `@check:` binds each criterion to it.

The gap in that stack is small but load-bearing: between "a human approved
this intent" and "the code merged", the only account of what happened is
written by the agent itself. That account is the thing SpecGate verifies.

## Limits, before you find them yourself

- **Names are recorded, not proven.** SpecGate has no login. The chain proves
  the record was not edited after the fact; it does not prove who was at the
  keyboard, because the name on a decision is whatever the caller supplied, and
  the service trusts anyone who can reach it. Run it on a trusted network. One
  coding agent cannot peer-review its own completion, and whoever filed the
  completion cannot approve it either — both are enforced by name — but a
  determined impersonator only has to type a different name.
- **The Local trail has no chain.** One laptop, one SQLite file: the trail
  replays, but `--verify` has nothing to recompute and says so rather than
  answering.
- **Staleness detection needs a Git remote.** A repo that has never been
  pushed gets no "this evidence is older than your code" warning — and
  SpecGate tells you so instead of pretending.
- **Local mode is one machine.** Sharing state means exporting into Full
  mode; Local stores don't sync between laptops, however nicely you ask.

## Who should close this tab

- Weekend prototypes. If a shipped bug costs you nothing, every ceremony
  costs you something. Go build the thing.
- Work with no testable acceptance criteria — design spikes, exploratory
  refactors. SpecGate verifies criteria; no criteria, nothing to verify.
- One-file fixes. The loop is bigger than the task. Just read the diff — for
  one file, that actually works.

## Related

- [Quickstart](../quickstart.md)
- [How SpecGate works](how-specgate-works.md)
- [Feature status](../reference/feature-status.md)
- [Trust and security](trust-and-security.md)
