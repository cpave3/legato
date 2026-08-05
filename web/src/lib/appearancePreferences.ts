export const ANIMATED_AGENT_ACTIVITY_KEY = "legato:animated-agent-activity"

export function animatedAgentActivityEnabled(): boolean {
  return localStorage.getItem(ANIMATED_AGENT_ACTIVITY_KEY) !== "false"
}

export function setAnimatedAgentActivityEnabled(enabled: boolean): void {
  localStorage.setItem(ANIMATED_AGENT_ACTIVITY_KEY, enabled ? "true" : "false")
}
