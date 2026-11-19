import { useState } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { fetchDoctor, recheckDoctor, requiredFailed } from "../lib/doctor"
import { queryKeys } from "../lib/query"

// DoctorBanner is the strip at the top of every page while a required check
// of `legato doctor` fails on the server's machine: what is missing, each
// fix, and Check again. Layout renders it only while the settings say
// setup_incomplete, so the report is read only then.
export function DoctorBanner({ baseUrl }: { baseUrl: string }) {
  const queryClient = useQueryClient()
  const { data } = useQuery({ queryKey: queryKeys.doctor(baseUrl), queryFn: () => fetchDoctor(baseUrl), staleTime: 60_000 })
  const [checking, setChecking] = useState(false)
  const [error, setError] = useState("")
  const failed = requiredFailed(data)
  if (failed.length === 0) return null

  const checkAgain = async () => {
    setChecking(true)
    setError("")
    try {
      queryClient.setQueryData(queryKeys.doctor(baseUrl), await recheckDoctor(baseUrl))
      await queryClient.invalidateQueries({ queryKey: queryKeys.meta(baseUrl) })
      await queryClient.invalidateQueries({ queryKey: queryKeys.settings(baseUrl) })
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setChecking(false)
    }
  }

  return (
    <div role="alert" className="flex flex-wrap items-start gap-x-4 gap-y-2 border-b border-lw-error/40 bg-lw-surface px-4 py-3 text-sm">
      <div className="min-w-0 flex-1">
        <p className="font-medium text-lw-error">Part of Legato cannot work on this machine</p>
        <ul className="mt-1 space-y-1 text-lw-text-2">
          {failed.map((check) => (
            <li key={check.id}>
              <span className="text-lw-text">{check.title}</span>
              {check.detail && <>: {check.detail}</>}
              {check.fix && <span className="block text-xs">{check.fix}</span>}
            </li>
          ))}
        </ul>
        {error && <p className="mt-1 text-xs text-lw-error">{error}</p>}
      </div>
      <button type="button" disabled={checking} onClick={() => void checkAgain()}
        className="shrink-0 rounded border border-lw-line px-3 py-1.5 text-sm font-medium hover:bg-lw-raised disabled:opacity-50">
        {checking ? "Checking…" : "Check again"}
      </button>
    </div>
  )
}
