import { useEffect, useState } from "react"
import { fetchReeve, type ReeveOverview } from "../lib/reeve"

// useReeveOverview reads the Reeve's overview, again whenever key changes
// (the agents list), without needing a query client: the agents page
// renders it on its own.
export function useReeveOverview(baseUrl: string, key = "") {
  const [state, setState] = useState<{ data?: ReeveOverview; isError: boolean }>({ isError: false })
  const [nonce, setNonce] = useState(0)
  useEffect(() => {
    let live = true
    fetchReeve(baseUrl)
      .then((data) => { if (live) setState({ data, isError: false }) })
      .catch(() => { if (live) setState({ isError: true }) })
    return () => { live = false }
  }, [baseUrl, key, nonce])
  return { ...state, refetch: () => setNonce((n) => n + 1) }
}
