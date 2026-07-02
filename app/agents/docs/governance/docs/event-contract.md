# Governance Event Contract (UI-facing)

This document is the wire-level companion to [`spec.md`](spec.md): the spec says
what the governance graph does, this file says how the governance-chat UI observes it.

The `governance` graph is a single governance-chat node — no sub-agents, no
async-task channel, and no HITL interrupts (see [`spec.md`](spec.md) §4). The UI
surface is the assistant-ui governance-ops
(`app/ui/src/components/agent/governance-runtime.tsx`), which drives the
`@assistant-ui/react-langgraph` adapter (`unstable_createLangGraphStream` +
`useLangGraphRuntime`).

## Live Stream Modes

The runtime folds each LangGraph frame into assistant-ui runtime state. The
current surface requests `['messages', 'updates']`.

| Stream mode | Carries | UI owner |
| --- | --- | --- |
| `messages` / `messages-tuple` | governance-chat assistant chunks (reasoning + text) | assistant-ui transcript; reasoning renders via the reasoning message part |
| `updates` | partial state writes | snapshot / projection helpers |
| `values` | checkpoint state snapshots (when requested) | durable hydration (thread title, transcript) |

There is a single namespace — the root node — so there is no sub-agent namespace
fan-out and no async-task channel.

## Narration

The assistant narrates inline in its own reply. There is no separate companion
narration channel and no sub-agent cards.

## Tool Activity

Governance-op tool calls (`get_artifact`, `get_artifact_documents`,
`list_artifact_readiness`, `run_artifact_readiness`) stream through the standard
`messages` tool-call lifecycle and render via the tool-disclosure surface
(`ui/src/components/governance-chat/tool-disclosure.tsx`). These are read/run
governance-ops — there are no gated or irreversible tool interrupts.

## State Hydration

Durable per-thread state (LangGraph `values`) plus the `messages` channel is the
persistable audit trail. The chat surface (thread title) is served by
`webapp.py` over the LangGraph SDK loopback; the UI rehydrates the transcript
via the LangGraph SDK (`getState`). See `title_api.py`.

## References

- Streaming/debugging checklist: [`../../../../../docs/testing.md`](../../../../../docs/testing.md)
- Governance-chat runtime: [`../../../../ui/src/components/agent/governance-runtime.tsx`](../../../../ui/src/components/agent/governance-runtime.tsx)
- Governance node contract: [`spec.md`](spec.md)
