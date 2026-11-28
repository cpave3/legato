import type { Approval } from "../../lib/approvals"
import { approvalQuestions } from "../../lib/questionDrafts"
import { ApprovalCard } from "../approvals/ApprovalTray"
import { QuestionRow } from "./QuestionRow"

// RailApproval is one pending approval in the rail or the phone's sheet: an
// agent's question as a compact row, anything else as its card.
export function RailApproval({ approval, wanted, inView, onDone }: { approval: Approval; wanted: string | null; inView: string | null; onDone: () => Promise<void> }) {
  if (approvalQuestions(approval).length > 0) {
    return <QuestionRow approval={approval} focused={approval.id === wanted} here={inView !== null && approval.task_id === inView} onDone={onDone} />
  }
  return <ApprovalCard approval={approval} focused={approval.id === wanted} onDone={onDone} />
}
