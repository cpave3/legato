import { apiFetch } from "./api"
import type { AwayOn, BoardCard } from "./board-types"

// Bringing away tasks home from the Away lane: Return a task done on its
// worker, Recall one in any state (with whatever exists so far), and Return
// all done on one worker. See docs/ai/web-ui.md § Returning and recalling.

export interface AwayActions {
  // Return: the task is done on the worker, or a return is under way.
  return: boolean
  // Recall: any other state the worker can be asked in.
  recall: boolean
  // Recalling stops an agent that may still be at work: ask first.
  confirmRecall: boolean
}

const none: AwayActions = { return: false, recall: false, confirmRecall: false }

// What an away row and its task panel offer: only what the task's state
// allows. A worker that cannot be reached, no longer accepts this main or
// lost the task offers neither (Take back is for those).
export function awayActions(away: AwayOn): AwayActions {
  switch (away.view) {
    case "unreachable":
    case "not_accepted":
    case "lost":
      return none
    case "done":
    case "coming_back":
      return { return: true, recall: false, confirmRecall: false }
    case "failed":
    case "ended":
      return { return: false, recall: true, confirmRecall: false }
    case "starting":
    case "working":
    case "needs_you":
    case "idle":
      return { return: false, recall: true, confirmRecall: true }
  }
  switch (away.state) {
    case "done":
    case "returning":
      return { return: true, recall: false, confirmRecall: false }
    case "failed":
      return { return: false, recall: true, confirmRecall: false }
    default:
      return { return: false, recall: true, confirmRecall: true }
  }
}

// The cards of a worker group that Return all done brings back.
export function doneOnWorker(cards: BoardCard[]): BoardCard[] {
  return cards.filter((card) => card.away && (card.away.view ?? card.away.state) === "done")
}

// ReturnedTask is a task brought home: the column it landed in.
export interface ReturnedTask {
  task_id: string
  column: string
  recalled: boolean
  branches: string[]
}

async function post(baseUrl: string, path: string): Promise<Record<string, unknown>> {
  const res = await apiFetch(baseUrl, path, { method: "POST" })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) {
    if (res.status === 404 && !body.error) throw new Error("This server's Legato cannot bring tasks home. Update it.")
    throw new Error(body.error ?? `The server answered ${res.status}.`)
  }
  return body
}

// returnTask returns a task done on its worker. A refusal throws the
// server's words.
export async function returnTask(baseUrl: string, taskId: string): Promise<ReturnedTask> {
  return (await post(baseUrl, `/api/tasks/${encodeURIComponent(taskId)}/return`)).returned as ReturnedTask
}

// recallTask brings a task home in any state.
export async function recallTask(baseUrl: string, taskId: string): Promise<ReturnedTask> {
  return (await post(baseUrl, `/api/tasks/${encodeURIComponent(taskId)}/recall`)).returned as ReturnedTask
}

// returnAllDone returns every task done on a worker; error says why some
// did not come back.
export async function returnAllDone(baseUrl: string, workerId: string): Promise<{ returned: ReturnedTask[]; error?: string }> {
  const body = await post(baseUrl, `/api/workers/${encodeURIComponent(workerId)}/return-done`)
  return { returned: (body.returned as ReturnedTask[]) ?? [], error: body.error as string | undefined }
}
