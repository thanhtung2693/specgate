export const GOVERNANCE_AGENT_PROMPT_EVENT = "specgate:governance-agent-prompt"

export type GovernanceAgentPromptEventDetail = {
  prompt: string
}

const queueKey = "__specgateGovernanceAgentPromptQueue"

function pendingPromptQueue() {
  const globalWindow = window as Window & { [queueKey]?: string[] }
  globalWindow[queueKey] ??= []
  return globalWindow[queueKey]
}

export function requestGovernanceAgentPrompt(prompt: string) {
  pendingPromptQueue().push(prompt)
  window.dispatchEvent(
    new CustomEvent<GovernanceAgentPromptEventDetail>(GOVERNANCE_AGENT_PROMPT_EVENT, {
      detail: { prompt },
    }),
  )
}

export function takePendingGovernanceAgentPrompts() {
  return pendingPromptQueue().splice(0)
}
