# Who SpecGate is for

SpecGate is a Local CLI for solo developers and small teams using coding
agents. It keeps approved scope, criterion evidence, and human decisions
together as features, spec files, and agent sessions change.

## The problem: diffs don't show what's missing

You ask an agent to trim names, convert them to lowercase, and reject empty
names after trimming. The agent reports that the work is done, and both
existing tests pass. The third criterion requires check `@check:reject-empty`.
The missing test leaves that check skipped, so the review shows an evidence
gap for that exact criterion.

The agent adds the validation and test, reruns the commands, and submits
fresh evidence for the same approved work. You inspect the new review and
record your decision. [Run the delivery proof locally](../guides/try-delivery-proof.md).

Review can surface gaps in the criteria and checks you supplied. A command exit
code, an observed selected JUnit case, and a test of the real end-to-end or
provider path answer different questions. See [How verification works](verification.md).

## For solo developers: several specs, one explicit work reference

Hand the agent one work reference and start with
`specgate work resume <ref> --json`. It returns the scope, criteria, approved
document index, and next action. The agent reads the relevant frozen documents
on demand. After you edit a spec, publish and approve a new version if the work
should use it. Existing work retains the criteria and snapshot it started with.

You can approve the scope and review the delivery yourself. Your agent uses
its own reporting identity. Peer review is optional.

## For teams: names on decisions

SpecGate records who approved an exact artifact version and who decided on an
exact delivery review. Local names provide cooperative attribution on a
trusted machine. See [Trust and security](trust-and-security.md).

Each developer keeps a separate SQLite store and coordinates through Git and
their usual PR workflow. A teammate can read an optional unsigned, read-only
[delivery handoff](../reference/cli.md#hand-a-review-to-a-teammate-through-the-repository)
next to the diff. The human decision belongs to the originating store; stores
and decisions do not sync.

## Related

- [Quickstart](../quickstart.md)
- [How SpecGate works](how-specgate-works.md)
- [Feature status](../reference/feature-status.md)
