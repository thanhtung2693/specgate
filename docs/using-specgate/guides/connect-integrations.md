# Connect team integrations

Use this guide to connect a Full-mode workspace to repositories or optional
work tracking. This guide applies to Full mode only. Local mode keeps the direct
CLI and IDE-agent workflow; it has no integration surface.

## Before you start

Confirm the Full deployment is healthy:

```bash
specgate doctor
```

The provider must be able to reach the deployment's OAuth callback and managed
webhook receivers. A localhost-only deployment needs a public tunnel or reverse
proxy. When the public origin differs from the host visible to SpecGate, set
`OAUTH_PUBLIC_CALLBACK_BASE_URL` as described in the
[Configuration reference](../reference/configuration.md).

Open the integration settings:

```bash
specgate open
```

Then choose **Settings → Integrations**.

## Connect repositories

**Repositories** supports GitHub and GitLab. For either provider:

1. Authorize the provider.
2. Select the repository (or GitLab project) that belongs to this workspace.
3. Let SpecGate provision that resource's managed, signed webhook.

Selected resources own their managed webhook credentials. There is no
integration-level webhook secret to configure in SpecGate.

You can instead hand off directly to a coding IDE agent: it reads the same
approved Context Pack through the CLI, implements, and submits delivery evidence
without a work-tracking handoff.

### Link a pull request or merge request

Put this exact marker in the PR or MR description, replacing `CR-123` with the
SpecGate work key or ID:

```html
<!-- specgate-work-ref: CR-123 -->
```

SpecGate does not infer a work item from a branch name, title, commit message,
or ordinary prose.

A merged PR or MR becomes `repository_observed` only when its `head_sha` equals
the latest submitted completion receipt's `git_receipt.head_revision`. Its
`merge_commit_sha` is separate inspection metadata: squash and rebase merges can
produce a different merge commit. An open PR/MR, missing marker, missing head,
or older/different head remains visible but does not corroborate the latest
completion.

## Optionally hand off approved work to Linear

**Work tracking** supports Linear. Authorize Linear, select a team resource,
then choose **Hand off to Linear** from a Ready work item in Full mode.

SpecGate creates or returns one primary Linear issue for that work item. It
includes the summary, acceptance criteria, exact work marker, an authenticated
`specgate work context <key> --json` pickup command, and the PR/MR marker
instruction. Repeating the handoff returns the existing link instead of making
another issue.

The linked Linear issue is informative. After a human accepts delivery in
SpecGate, SpecGate may move that one linked issue to Done; a failure to do so
does not change the acceptance decision.

## Troubleshoot connections and webhooks

### Authorization or resource selection fails

- Confirm the OAuth application credentials are configured on the deployment.
- Confirm the provider allows the public callback origin.
- Reopen Settings → Integrations and retry authorization or resource selection.

### A managed webhook cannot be provisioned or delivered

- Confirm the selected repository, project, or Linear team still exists and is
  accessible to the authorized account.
- Confirm the provider can reach the Full deployment from the public internet.
- If a reverse proxy changes the public origin, set
  `OAUTH_PUBLIC_CALLBACK_BASE_URL` and reprovision the selected resource's
  webhook.
- Inspect the resource's recent webhook delivery state in Settings →
  Integrations and the Doc Registry logs for the provider response.

### A merged PR/MR is linked but does not corroborate delivery

- Confirm its description contains the exact work marker.
- Compare the provider's PR/MR `head_sha` with the latest completion receipt's
  `git_receipt.head_revision` in `specgate delivery status <work-ref> --detail`.
- Submit a new completion after changing the delivered head; an earlier merged
  event cannot observe that newer receipt.

## Related

- [How verification works](../concepts/verification.md)
- [Configuration reference](../reference/configuration.md)
- [CLI workflow](cli-workflow.md)
