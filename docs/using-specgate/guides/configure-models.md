# Configure models

Use this guide when you want SpecGate to run server-side assistive or
model-backed governance features.

You can use SpecGate without a model. The deterministic core still stores
artifacts, resolves policy, produces Context Packs, and records delivery
evidence.

## What runs in each mode

You can start with no API key. Local IDE-agent semantic readiness and Full
model-less readiness both dispatch frozen tasks for the coding agent; the agent
submits a digest-bound result that SpecGate stamps `agent_attested`. The IDE
agent can read the checkout, but that is repository context, not Governance
Knowledge. Add a platform model when you want independent platform evaluation;
it does not silently upgrade the trust of an IDE result.

| Capability | Local + IDE agent | Full without platform model | Full with platform model |
|---|---|---|---|
| Deterministic workflow and checks | CLI/SQLite | Server | Server |
| Artifact semantic readiness | IDE task, `agent_attested` | IDE task, `agent_attested` | Platform model, `platform_evaluated` |
| Quick-work AC drafting | IDE preview, human confirms | IDE preview, human confirms | IDE preview or platform draft; human confirms |
| Delivery semantic review | different review-only agent or human | different review-only agent or human | platform review; human authority unchanged |
| Explanations and summaries | ephemeral IDE answer | ephemeral IDE answer or governance chat | ephemeral IDE answer or governance chat |
| Governance chat | unavailable | requires chat model | requires chat model |
| Semantic Knowledge | repository context, not Governance Knowledge | requires embeddings/pgvector | requires embeddings/pgvector |

The IDE agent is the natural executor for repository-grounded evidence because
it can re-run checkout commands locally. A different review-only agent may add
delivery evidence, but human approval remains separate.

Two model configurations exist and are independent: the **governance ops
model** for gates, delivery review, and Full-mode quick-work AC drafting (set
via `specgate model set`, stored encrypted in server settings) and the
**governance chat model** (set via
`GOVERNANCE_OPS_MODEL_PROVIDER` / `GOVERNANCE_OPS_MODEL` /
`GOVERNANCE_OPS_API_KEY` env vars on the agents service). Configuring one does
not configure the other.

The coding IDE agent can still perform scoping and implementation work through
SpecGate skills without a server-side model.

## Configure through the CLI

Guided setup:

```bash
specgate model set
```

The CLI asks for provider, model, and API key. API key input is masked.

Non-interactive setup:

```bash
# Set OPENAI_API_KEY through your shell or CI secret manager first.
specgate model set --provider openai --model gpt-5.4-mini
```

Use `GOOGLE_API_KEY`, `ANTHROPIC_API_KEY`, or `OPENROUTER_API_KEY` for the
other providers. Provider environment variables keep secrets out of process
arguments; use your shell or CI secret manager to avoid storing them in command
history. For safety, `model set` does not use a server URL from the current
repository's `.specgate/config`; select the trusted destination with `--server`,
`SPECGATE_SERVER`, or saved user configuration.

Provider names:

| Provider | Value |
|---|---|
| OpenAI | `openai` |
| Anthropic | `anthropic` |
| Google | `google` |
| OpenRouter | `openrouter` |

For OpenRouter, guided setup opens a searchable model picker filtered by the
catalog's structured text-output metadata. Choose manual entry when the catalog
omits a model you need or you want an exact model id. For every provider, CLI
and Web UI treat an explicit ID as opaque provider input; built-in IDs are
suggestions, and SpecGate does not guess validity from naming conventions.

## Set reasoning effort

The governance model's reasoning effort (`governance.default_thinking_level`) is
the main quality/latency lever for gates, delivery review, and summaries:

```bash
specgate model set --provider openai --thinking-level medium
```

Allowed values are `low`, `medium`, and `high`. Unsupported providers ignore or
reject provider-specific options according to server validation.

**Choosing a level.** `low` is the runtime default so default installs stay
fast and stream-friendly. `medium` is the recommended first upgrade for
delivery review and gates when you configure a server-side model:

| Level | Behavior | Use when |
| --- | --- | --- |
| `low` | Fastest, but over-literal — can wrongly reject a correct delivery (e.g. treats an optional or extra schema field as a violation). **Default.** | Cheap, low-stakes summaries and latency-sensitive local evaluation. |
| `medium` | Well-calibrated and fast; asks for more evidence on thin reports instead of wrongly rejecting. | Delivery review and gates once a server-side model is configured. |
| `high` | Most thorough, but noticeably slower per criterion. | Occasional deep review of a high-impact change. |

For stronger enterprise/team gates, also step up the *model* (e.g. a `-pro`
variant) rather than only the thinking level. Reserve objective, checkable
criteria (schema/field presence, "tests ran") for deterministic checks the judge
cannot misread — see [Verification](../concepts/verification.md).

`specgate doctor` shows the active level on the `Model:` line
(`... (thinking: medium)`), so you can confirm the lever without opening settings.

## Check model-backed features

Run:

```bash
specgate doctor
specgate model test
specgate gates check <artifact-id>
specgate delivery submit <work-ref> --file .specgate/completion-<ref>.json
```

`model test` is a settings-only check: it verifies that provider, model, and
API-key settings are present and does not contact the model provider. If no
model is configured, deterministic flows still work. Artifact readiness gates
can be dispatched to the IDE agent for an agent-attested review path; delivery
review reports an `agent_attested` verdict. A passing attested delivery review
pauses for a fresh peer-agent review of the exact completion receipt (or human
review); it does not silently pass. Scaffold and submit that review with
`specgate delivery peer-review <ref> --init` then `--file`. The peer is a
cooperative review signal, not an independent approval.

To deliberately use this path, select **Model-less** in Settings → Models or
run `specgate model off`. This preserves the selected provider, model, and API
key for a one-step return with `specgate model on`.

## Configure embeddings for workspace-scoped Knowledge

Workspace-scoped Knowledge search is available as an experimental v0.1 foundation. It needs:

- a knowledge driver such as `pgvector`;
- embedding provider settings;
- consistent embedding dimensions across indexed content.

Knowledge upload, ingest, search, citations, and Context Pack
provenance are usable for local evaluation, but retrieval-backed authoring and
Knowledge-aware readiness gates are still experimental.

See [Configuration reference](../reference/configuration.md) for environment and
settings keys.

## Troubleshooting

### Assistive actions fail

Check:

```bash
specgate doctor
specgate model test
specgate model set
```

Confirm the provider key is current, the model id exists, and the account has
quota.

### Readiness gates are unavailable

Confirm the agents service is running:

```bash
specgate local-status
```

Then check the Full appliance log from its deployment directory:

```bash
docker compose logs specgate
```

### Model output is low confidence

Low confidence usually means the artifact lacks required roles, acceptance
criteria are vague, or the model did not receive enough evidence. Improve the
artifact or completion report before raising thresholds.

## Related

- [Configuration reference](../reference/configuration.md)
- [Governance and gates](../concepts/governance-and-gates.md)
- [Operate SpecGate](operate-specgate.md)
