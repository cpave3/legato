// A run's steps as a tree (LEG-490): the run's own steps in order, and
// under a loop step its passes, each with its steps. The server lists a
// run's step instances flat, keyed by path (each/2/build).

export type RunStepRow = { step_id: string; step_order: number; path?: string; parent_path?: string }
export type RunStepNode<T> = { step: T; passes: { pass: number; steps: RunStepNode<T>[] }[] }

/** stepPathOf is a step instance's path: its ID for one of the run's own. */
export const stepPathOf = (step: { step_id: string; path?: string }): string => step.path || step.step_id

/** isTopStep is true for one of the run's own steps, not a pass's. */
export const isTopStep = (step: { parent_path?: string }): boolean => !step.parent_path

export function runStepTree<T extends RunStepRow>(steps: T[]): RunStepNode<T>[] {
  const scopes = new Map<string, T[]>()
  for (const step of steps) {
    const parent = step.parent_path ?? ""
    scopes.set(parent, [...(scopes.get(parent) ?? []), step])
  }
  const build = (scope: string): RunStepNode<T>[] =>
    [...(scopes.get(scope) ?? [])].sort((a, b) => a.step_order - b.step_order).map(step => {
      const path = stepPathOf(step)
      const passes: RunStepNode<T>["passes"] = []
      for (const key of scopes.keys()) {
        const rest = key.startsWith(path + "/") ? key.slice(path.length + 1) : ""
        if (/^\d+$/.test(rest)) passes.push({ pass: Number(rest), steps: build(key) })
      }
      passes.sort((a, b) => a.pass - b.pass)
      return { step, passes }
    })
  return build("")
}
