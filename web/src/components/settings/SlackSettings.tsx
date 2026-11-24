import type { ConfigSetting } from "../../lib/configSettings"
import { ChatBotCard } from "./ChatBotCard"

const code = "rounded bg-lw-raised px-1 font-lw-mono text-lw-text"

export function SlackSettings({ settings, baseUrl }: { settings: ConfigSetting[]; baseUrl: string }) {
  return <ChatBotCard name="slack" title="Slack" settings={settings} baseUrl={baseUrl}
    intro="A Slack bot that workflows can start on (a message, or a mention of it) and post as. It connects over Socket Mode, so Slack needs no address for Legato."
    setup={<>
      <p>Create an app at api.slack.com/apps and turn on Socket Mode. Make an app-level token with the <code className={code}>connections:write</code> scope.</p>
      <p>Under OAuth &amp; Permissions, give the bot the scopes <code className={code}>chat:write</code>, <code className={code}>channels:history</code>, <code className={code}>groups:history</code>, <code className={code}>im:history</code> and <code className={code}>app_mentions:read</code>, then install the app to your workspace.</p>
      <p>Under Event Subscriptions, subscribe the bot to <code className={code}>message.channels</code>, <code className={code}>message.groups</code> and <code className={code}>message.im</code>.</p>
      <p>Store both tokens with the secret store (<code className={code}>legato secret add slack-app-token</code>, <code className={code}>legato secret add slack-bot-token</code>) and name them here. Invite the bot to each channel it should read or post in.</p>
    </>} />
}
