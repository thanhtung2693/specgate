# Connect delivery integrations

Use this guide when you want SpecGate to correlate external delivery signals
with governed work. Integrations are experimental in the alpha release.

## What integrations add

Integrations can contribute:

- tracker links and issue metadata;
- repository or pull-request events;
- corroborated delivery evidence;
- outbound handoff links back to tracker systems.

The core SpecGate loop still works without integrations. You can create work,
publish artifacts, read Context Packs, and submit delivery evidence through the
CLI.

## Before you start

Confirm the stack is healthy:

```bash
specgate doctor
```

Confirm the deployment is reachable from the integration provider if webhooks
are involved. Localhost-only deployments cannot receive public webhook calls
without a tunnel or reverse proxy.

## Connect a provider

Use the web UI settings surface when available:

```bash
specgate open
```

Then open Settings and choose Integrations.

Provider support is alpha and may vary by release. Common setup steps are:

1. create an OAuth application or provider token;
2. configure callback or webhook URLs;
3. store provider credentials in deployment settings;
4. select repositories, projects, or teams;
5. send a test webhook or event;
6. verify SpecGate links the event to a work item.

## Configure webhooks

Use a stable public URL for callbacks and webhooks. If the public URL differs
from the request host seen by SpecGate, configure the deployment’s public
callback origin. See [Configuration reference](../reference/configuration.md).

Webhook secrets should be unique per provider. Do not reuse model API keys,
MCP keys, or database credentials.

## Link events to work

SpecGate correlates external events through identifiers such as:

- change-request ID;
- tracker key;
- issue URL;
- branch, PR, or commit metadata when available.

For reliable matching, include a SpecGate work reference in branch names, PR
titles, commit messages, or tracker links where your workflow permits.

## Use corroborated evidence

Delivery review can distinguish self-reported agent evidence from corroborated
external evidence. For example, a merged PR or CI webhook can strengthen a
completion claim.

When a governance profile requires corroborated evidence, a passing
self-reported completion may be downgraded until matching external evidence is
received.

## Troubleshooting

### Provider connection fails

- check OAuth client ID and secret;
- check callback URL;
- confirm the provider allows the callback origin;
- inspect Doc Registry logs.

### Webhooks return unauthorized

- verify the webhook secret;
- confirm the provider is signing requests as expected;
- check reverse-proxy header forwarding.

### Event is accepted but unmatched

- include the work reference in the PR, branch, commit, or tracker item;
- run `specgate work show <ref>` to confirm the work item exists;
- check whether the selected workspace filters your view.

### Delivery review lacks corroboration

- confirm CI or provider events are reaching SpecGate;
- confirm they include a work reference or linkable tracker/PR metadata;
- rerun `specgate delivery status <work-ref> --detail`.

## Related

- [Evidence reference](../reference/evidence.md)
- [Trust and security](../concepts/trust-and-security.md)
- [Configuration reference](../reference/configuration.md)
