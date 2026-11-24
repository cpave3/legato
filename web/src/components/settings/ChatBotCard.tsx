import { useState, type ReactNode } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import type { ConfigSetting } from "../../lib/configSettings"
import { canReconnect, chatBotStateLabel, fetchChatBots, reconnectChatBot, type ChatBotStatus } from "../../lib/chatBots"
import { cn } from "../../lib/utils"
import { RegistrySettings } from "./RegistrySettings"
import { quietButton } from "./SettingsUI"

// How often the card reads the bots' status while it is shown.
const pollEvery = 5000

const tones: Record<string, string> = {
  connected: "bg-lw-live-soft text-lw-live",
  elsewhere: "bg-lw-live-soft text-lw-live",
  connecting: "bg-lw-raised text-lw-text-2",
  backing_off: "bg-lw-needs-soft text-lw-needs",
  auth_failed: "bg-lw-error-soft text-lw-error",
  disabled: "bg-lw-raised text-lw-text-2",
}

const chatBotsKey = (baseUrl: string) => [baseUrl, "chat-bots"]

// ChatBotCard is one chat bot's card in Settings › Integrations: what it is
// for, how to set up its app, its connection's status with Reconnect, and
// its registry settings (the tokens are secrets' names, never tokens).
export function ChatBotCard({ name, title, settings, baseUrl, intro, setup }: {
  name: string
  title: string
  settings: ConfigSetting[]
  baseUrl: string
  intro: ReactNode
  setup: ReactNode
}) {
  return (
    <section id={`chat-bot-${name}`} aria-label={`${title} bot`} className="scroll-mt-20 lg:scroll-mt-6">
      <div className="mb-3 space-y-2 text-[13px] leading-relaxed text-lw-text-2">
        <p>{intro}</p>
        <details>
          <summary className="w-fit cursor-pointer text-lw-link hover:underline">How to set up the app</summary>
          <div className="mt-2 space-y-2 text-xs">{setup}</div>
        </details>
        <p className="text-xs">Changes here apply when Legato restarts.</p>
      </div>
      <ChatBotStatusLine name={name} title={title} baseUrl={baseUrl} />
      <RegistrySettings settings={settings} baseUrl={baseUrl} prefixes={[`integrations.${name}`]} />
    </section>
  )
}

function ChatBotStatusLine({ name, title, baseUrl }: { name: string; title: string; baseUrl: string }) {
  const queryClient = useQueryClient()
  const bots = useQuery({ queryKey: chatBotsKey(baseUrl), queryFn: () => fetchChatBots(baseUrl), refetchInterval: pollEvery })
  const [busy, setBusy] = useState(false)
  const [refusal, setRefusal] = useState("")
  const bot: ChatBotStatus | undefined = bots.data?.find((b) => b.name === name)
  const reconnect = async () => {
    setBusy(true)
    setRefusal("")
    try {
      queryClient.setQueryData(chatBotsKey(baseUrl), await reconnectChatBot(baseUrl, name))
    } catch (reason) {
      setRefusal(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setBusy(false)
    }
  }
  if (bots.isPending) return <p role="status" className="mb-3 text-xs text-lw-text-2">Reading the {title} bot's status…</p>
  if (bots.error && !bot) return <p role="alert" className="mb-3 text-xs text-lw-error">Could not read the {title} bot's status: {bots.error.message}</p>
  if (!bot) return null
  return (
    <div role="group" aria-label={`${title} connection`} className="mb-3 flex flex-col gap-1.5">
      <span className="flex flex-wrap items-center gap-2">
        <span className={cn("inline-flex items-center rounded px-1.5 py-px text-[11px] leading-[18px]", tones[bot.state] ?? tones.disabled)}>{chatBotStateLabel(bot.state)}</span>
        {bot.state === "backing_off" && bot.next_attempt && <span className="text-xs text-lw-text-2">Trying again at {new Date(bot.next_attempt).toLocaleTimeString()}</span>}
        {!bot.configured && bot.enabled && <span className="text-xs text-lw-text-2">Name its tokens below.</span>}
        {canReconnect(bot.state) && <button type="button" className={quietButton + " ml-auto"} aria-label={`Reconnect ${title}`} disabled={busy} onClick={() => void reconnect()}>Reconnect</button>}
      </span>
      {bot.last_error && <p className="text-xs text-lw-error">{bot.last_error}</p>}
      {refusal && <p role="alert" className="text-xs text-lw-error">{refusal}</p>}
    </div>
  )
}
