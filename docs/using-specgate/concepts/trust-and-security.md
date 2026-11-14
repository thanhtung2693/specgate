# Trust and security

SpecGate uses a Local SQLite store. Use it on a trusted machine. Local user
names attribute decisions in the workflow. OS access controls protect the
store and configuration.

## Browser review

`specgate ui` binds to loopback. Local processes and accounts that can reach
the listener are inside its trust boundary. A browser session cookie, exact
Host and Origin checks, and a CSRF header protect actions from other browser
origins. The listener does not authenticate an OS user. Review URLs contain no
session credentials. Stop the foreground command to close the listener.

A browser decision carries the displayed work, review, completion, and
applicable acceptance basis. The store rejects a stale decision and requires
a fresh read. Source excerpts use recorded citations and bounded reads
within the active checkout.

## Evidence and sensitive data

Agent-reported checks, CLI-observed results, checkout comparisons, and human
decisions remain separate. Only run verification commands you trust. They
run as the current OS user. A custom test runner can generate a misleading
report, so review its inputs alongside observed results.

Keep Local state and backups as private as the original specifications and
evidence. Handoff files redact receipt credentials and grounding excerpts.
They still contain work scope and evidence. Inspect an export before committing
it. A handoff is an unsigned review request and cannot record an acceptance
decision in another store.

- [Local operation and backups](../guides/operate-specgate.md)
- [Local browser review](../guides/local-review.md)
- [Evidence reference](../reference/evidence.md)
