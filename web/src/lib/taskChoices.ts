import type { BoardCard } from "./board-types"

// The tasks a workflow's task input offers, and their order: apart from the
// picker that draws them.

export type SelectableTask = BoardCard & { archived?: boolean }

export function isDone(task: BoardCard): boolean {
  return task.status.toLowerCase() === "done"
}

/** offeredTasks are the open tasks, or with completed shown every task
 * on the board and the archived ones too. */
export function offeredTasks(active: SelectableTask[], archived: SelectableTask[], showCompleted: boolean): SelectableTask[] {
  return showCompleted ? [...active, ...archived] : active.filter(task => !isDone(task))
}

const UNASSIGNED = "Unassigned"

// byWorkspace orders workspaces A to Z, with Unassigned last.
function byWorkspace(a: string, b: string): number {
  if (a === UNASSIGNED) return 1
  if (b === UNASSIGNED) return -1
  return a.localeCompare(b)
}

/** taskGroups are the tasks by workspace (Unassigned for none), each task
 * once, workspaces A to Z with Unassigned last and tasks A to Z within. */
export function taskGroups(tasks: SelectableTask[]): (readonly [string, SelectableTask[]])[] {
  const grouped = new Map<string, SelectableTask[]>()
  for (const task of tasks) {
    const workspace = task.workspace_name || UNASSIGNED
    const existing = grouped.get(workspace) ?? []
    if (!existing.some(item => item.id === task.id)) existing.push(task)
    grouped.set(workspace, existing)
  }
  return [...grouped.entries()]
    .sort(([a], [b]) => byWorkspace(a, b))
    .map(([workspace, workspaceTasks]) => [workspace, workspaceTasks.sort((a, b) => a.title.localeCompare(b.title))] as const)
}

/** taskText is a task as the picker names it. */
export const taskText = (task: BoardCard) => `${task.id} · ${task.title}`

/** completedWord says a task is Archived or Done, or nothing. */
export function completedWord(task: SelectableTask): string | null {
  if (task.archived) return "Archived"
  return isDone(task) ? "Done" : null
}
