import { useState } from "react"
import { Play } from "lucide-react"
import { Button } from "./ui/Button"

// Resumes the interrupted agent of a suspended step: its conversation goes
// on with the same attempt, so the step completes as if nothing stopped.
export function WorkflowStepResumeAction({ onResume }: { onResume: () => Promise<void> }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  async function resume() {
    setBusy(true)
    setError("")
    try {
      await onResume()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setBusy(false)
    }
  }
  return <>
    <Button variant="primary" onClick={() => void resume()} disabled={busy}><Play size={15} />{busy ? "Resuming…" : "Resume step"}</Button>
    {error && <p role="alert" className="basis-full text-sm text-lw-error">{error}</p>}
  </>
}
