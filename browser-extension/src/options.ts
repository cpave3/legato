import { DEFAULT_CONFIG, type ExtensionConfig } from "./config"
import { LegatoClient } from "./legato"
import { replaceOriginPermission } from "./permissions"
import { clearToken, loadConfig, saveConfig } from "./storage"

const form = document.querySelector<HTMLFormElement>("#settings-form")!
const status = document.querySelector<HTMLParagraphElement>("#status")!
const fields = {
  baseUrl: document.querySelector<HTMLInputElement>("#base-url")!,
  token: document.querySelector<HTMLInputElement>("#token")!,
  workflowName: document.querySelector<HTMLInputElement>("#workflow-name")!,
  repoInput: document.querySelector<HTMLInputElement>("#repo-input")!,
  pullRequestInput: document.querySelector<HTMLInputElement>("#pr-input")!,
  primaryDirInput: document.querySelector<HTMLInputElement>("#primary-dir-input")!,
}

function show(message: string, kind: "success" | "error"): void {
  status.textContent = message
  status.dataset.kind = kind
}

function readForm(): ExtensionConfig {
  return Object.fromEntries(Object.entries(fields).map(([key, input]) => [key, input.value])) as ExtensionConfig
}

function fill(config: ExtensionConfig): void {
  for (const [key, input] of Object.entries(fields)) input.value = config[key as keyof ExtensionConfig]
}

async function initialize(): Promise<void> {
  try {
    fill(await loadConfig())
  } catch {
    fill(DEFAULT_CONFIG)
  }
}

form.addEventListener("submit", (event) => {
  event.preventDefault()
  void (async () => {
    try {
      let previous: ExtensionConfig | undefined
      try { previous = await loadConfig() } catch { previous = undefined }
      const requested = readForm()
      const granted = await replaceOriginPermission(previous?.baseUrl, requested.baseUrl)
      if (!granted) throw new Error("Permission for the configured Legato origin was not granted")
      const config = await saveConfig(requested)
      const discovery = await new LegatoClient(config.baseUrl, config.token).discover()
      show(`Connected. Found ${discovery.definitions?.length ?? 0} workflow(s).`, "success")
    } catch (error) {
      show(error instanceof Error ? error.message : String(error), "error")
    }
  })()
})

document.querySelector<HTMLButtonElement>("#clear-token")!.addEventListener("click", () => {
  void clearToken().then(() => {
    fields.token.value = ""
    show("Token cleared.", "success")
  }).catch((error: unknown) => show(error instanceof Error ? error.message : String(error), "error"))
})

void initialize()
