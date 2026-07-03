# Configure models

Use this guide when you want SpecGate to run server-side assistive or
LLM-backed governance features.

You can use SpecGate without a model. The deterministic core still stores
artifacts, resolves policy, produces Context Packs, and records delivery
evidence.

## What a model enables

A configured server-side model can power:

- route suggestions;
- acceptance-criteria drafting;
- artifact readiness quality gates;
- delivery-review judgment;
- summaries and governance chat assistance.

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
specgate model set --provider openai --model gpt-5.4-mini --api-key <key>
```

Provider names:

| Provider | Value |
|---|---|
| OpenAI | `openai` |
| Anthropic | `anthropic` |
| Google | `google` |
| OpenRouter | `openrouter` |

For OpenRouter, guided setup opens a searchable model picker filtered to
text-output models. Choose manual entry when you need an exact model id.

## Set reasoning effort

Some providers and models support reasoning effort:

```bash
specgate model set --provider openai --thinking-level medium
```

Allowed values are `low`, `medium`, and `high`. Unsupported providers ignore or
reject provider-specific options according to server validation.

## Check model-backed features

Run:

```bash
specgate doctor
specgate gates check <artifact-id>
specgate delivery submit <work-ref> --file completion.json
```

If no model is configured, deterministic flows still work. LLM-backed gates and
review features report that model-backed work is unavailable or needs
configuration.

## Configure embeddings for knowledge search

Knowledge search is experimental. It needs:

- a knowledge driver such as `pgvector`;
- embedding provider settings;
- consistent embedding dimensions across indexed content.

See [Configuration reference](../reference/configuration.md) for environment and
settings keys.

## Troubleshooting

### Assistive actions fail

Check:

```bash
specgate doctor
specgate model set
```

Confirm the provider key is current, the model id exists, and the account has
quota.

### Readiness gates are unavailable

Confirm the agents service is running:

```bash
specgate local-status
```

Then check agents logs from the deployment directory:

```bash
docker compose logs agents
```

### Model output is low confidence

Low confidence usually means the artifact lacks required roles, acceptance
criteria are vague, or the model did not receive enough evidence. Improve the
artifact or completion report before raising thresholds.

## Related

- [Configuration reference](../reference/configuration.md)
- [Governance and gates](../concepts/governance-and-gates.md)
- [Operate SpecGate](operate-specgate.md)
