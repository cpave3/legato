import { useEffect, useLayoutEffect, useRef, useState, type Dispatch, type SetStateAction } from "react"
import { decideApproval, type Approval, type ApprovalDecision } from "../../lib/approvals"
import { onShellApproval, type ShellApprovalResult } from "../../lib/mobileShell"

interface Settle {
  setBusy: Dispatch<SetStateAction<boolean>>
  setError: Dispatch<SetStateAction<string | null>>
  onDone: () => Promise<void>
}

// A signed approval is decided in the Legato app's own sheet; it says how
// that went when the sheet closes.
function settleShellResult(result: ShellApprovalResult, approvalId: string, { setBusy, setError, onDone }: Settle) {
  if (result.id !== approvalId) return
  setBusy(false)
  if (result.status === "decided") void onDone()
  if (result.status === "error") setError(result.error || "The phone could not sign this decision.")
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

// useApprovalDecision is one approval card's request state: busy while a
// decision is on its way (here or in the app's sheet), the error of the last
// one, and run, which sends one and refreshes after.
export function useApprovalDecision(baseUrl: string, approval: Approval, onDone: () => Promise<void>) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => onShellApproval((result) => settleShellResult(result, approval.id, { setBusy, setError, onDone })), [approval.id, onDone])

  const run = async (action: () => Promise<unknown>) => {
    setBusy(true)
    setError(null)
    try {
      await action()
      await onDone()
    } catch (err) {
      setError(errorText(err))
    } finally {
      setBusy(false)
    }
  }
  const decide = (decision: ApprovalDecision) => run(() => decideApproval(baseUrl, approval.id, decision))
  return { busy, setBusy, error, setError, run, decide }
}

export type ApprovalDecisionState = ReturnType<typeof useApprovalDecision>

// useDenyNote is the note a denial carries, and the focus that moves into
// it when the person starts to deny and back to Deny… when they go back.
export function useDenyNote() {
  const [denying, setDenying] = useState(false)
  const [note, setNote] = useState("")
  const noteRef = useRef<HTMLTextAreaElement>(null)
  const denyRef = useRef<HTMLButtonElement>(null)
  const wasDenying = useRef(false)
  useLayoutEffect(() => {
    if (denying) noteRef.current?.focus()
    else if (wasDenying.current) denyRef.current?.focus()
    wasDenying.current = denying
  }, [denying])
  return { denying, setDenying, note, setNote, noteRef, denyRef }
}

// useScrollIntoViewWhen keeps a card in view while it is the focused one.
export function useScrollIntoViewWhen(focused: boolean) {
  const ref = useRef<HTMLElement>(null)
  useEffect(() => {
    if (focused) ref.current?.scrollIntoView?.({ block: "nearest" })
  }, [focused])
  return ref
}
