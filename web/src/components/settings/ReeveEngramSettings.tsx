import { useEffect, useState } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { Brain } from "lucide-react"
import { fetchReeveEngram, removeReeveMemory, removeReeveThread, saveReeveSeed } from "../../lib/reeve"
import { queryKeys } from "../../lib/query"
import { SettingsCard, primaryButton, quietButton } from "./SettingsUI"
import { useReeveName } from "../../lib/reeveName"

// ReeveEngramSettings is the Reeve's own engram: its seed, which changes
// only with a signature, and its memories and open threads, which the
// person can remove. Editing a memory stays with the engram CLI.
export function ReeveEngramSettings({ baseUrl }: { baseUrl: string }) {
  const queryClient = useQueryClient()
  const engram = useQuery({ queryKey: queryKeys.reeveEngram(baseUrl), queryFn: () => fetchReeveEngram(baseUrl), retry: false })
  const reeve = useReeveName()
  const [seed, setSeed] = useState("")
  const [busy, setBusy] = useState("")
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const loaded = engram.data?.seed.text
  useEffect(() => { if (loaded !== undefined) setSeed(loaded) }, [loaded])

  const run = async (what: string, action: () => Promise<unknown>, done?: string) => {
    setBusy(what)
    setError("")
    setNotice("")
    try {
      await action()
      if (done) setNotice(done)
      await queryClient.invalidateQueries({ queryKey: queryKeys.reeveEngram(baseUrl) })
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setBusy("")
    }
  }
  const signed = `Approve the change on your phone, or with a passkey, and ${reeve.mid}'s next session starts with this seed.`
  const proposeSeed = (text: string) => run("seed", () => saveReeveSeed(baseUrl, text), signed)

  if (engram.isError) {
    return <SettingsCard id="reeve-engram" icon={Brain} title={`${reeve.start}'s engram`} description={`This server does not keep ${reeve.mid}'s engram: it is older, or runs without tmux.`} />
  }
  const data = engram.data
  if (!data) return null
  if (!data.enabled) {
    return (
      <SettingsCard id="reeve-engram" icon={Brain} title={`${reeve.start}'s engram`}
        description={<>{reeve.start} starts each session without its engram, and remembers nothing of earlier ones: {data.error}</>} />
    )
  }
  return (
    <SettingsCard id="reeve-engram" icon={Brain} title={`${reeve.start}'s engram`}
      description={<>
        <span className="font-lw-mono text-lw-text">{data.name}</span>, kept in <code>{data.store}</code>, apart from your own engrams.
        {!data.exists && ` Legato issues it with this seed when ${reeve.mid}'s first session starts.`}
      </>}>
      <label className="flex flex-col gap-1 text-xs text-lw-text-2">
        <span className="flex flex-wrap items-center gap-2">
          The seed
          <span className="rounded bg-lw-raised px-1.5 text-[11px] text-lw-text-2">{data.seed.edited ? "edited" : "built in"}</span>
          <span className="text-lw-faint">One of {reeve.mid}'s own rules: a change needs a signature.</span>
        </span>
        <textarea aria-label={`${reeve.start}'s seed`} value={seed} onChange={(e) => setSeed(e.target.value)} rows={8}
          className="w-full rounded-lg border border-lw-line bg-lw-ground px-2 py-1.5 font-lw-mono text-xs text-lw-text" />
      </label>
      <div className="mt-2 flex flex-wrap items-center gap-2">
        <button type="button" className={primaryButton} disabled={busy !== "" || seed.trim() === "" || seed.trim() === data.seed.text.trim()}
          onClick={() => void proposeSeed(seed)}>Save the seed</button>
      </div>
      {data.seed.newer && (
        <div className="mt-3 rounded-lg border border-lw-line p-3 text-xs text-lw-text-2" data-testid="reeve-seed-newer">
          <p>This version of Legato has a newer built-in seed. Legato never puts it in by itself.</p>
          <details className="mt-1">
            <summary className="cursor-pointer">Show it</summary>
            <pre className="mt-1 max-h-48 overflow-auto whitespace-pre-wrap rounded bg-lw-ground p-2 font-lw-mono">{data.seed.template}</pre>
          </details>
          <button type="button" className={`${quietButton} mt-2`} disabled={busy !== ""} onClick={() => void proposeSeed(data.seed.template)}>
            Use the new built-in seed
          </button>
        </div>
      )}
      {notice && <p role="status" className="mt-2 text-xs text-lw-text-2">{notice}</p>}
      {error && <p role="alert" className="mt-2 text-xs text-lw-error">{error}</p>}

      <section className="mt-4 border-t border-lw-line pt-3" aria-label={`${reeve.start}'s memories`}>
        <h3 className="text-sm text-lw-text">Memories</h3>
        {data.memories.length === 0
          ? <p className="mt-1 text-xs text-lw-faint">None yet.</p>
          : (
            <ul className="mt-1 flex flex-col divide-y divide-lw-line">
              {data.memories.map((m) => (
                <li key={m.id} className="flex items-start gap-2 py-2 text-sm" data-testid={`reeve-memory-${m.id}`}>
                  <div className="min-w-0 flex-1">
                    <p className="whitespace-pre-wrap break-words text-lw-text">{m.body}</p>
                    <p className="text-[11px] text-lw-faint">{m.kind} · {m.source} · {new Date(m.created).toLocaleDateString()}</p>
                  </div>
                  <button type="button" className={quietButton} disabled={busy !== ""}
                    onClick={() => void run(`memory:${m.id}`, () => removeReeveMemory(baseUrl, m.id))}>Remove</button>
                </li>
              ))}
            </ul>
          )}
        <p className="mt-1 text-[11px] text-lw-faint">Removing a memory deletes it for good. To edit one, use <code>engram edit</code> with <code>ENGRAM_HOME={data.store}</code>.</p>
      </section>

      <section className="mt-4 border-t border-lw-line pt-3" aria-label={`${reeve.start}'s open threads`}>
        <h3 className="text-sm text-lw-text">Open threads</h3>
        {data.threads.length === 0
          ? <p className="mt-1 text-xs text-lw-faint">None open.</p>
          : (
            <ul className="mt-1 flex flex-col divide-y divide-lw-line">
              {data.threads.map((t) => (
                <li key={t.id} className="flex items-start gap-2 py-2 text-sm" data-testid={`reeve-thread-${t.id}`}>
                  <div className="min-w-0 flex-1">
                    <p className="break-words text-lw-text">{t.title}</p>
                    {t.body && <p className="whitespace-pre-wrap break-words text-xs text-lw-text-2">{t.body}</p>}
                    <p className="text-[11px] text-lw-faint">opened {new Date(t.opened).toLocaleDateString()}</p>
                  </div>
                  <button type="button" className={quietButton} disabled={busy !== ""}
                    onClick={() => void run(`thread:${t.id}`, () => removeReeveThread(baseUrl, t.id))}>Remove</button>
                </li>
              ))}
            </ul>
          )}
      </section>
    </SettingsCard>
  )
}
