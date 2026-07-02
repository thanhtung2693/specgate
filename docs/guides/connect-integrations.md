# Connect delivery integrations

> **Experimental — in development.** Integrations are available today.
> They are opt-in while the setup flow, provider coverage, and event contracts
> mature. See [Feature status](../features.md).

Integrations bring tracker and delivery signals into SpecGate. They add
corroboration; they do not replace acceptance-criteria evidence.

## What integrations contribute

Depending on provider, SpecGate can:

- link issues, pull requests, or merge requests to work items;
- record opened, merged, closed, or completed delivery signals;
- receive successful GitHub workflow-run evidence;
- detect scope-drift comments;
- hand a SpecGate work item to a tracker;
- update linked issue state after successful delivery review.

## Connect an integration

Choose a provider (GitHub, GitLab, or Linear), authenticate with that provider,
then select the repositories, projects, teams, or other resources SpecGate
should observe.

SpecGate supports one connection per provider, with multiple resources under
that connection where applicable.

## Connect GitHub

GitHub supports:

- repository selection;
- pull-request delivery events;
- issue-comment scope signals;
- successful `workflow_run` evidence;
- outbound issue handoff;
- closing linked issues after a passing delivery review.

Use OAuth where configured, or a token with only the repository permissions your
workflow needs.

GitHub webhook requests are verified using `X-Hub-Signature-256`.

## Connect GitLab

GitLab supports:

- project selection;
- merge-request delivery events;
- outbound issue handoff;
- closing linked issues after a passing delivery review.

OAuth and access-token paths are supported by deployment configuration.

GitLab webhook requests are checked against the configured signing token and
timestamp.

## Connect Linear

Linear supports:

- team and optional project selection;
- issue-state synchronization;
- outbound issue handoff;
- moving linked issues to a completed state after passing delivery review.

Linear is useful as a planning/tracker layer. Git provider evidence may still
provide stronger delivery corroboration.

## Select repositories, projects, or teams

Provider connection grants access. Resource selection determines which external
work SpecGate should observe.

Use the smallest useful scope:

- select only repositories participating in governed delivery;
- choose the Linear team owning the work;
- avoid broad organization tokens when a narrower credential works.

## Webhooks and secrets

SpecGate stores or derives a per-integration webhook secret. Provider events are
authenticated before processing.

Do not place webhook secrets in repository files. Rotate a secret when it may
have been exposed, then update both provider and SpecGate configuration.

Provider retries are deduplicated. A repeated delivery ID does not create a
second evidence event.

## How SpecGate links events to work

SpecGate uses work references, branch/URL information, tracker links, and
provider metadata to match an external event to a work item.

Unmatched events remain visible for diagnosis rather than being attached to a
random item.

For reliable matching:

- preserve the SpecGate work key in tracker and branch context;
- use generated tracker handoff where possible;
- avoid removing correlation data from issue or PR descriptions.

## Outbound tracker handoff

A human initiates the handoff for a ready work item. SpecGate creates the
external issue and preserves the link.

The tracker remains a coordination surface. The approved Context Pack remains
the implementation contract.

## Troubleshooting

### Provider connection fails

Check OAuth client configuration, redirect origin, token scope, and provider
account permissions.

### Webhooks return unauthorized

Confirm provider and SpecGate hold the same secret, timestamp is current, and a
proxy has not rewritten the raw request body.

### Event is accepted but unmatched

Check work key, branch name, issue URL, selected repository/project, and tracker
link.

### Delivery review lacks corroboration

Confirm the relevant repository is selected and provider sent the expected
merge or workflow event. A builder’s own report remains agent-attested until an
independent channel adds evidence.

## Continue

- [Trust and security](../concepts/trust-and-security.md)
