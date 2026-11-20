import { useEffect } from "react"
import { useSearchParams } from "react-router-dom"
import { showApproval } from "../lib/railState"

// useApprovalLink opens the rail (or the phone's tray) on the approval that
// ?approval=<id> names, then drops it from the address. The layout calls it
// once, so the rail and the tray never both take it.
export function useApprovalLink() {
  const [params, setParams] = useSearchParams()
  const wanted = params.get("approval")
  useEffect(() => {
    if (!wanted) return
    showApproval(wanted)
    const next = new URLSearchParams(params)
    next.delete("approval")
    setParams(next, { replace: true })
  }, [wanted, params, setParams])
}

