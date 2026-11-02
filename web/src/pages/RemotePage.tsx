import { Link } from "react-router-dom"
import { useEffect, useRef, useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "../components/ui/Button"
import { InlineNotice } from "../components/ui/InlineNotice"
import { LoadingState } from "../components/ui/LoadingState"
import { Dialog, DialogPanel, DialogHeader, DialogTitle, DialogBody, DialogFooter } from "../components/ui/Dialog"
import { useServer } from "../hooks/useServer"
import { useJobClones } from "../hooks/useJobClones"
import { removeJobClone, type JobClone } from "../lib/jobClones"
import { queryKeys } from "../lib/query"

function date(iso: string) {
  return new Date(iso).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" })
}

function tips(count: number) {
  return `${count} local ${count === 1 ? "tip" : "tips"} absent from cache`
}

export function RemotePage() {
  const { baseUrl, activeServerName } = useServer()
  return <RemoteInventory key={baseUrl} baseUrl={baseUrl} serverName={activeServerName} />
}

function RemoteInventory({ baseUrl, serverName }: { baseUrl: string; serverName: string }) {
  const clones = useJobClones(baseUrl)
  const client = useQueryClient()
  const [confirming, setConfirming] = useState<JobClone | null>(null)
  const refresh = useRef<HTMLButtonElement>(null)
  const submitting = useRef(false)
  const removal = useMutation({
    mutationFn: (id: number) => removeJobClone(baseUrl, id),
    onSuccess: async (_, id) => {
      const queryKey = queryKeys.jobClones(baseUrl)
      await client.cancelQueries({ queryKey })
      client.setQueryData<JobClone[]>(queryKey, (current) => current?.filter((clone) => clone.id !== id), {
        updatedAt: client.getQueryState(queryKey)?.dataUpdatedAt,
      })
      await client.invalidateQueries({ queryKey })
      setConfirming(null)
    },
    onSettled: () => { submitting.current = false },
  })
  useEffect(() => {
    if (removal.isSuccess) refresh.current?.focus()
  }, [removal.isSuccess])
  const currentClone = clones.data?.find((clone) => clone.id === confirming?.id)
  const selectedClone = currentClone ?? confirming
  const unavailable = confirming !== null && !currentClone
  const busy = selectedClone?.busy

  return <main className="mx-auto w-full max-w-5xl space-y-5 p-4 text-lw-text md:p-8">
    <div className="flex items-center justify-between gap-4">
      <h1 className="text-2xl font-semibold">Remote</h1>
      <Button asChild variant="outline"><button ref={refresh} type="button" disabled={clones.isFetching} onClick={() => void clones.refetch()}>{clones.isFetching ? "Refreshing…" : "Refresh"}</button></Button>
    </div>
    <h2 className="text-lg">Managed clones on {serverName}</h2>
    <InlineNotice variant="warning">
      Working-tree changes are not checked. Automatic cleanup can delete uncommitted or unpushed work.
    </InlineNotice>
    <details className="text-sm text-lw-text-2">
      <summary className="min-h-11 cursor-pointer content-center text-lw-link">About status and automatic cleanup</summary>
      <div className="space-y-2 pb-2">
        <p>Local tips count distinct branch-tip commits absent from Legato’s local cache. This is not a count of all unpushed commits,
          a current GitHub check, or proof of a merge.</p>
        <p>Cleanup follows <code>repository_cache.keep_idle</code> in the server’s config.yaml
          (default: seven idle days; zero disables idle cleanup). Clones retained for absent tips expire seven days later.
          A missing deadline does not mean indefinite retention.</p>
        <p>Busy status covers Legato executions, not every host process.</p>
      </div>
    </details>
    {clones.dataUpdatedAt > 0 && <p className="text-sm text-lw-text-2">Inventory updated at {date(new Date(clones.dataUpdatedAt).toISOString())}</p>}
    {clones.isPending && <LoadingState label="Loading clones…" size="inline" />}
    {clones.isError && <InlineNotice variant="error">
      Could not load clones: {clones.error.message}. {clones.data && "The inventory below is from the last successful request."}
      <Button variant="outline" disabled={clones.isFetching} onClick={() => void clones.refetch()}>Retry</Button>
    </InlineNotice>}
    {clones.isSuccess && clones.data.length === 0 && <p>No managed clones on this server.</p>}
    <ul className="space-y-4">
      {clones.data?.map((clone) => <li key={clone.id} aria-label={clone.repo} className="space-y-2 rounded-xl border border-lw-line bg-lw-surface p-4">
        <h3 className="font-semibold">{clone.repo}</h3>
        <p className="break-all font-lw-mono text-sm">{clone.branch || (clone.problem ? "Branch unavailable" : "Detached HEAD")}</p>
        <p className="break-all font-lw-mono text-sm">{clone.path}</p>
        <p className="flex flex-wrap gap-2">Tasks: {clone.task_ids.map((id) => <Link key={id} to={`/board?highlight_task=${encodeURIComponent(id)}`} className="text-lw-link underline">{id}</Link>)}</p>
        <p>{clone.busy ? "Agent working" : "No tracked agent working"}</p>
        <p>Last used: {date(clone.last_used_at)}</p>
        {clone.remove_after && <p>Scheduled removal: {date(clone.remove_after)}</p>}
        <p>Working-tree changes: not checked</p>
        <p>{clone.problem ? "Cache comparison unavailable" : tips(clone.unpushed)}</p>
        {clone.problem && <p className="break-words text-amber-300">{clone.problem}</p>}
        <Button variant="outline" aria-label={`Remove ${clone.repo}`} disabled={clone.busy || removal.isPending}
          onClick={() => { removal.reset(); setConfirming(clone) }}>Remove clone</Button>
      </li>)}
    </ul>
    <Dialog open={confirming !== null} dismissible={!removal.isPending} onClose={() => setConfirming(null)}>
      <DialogPanel>
        <DialogHeader><DialogTitle>Remove this clone?</DialogTitle></DialogHeader>
        {selectedClone && <DialogBody className="max-h-[65dvh] overflow-y-auto break-words">
          <p>Server: {serverName}</p>
          <p className="font-semibold">{selectedClone.repo}</p>
          <p className="break-all">{selectedClone.branch || (selectedClone.problem ? "Branch unavailable" : "Detached HEAD")}</p>
          <p className="break-all font-lw-mono text-sm">{selectedClone.path}</p>
          <InlineNotice variant="warning">This deletes the entire folder, including changed, untracked and ignored files.
            Working-tree changes are not checked. Other processes can change files after this inventory check.</InlineNotice>
          <p>{selectedClone.problem ? `Cache comparison unavailable: ${selectedClone.problem}` : `${tips(selectedClone.unpushed)}. These tips may be lost.`}</p>
          {busy && <p role="alert">An agent is working in this clone. It cannot be removed.</p>}
          {unavailable && !removal.isPending && <p role="alert">This clone is no longer in the inventory. Keep the clone and Refresh to check its current state.</p>}
          {removal.isError && <InlineNotice variant="error">Could not remove clone: {removal.error.message}. Keep the clone and Refresh to check its current state.</InlineNotice>}
        </DialogBody>}
        <DialogFooter className="flex-wrap">
          <Button variant="outline" disabled={removal.isPending} onClick={() => setConfirming(null)}>Keep clone</Button>
          <Button variant="destructive" disabled={Boolean(busy) || unavailable || removal.isPending} onClick={() => {
            if (!confirming || unavailable || busy || submitting.current) return
            submitting.current = true
            removal.mutate(confirming.id)
          }}>{removal.isPending ? "Removing…" : "Remove entire folder"}</Button>
        </DialogFooter>
      </DialogPanel>
    </Dialog>
  </main>
}
