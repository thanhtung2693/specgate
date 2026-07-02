# Configure models

SpecGate runs its deterministic governance with **no model**. A server-side model
adds an assistive layer; you can also push semantic work to the coding agent you
already run, keeping the server-side model small or absent.

## What the server-side model does

When configured, the server-side model powers the server-side assistive work:

- work-route suggestion (quick vs full);
- feature-lifecycle suggestion;
- acceptance-criteria drafting for quick work items;
- delivery review (post-build acceptance-criteria verification);
- source PRD/spec extraction into proposals;
- reconciliation proposal drafting;
- artifact and feature summaries;
- the built-in LLM readiness quality gates.

This is the single server-side model for every non-chat governance workload. The
experimental governance-ops chat graph runs on its own env-selected support
model (`GOVERNANCE_OPS_MODEL_PROVIDER` / `GOVERNANCE_OPS_MODEL` /
`GOVERNANCE_OPS_API_KEY`), configured in the agents runtime environment.

What the server-side model does **not** gate: deterministic policy resolution,
version snapshots, evidence validation, and trust stamping all run without it.

## The model is optional

SpecGate runs the full governed delivery loop with **no server-side model
configured** — you do not need an LLM key to start or to govern real work:

1. **The deterministic core needs no model** — governance levels, policy
   snapshots, evidence manifests, and trust stamping work with nothing
   configured.
2. **Readiness gates can be checked from your coding agent** — with no
   server-side model, use the focused SpecGate lifecycle skills and CLI commands
   to inspect readiness gaps and report implementation evidence. Human review
   remains the authority for approval.
3. **Your coding agent does the delivery work** — pickup, scoped implementation,
   and completion reporting run through the `specgate:delivering-work` skill. See
   [Use SpecGate with a coding agent](coding-agent-workflow.md).

Without a model, the server-side *conveniences* simply stay quiet — automatic
route classification, artifact extraction, and summaries are skipped rather than
failing. Delivery review still produces a verdict: it is derived deterministically
from the coding agent's per-acceptance-criterion claims (a missing or partial
claim resolves to `needs_human_review`), so a human makes the final call where the
agent's self-report is incomplete.

Configure a server-side model when you want SpecGate to pre-fill that assistive work
server-side instead of asking your coding agent each time.

## Configure a model without the UI

Model configuration lives in Doc Registry settings. There is no UI step
required.

### Option A — default provider, key only

The default provider is OpenAI (`gpt-5.4-mini`). Store the provider key with the
CLI:

```bash
specgate model set --provider openai --api-key <key>
```

The server-side model now runs on the default OpenAI model with no further setup.

### Option B — choose a provider and model with the CLI

Use `specgate model set` to select the provider, model, and key — no UI, no raw
API call. The key is stored encrypted:

```bash
specgate model set --provider openai --model gpt-5.4-mini --api-key sk-...
```

Or run it with no flags to be prompted — pick the provider from a list, enter a
model id, and type the key into a masked field:

```bash
specgate model set
```

Providers: `openai`, `google`, `anthropic`, `openrouter`. `--model` is optional
(the provider default applies for providers with a known default);
`--thinking-level low|medium|high` is optional. OpenRouter has a very large
catalog, so the interactive CLI fetches OpenRouter's model list and opens a
searchable picker filtered to text-output models. Choose **Manual entry** in the
picker when you want to paste an exact model id directly.

You can also provide the provider up front and let the CLI prompt for the model
and key:

```bash
specgate model set --provider openrouter
```

Inspect the current configuration (the key is shown only as set/not set):

```bash
specgate model show
```

The CLI writes the same Doc Registry settings the API exposes
(`governance.model_provider`, `governance.model`, `<provider>.api_key`); a raw
`PUT /settings` call works too if you prefer to script it directly.

### Reasoning effort

Pass `--thinking-level low|medium|high` to `specgate model set` (or set
`governance.default_thinking_level` via the settings API). `low` keeps the fast,
stream-friendly default; `medium` and `high` opt into deeper reasoning where the
model supports it.

## LLM readiness quality gates

The built-in LLM readiness gates (acceptance-criteria verifiability, rollback
plan, scope clarity, and similar) resolve their executor from whether a
server-side model is configured:

- **Server-side model configured** → the gates run **server-side** on it, alongside
  the rest of the assistive work.
- **No server-side model** → use the CLI and focused IDE skills to inspect the
  readiness state and fix gaps in the source artifact before human approval.

So the server-side model is optional for the readiness gates too — without one, the
coding agent you already run can still help repair the underlying artifact.

## Enable knowledge embeddings (experimental)

> Knowledge search / vector DB is an opt-in, in-development feature. See
> [Feature status](../features.md).

Embeddings support governed knowledge upload and semantic search. Set an
embedding provider and model through the settings API, and set
`KNOWLEDGE_DRIVER=pgvector` (it defaults to `none`). Leave it off when you do not
need governed knowledge indexing — core artifact governance is unaffected.

## Troubleshooting

### Assistive actions fail

- confirm a provider key is configured with `specgate model set` or settings;
- confirm `governance.model_provider` / `governance.model` if you selected a non-default
  provider;
- verify provider quota and model access;
- run `specgate doctor`.

### Readiness gates are unavailable

The built-in LLM gates need a model. Either configure one (above) or author the
gates with the `ide_agent` executor so your coding agent runs them.

### Knowledge upload or search is unavailable

Configure an embedding provider and model, and confirm `KNOWLEDGE_DRIVER` is not
`none`.

### Model output is low confidence

Low-confidence pass or fail judgments may become `needs_human_review`. That is a
safety behavior, not a transport error. Adjust the rubric, model, reasoning
effort, or evidence rather than forcing a pass.

## Continue

- [Governance and gates](../concepts/governance-and-gates.md)
- [Configuration reference](../reference/configuration.md)
