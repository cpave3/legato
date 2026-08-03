import type { WorkflowDefinition } from "./workflow"

export type CardWorkflowOption = { definition: WorkflowDefinition; taskInput: string }

export function cardWorkflowOptions(definitions: WorkflowDefinition[]): CardWorkflowOption[] {
  return definitions.flatMap(definition => {
    const taskInputs = Object.entries(definition.inputs ?? {}).filter(([, input]) => input.type === "task")
    return taskInputs.length === 1 ? [{ definition, taskInput: taskInputs[0][0] }] : []
  })
}
