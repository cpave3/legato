import type { ConfigSetting } from "../../lib/configSettings"
import { ChatBotCard } from "./ChatBotCard"

const code = "rounded bg-lw-raised px-1 font-lw-mono text-lw-text"

export function DiscordSettings({ settings, baseUrl }: { settings: ConfigSetting[]; baseUrl: string }) {
  return <ChatBotCard name="discord" title="Discord" settings={settings} baseUrl={baseUrl}
    intro="A Discord bot that workflows can start on (a message, or a mention of it) and post as. It connects to Discord's Gateway, so Discord needs no address for Legato."
    setup={<>
      <p>Create an application at discord.com/developers/applications and add a bot to it.</p>
      <p>Under Bot › Privileged Gateway Intents, turn on the Message Content intent; without it a server message's text arrives only when it mentions the bot.</p>
      <p>Invite the bot to your server with the Send Messages and Read Message History permissions.</p>
      <p>Store its token with the secret store (<code className={code}>legato secret add discord-token</code>) and name it here.</p>
    </>} />
}
