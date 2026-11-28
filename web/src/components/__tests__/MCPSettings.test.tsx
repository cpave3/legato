import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { MCPSettings } from "../settings/MCPSettings"

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}

type Conn = {
  name: string
  url: string
  kind?: string
  command?: string
  args?: string[]
  dir?: string
  env?: Record<string, string>
  auth: string
  enabled: boolean
  revision: number
  rules: Array<Record<string, unknown>>
  read_only_tools?: string[]
  authorization: string
  attempt?: string
  connection: string
  problem?: { code: string; message: string }
  shared_with?: string
}
type Attempt = { id: string; connection: string; status: string; scope?: string; error_code?: string; expires_at: number; evidence?: Record<string, unknown> }

let conns: Conn[]
let attempts: Record<string, Attempt>
let requests: Array<{ method: string; url: string; body?: Record<string, unknown>; contentType?: string }>
let opened: { location: { href: string }; opener: unknown } | null
let testResult: Record<string, unknown>
let posted: string[]

const notion = (over: Partial<Conn> = {}): Conn => ({
  name: "notion", url: "https://mcp.notion.example/mcp", auth: "oauth", enabled: true, revision: 1, rules: [],
  authorization: "required", connection: "unknown",
  problem: { code: "authorization_required", message: "Connect it to let Legato use it." }, ...over,
})

const evidence = { issuer: "https://auth.notion.example", resource: "https://mcp.notion.example/mcp", granted_scope: "read", refreshable: true, account: "ana@example.test" }

beforeEach(() => {
  conns = [notion()]
  attempts = {}
  requests = []
  opened = null
  posted = []
  testResult = { tools: [] }
  vi.stubGlobal("open", vi.fn(() => {
    opened = { location: { href: "" }, opener: window }
    return opened
  }))
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    const method = init?.method ?? "GET"
    const body = init?.body ? JSON.parse(String(init.body)) : undefined
    const headers = (init?.headers ?? {}) as Record<string, string>
    requests.push({ method, url, body, contentType: headers["Content-Type"] })
    const path = String(url)
    if (method === "GET" && path === "/api/settings/mcp/connections") return json({ connections: conns })
    const tested = path.match(/^\/api\/settings\/mcp\/connections\/([^/]+)\/test$/)
    if (tested && method === "POST") return json(testResult)
    let m = path.match(/^\/api\/settings\/mcp\/connections\/([^/]+)$/)
    if (m) {
      const c = conns.find((x) => x.name === m![1])
      if (!c) return json({ error: "no MCP connection of that name" }, 404)
      if (method === "GET") return json(c)
      if (method === "PATCH") {
        if (body?.expected_revision !== c.revision) return json({ error: "the connection changed; reload it" }, 409)
        if (c.kind === "stdio" && ["command", "args", "dir", "env"].some((k) => body && k in body)) {
          return json({ status: "needs_approval", approval_id: "apr-2", signed: false, message: "waits for approval" }, 202)
        }
        Object.assign(c, body, { revision: c.revision + 1 })
        delete (c as Record<string, unknown>).expected_revision
        return json(c)
      }
      if (method === "DELETE") {
        conns = conns.filter((x) => x !== c)
        return new Response(null, { status: 204 })
      }
    }
    if (method === "POST" && path === "/api/settings/mcp/connections") {
      if (conns.some((x) => x.name === body?.name)) return json({ error: "an MCP connection of that name exists" }, 409)
      if (body?.kind === "stdio") return json({ status: "needs_approval", approval_id: "apr-1", signed: false, message: "waits for approval" }, 202)
      const c = notion({ name: String(body?.name), url: String(body?.url), auth: String(body?.auth ?? "oauth"), rules: (body?.rules as Conn["rules"]) ?? [] })
      if (c.auth === "none") Object.assign(c, { authorization: "not_required", problem: undefined })
      conns.push(c)
      return json(c, 201)
    }
    m = path.match(/^\/api\/mcp\/connections\/([^/]+)\/oauth\/(begin|disconnect)$/)
    if (m && method === "POST") {
      const c = conns.find((x) => x.name === m![1])!
      if (m[2] === "disconnect") {
        Object.assign(c, { authorization: "required", attempt: undefined })
        return json({ upstream: "revoked" })
      }
      const id = `att-${Object.keys(attempts).length + 1}`
      attempts[id] = { id, connection: c.name, status: "pending", scope: "read", expires_at: 1900000000 }
      Object.assign(c, { authorization: "pending", attempt: id })
      return json({ id, authorize_url: `https://auth.notion.example/authorize?state=${id}`, expires_at: 1900000000 })
    }
    m = path.match(/^\/api\/mcp\/oauth\/attempts\/([^/]+)(?:\/(confirm|cancel))?$/)
    if (m) {
      const a = attempts[m[1]]
      if (!a) return json({ error: "this authorization attempt is over" }, 409)
      const c = conns.find((x) => x.name === a.connection)!
      if (method === "GET") return json(a)
      if (m[2] === "confirm") {
        a.status = "authorized"
        Object.assign(c, { authorization: "authorized", attempt: undefined, problem: undefined })
        return json(a)
      }
      if (m[2] === "cancel") {
        a.status = "cancelled"
        Object.assign(c, { authorization: "required", attempt: undefined })
        return new Response(null, { status: 204 })
      }
    }
    return json({ error: `unexpected ${method} ${path}` }, 500)
  }))
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  delete (window as { legatoShell?: unknown }).legatoShell
  delete (window as { ReactNativeWebView?: unknown }).ReactNativeWebView
})

// settle makes the provider send the browser back: the attempt waits for
// the operator's confirmation.
function settle(id: string) {
  attempts[id].status = "awaiting_confirmation"
  attempts[id].evidence = evidence
  const c = conns.find((x) => x.name === attempts[id].connection)!
  c.authorization = "awaiting_confirmation"
}

describe("MCPSettings", () => {
  it("lists each connection with where it stands", async () => {
    conns.push(notion({ name: "docs", auth: "none", authorization: "not_required", connection: "connected", problem: undefined }))
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    const row = await screen.findByRole("listitem", { name: "notion" })
    expect(within(row).getByText("https://mcp.notion.example/mcp")).toBeTruthy()
    expect(within(row).getByText("Connect it to let Legato use it.")).toBeTruthy()
    expect(within(row).getByRole("button", { name: "Connect notion" })).toBeTruthy()
    const docs = screen.getByRole("listitem", { name: "docs" })
    expect(within(docs).queryByRole("button", { name: /Connect docs/ })).toBeNull()
  })

  it("says up front that a browser alone connects only on Legato's own computer", async () => {
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    await screen.findByRole("listitem", { name: "notion" })
    expect(screen.getByText(/Browser-only connection setup requires Legato on this computer. For a remote server, use the Legato app./)).toBeTruthy()
  })

  it("connects from the browser: consent in a new tab, then the operator confirms the account", async () => {
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    fireEvent.click(await screen.findByRole("button", { name: "Connect notion" }))
    await waitFor(() => expect(opened?.location.href).toBe("https://auth.notion.example/authorize?state=att-1"))
    expect(opened?.opener).toBeNull()
    const begin = requests.find((r) => r.url.endsWith("/oauth/begin"))!
    expect(begin.body).toEqual({ mode: "web" })
    expect(begin.contentType).toBe("application/json")
    expect(await screen.findByText(/Waiting for you to consent/)).toBeTruthy()

    act(() => settle("att-1"))
    const confirm = await screen.findByRole("button", { name: "Confirm notion" })
    const row = screen.getByRole("listitem", { name: "notion" })
    expect(within(row).getByText("ana@example.test")).toBeTruthy()
    expect(within(row).getByText("https://auth.notion.example")).toBeTruthy()
    fireEvent.click(confirm)
    await waitFor(() => expect(within(screen.getByRole("listitem", { name: "notion" })).getByText("Authorized")).toBeTruthy())
    expect(requests.some((r) => r.method === "POST" && r.url === "/api/mcp/oauth/attempts/att-1/confirm")).toBe(true)
  })

  it("explains a consent answer that never arrived, and asks for the app on another computer", async () => {
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    fireEvent.click(await screen.findByRole("button", { name: "Connect notion" }))
    await screen.findByText(/Waiting for you to consent/)
    act(() => {
      attempts["att-1"].status = "expired"
      Object.assign(conns[0], { authorization: "required", attempt: undefined })
    })
    expect(await screen.findByText(/did not reach Legato in time/)).toBeTruthy()
    expect(screen.getAllByText(/For a remote server, use the Legato app./).length).toBe(2)
    expect(screen.getByRole("button", { name: "Connect notion" })).toBeTruthy()
  })

  it("says when consent was refused", async () => {
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    fireEvent.click(await screen.findByRole("button", { name: "Connect notion" }))
    await screen.findByText(/Waiting for you to consent/)
    act(() => {
      attempts["att-1"].status = "denied"
      Object.assign(conns[0], { authorization: "required", attempt: undefined })
    })
    expect(await screen.findByText("Authorization was refused on the consent page.")).toBeTruthy()
  })

  it("cancels an attempt", async () => {
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    fireEvent.click(await screen.findByRole("button", { name: "Connect notion" }))
    await screen.findByText(/Waiting for you to consent/)
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    await screen.findByRole("button", { name: "Connect notion" })
    expect(requests.some((r) => r.method === "POST" && r.url === "/api/mcp/oauth/attempts/att-1/cancel" && r.contentType === "application/json")).toBe(true)
  })

  it("finds an attempt waiting for confirmation again after a reload", async () => {
    attempts["att-7"] = { id: "att-7", connection: "notion", status: "awaiting_confirmation", expires_at: 1900000000, evidence }
    conns[0] = notion({ authorization: "awaiting_confirmation", attempt: "att-7" })
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    fireEvent.click(await screen.findByRole("button", { name: "Confirm notion" }))
    await waitFor(() => expect(requests.some((r) => r.url === "/api/mcp/oauth/attempts/att-7/confirm")).toBe(true))
  })

  it("connects through the Legato app, which receives the consent answer, then the page confirms", async () => {
    const shell = { mcp: 1, key: "k1" }
    ;(window as unknown as { legatoShell: unknown }).legatoShell = shell
    ;(window as unknown as { ReactNativeWebView: unknown }).ReactNativeWebView = { postMessage: (text: string) => posted.push(text) }
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    await screen.findByRole("listitem", { name: "notion" })
    expect(screen.queryByText(/Browser-only connection setup/)).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Connect notion" }))
    await waitFor(() => expect(posted.length).toBe(1))
    const asked = JSON.parse(posted[0])
    expect(asked).toMatchObject({ legatoShell: "mcpConnect", key: "k1", connection: "notion" })
    expect(window.open).not.toHaveBeenCalled()
    expect(requests.some((r) => r.url.endsWith("/oauth/begin"))).toBe(false)
    expect(await screen.findByText(/The Legato app opened the consent page/)).toBeTruthy()

    // The app began the attempt, caught the answer and handed it over.
    attempts["att-3"] = { id: "att-3", connection: "notion", status: "awaiting_confirmation", expires_at: 1900000000, evidence }
    Object.assign(conns[0], { authorization: "awaiting_confirmation", attempt: "att-3" })
    act(() => {
      window.dispatchEvent(new CustomEvent("legato:shell-mcp", { detail: { id: asked.id, connection: "notion", status: "awaiting_confirmation" } }))
    })
    fireEvent.click(await screen.findByRole("button", { name: "Confirm notion" }))
    await waitFor(() => expect(requests.some((r) => r.url === "/api/mcp/oauth/attempts/att-3/confirm")).toBe(true))
  })

  it("shows what the app could not do", async () => {
    ;(window as unknown as { legatoShell: unknown }).legatoShell = { mcp: 1, key: "k1" }
    ;(window as unknown as { ReactNativeWebView: unknown }).ReactNativeWebView = { postMessage: (text: string) => posted.push(text) }
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    fireEvent.click(await screen.findByRole("button", { name: "Connect notion" }))
    await waitFor(() => expect(posted.length).toBe(1))
    act(() => {
      window.dispatchEvent(new CustomEvent("legato:shell-mcp", { detail: { id: JSON.parse(posted[0]).id, connection: "notion", error: "The authorization server refused the redirect." } }))
    })
    expect(await screen.findByText("The authorization server refused the redirect.")).toBeTruthy()
  })

  it("adds a server", async () => {
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    await screen.findByRole("listitem", { name: "notion" })
    fireEvent.change(screen.getByRole("textbox", { name: "MCP server name" }), { target: { value: "linear" } })
    fireEvent.change(screen.getByRole("textbox", { name: "MCP server address" }), { target: { value: "https://mcp.linear.example/mcp" } })
    fireEvent.click(screen.getByRole("button", { name: "Add" }))
    expect(await screen.findByRole("listitem", { name: "linear" })).toBeTruthy()
    expect(requests.find((r) => r.method === "POST" && r.url === "/api/settings/mcp/connections")?.body).toEqual({ name: "linear", url: "https://mcp.linear.example/mcp", auth: "oauth" })
    fireEvent.change(screen.getByRole("textbox", { name: "MCP server name" }), { target: { value: "notion" } })
    fireEvent.change(screen.getByRole("textbox", { name: "MCP server address" }), { target: { value: "https://x.example/mcp" } })
    fireEvent.click(screen.getByRole("button", { name: "Add" }))
    expect((await screen.findByRole("alert")).textContent).toContain("exists")
  })

  it("says which agents may use which tools, saving against the revision it showed", async () => {
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    const row = await screen.findByRole("listitem", { name: "notion" })
    expect(within(row).getByText(/no agent may use its tools/)).toBeTruthy()
    fireEvent.change(within(row).getByRole("textbox", { name: "Tools of notion" }), { target: { value: "search" } })
    fireEvent.change(within(row).getByRole("textbox", { name: "Repository for notion" }), { target: { value: "acme/app" } })
    fireEvent.change(within(row).getByRole("combobox", { name: "Approval for notion" }), { target: { value: "auto" } })
    fireEvent.click(within(row).getByRole("button", { name: "Add rule" }))
    await waitFor(() => expect(within(screen.getByRole("listitem", { name: "notion" })).getByText(/1 rule/)).toBeTruthy())
    const patch = requests.find((r) => r.method === "PATCH")!
    expect(patch.body).toEqual({ rules: [{ tools: "search", repository: "acme/app", approve: "auto" }], expected_revision: 1 })

    fireEvent.click(within(screen.getByRole("listitem", { name: "notion" })).getByRole("button", { name: "Remove rule 1 of notion" }))
    await waitFor(() => expect(within(screen.getByRole("listitem", { name: "notion" })).getByText(/no agent may use its tools/)).toBeTruthy())
    expect(requests.filter((r) => r.method === "PATCH")[1].body).toEqual({ rules: [], expected_revision: 2 })
  })

  it("shows a change someone else made first, and reloads", async () => {
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    await screen.findByRole("listitem", { name: "notion" })
    conns[0].revision = 5
    fireEvent.click(screen.getByRole("button", { name: "Use notion" }))
    expect((await screen.findByRole("alert")).textContent).toContain("changed")
    fireEvent.click(screen.getByRole("button", { name: "Use notion" }))
    await waitFor(() => expect(screen.getByRole("button", { name: "Use notion" }).getAttribute("aria-pressed")).toBe("false"))
  })

  it("disconnects an authorized server and says whether the server revoked its tokens", async () => {
    conns[0] = notion({ authorization: "authorized", connection: "connected", problem: undefined })
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    fireEvent.click(await screen.findByRole("button", { name: "Disconnect" }))
    fireEvent.click(screen.getByRole("button", { name: "Disconnect notion" }))
    expect(await screen.findByText("Disconnected. The server revoked Legato's tokens.")).toBeTruthy()
    expect(screen.getByRole("button", { name: "Connect notion" }).textContent).toBe("Connect")
  })

  it("says Burnlite's connection is the board's too, and that disconnecting stops both", async () => {
    conns = [notion({ name: "burnlite", url: "https://burnlite.example/api/llm/mcp", authorization: "authorized", problem: undefined, shared_with: "burnlite" })]
    render(<MCPSettings baseUrl="" />)
    expect(await screen.findByText(/also the Burnlite board's connection/i)).toBeTruthy()
    fireEvent.click(await screen.findByRole("button", { name: "Disconnect" }))
    expect(screen.getByText(/Burnlite board stops syncing too/)).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Disconnect burnlite" }))
    expect(await screen.findByText(/The Burnlite board is disconnected too/)).toBeTruthy()
  })

  it("removes a server after asking", async () => {
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    fireEvent.click(await screen.findByRole("button", { name: "Remove" }))
    fireEvent.click(screen.getByRole("button", { name: "Remove notion" }))
    await waitFor(() => expect(screen.queryByRole("listitem", { name: "notion" })).toBeNull())
    expect(requests.some((r) => r.method === "DELETE" && r.url === "/api/settings/mcp/connections/notion")).toBe(true)
  })

  it("adds a server with a client registered for Legato by hand", async () => {
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    await screen.findByRole("listitem", { name: "notion" })
    fireEvent.change(screen.getByRole("textbox", { name: "MCP server name" }), { target: { value: "corp" } })
    fireEvent.change(screen.getByRole("textbox", { name: "MCP server address" }), { target: { value: "https://mcp.corp.example/mcp" } })
    fireEvent.change(screen.getByRole("textbox", { name: "Client ID" }), { target: { value: "legato-corp" } })
    fireEvent.change(screen.getByRole("textbox", { name: "Registered redirect" }), { target: { value: "http://127.0.0.1:47123/callback" } })
    fireEvent.change(screen.getByLabelText("Client secret"), { target: { value: "s3cret" } })
    fireEvent.click(screen.getByRole("button", { name: "Add" }))
    await screen.findByRole("listitem", { name: "corp" })
    expect(requests.find((r) => r.method === "POST" && r.url === "/api/settings/mcp/connections")?.body).toEqual({
      name: "corp", url: "https://mcp.corp.example/mcp", auth: "oauth",
      static_client_id: "legato-corp", static_redirect_uri: "http://127.0.0.1:47123/callback", static_client_secret: "s3cret",
    })
    expect((screen.getByLabelText("Client secret") as HTMLInputElement).value).toBe("")
  })

  it("adds a local server, which waits for the operator's approval before it is saved", async () => {
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    await screen.findByRole("listitem", { name: "notion" })
    fireEvent.change(screen.getByRole("combobox", { name: "Kind of server" }), { target: { value: "stdio" } })
    fireEvent.change(screen.getByRole("textbox", { name: "MCP server name" }), { target: { value: "design" } })
    fireEvent.change(screen.getByRole("textbox", { name: "Command" }), { target: { value: "node" } })
    fireEvent.change(screen.getByRole("textbox", { name: "Arguments, one per line" }), { target: { value: "tools/design/mcp.mjs\n" } })
    fireEvent.change(screen.getByRole("textbox", { name: "Working directory" }), { target: { value: "~/Development/autopilot-proposals-styles" } })
    fireEvent.change(screen.getByRole("textbox", { name: "Environment, one NAME=value per line" }), { target: { value: "PATH=/opt/node/bin:/usr/bin\n" } })
    fireEvent.click(screen.getByRole("button", { name: "Add" }))
    const waiting = await screen.findByText(/Waiting for your approval to run design/)
    expect(waiting.textContent).toMatch(/outside any sandbox/)
    expect(requests.find((r) => r.method === "POST" && r.url === "/api/settings/mcp/connections")?.body).toEqual({
      name: "design", kind: "stdio", command: "node", args: ["tools/design/mcp.mjs"],
      dir: "~/Development/autopilot-proposals-styles", env: { PATH: "/opt/node/bin:/usr/bin" },
    })
    expect(screen.queryByRole("listitem", { name: "design" })).toBeNull()
    // The operator approves it in the tray; the list finds it.
    conns.push(local())
    expect(await screen.findByRole("listitem", { name: "design" })).toBeTruthy()
  })

  it("shows what a local server runs, and tests it", async () => {
    conns = [local()]
    testResult = { tools: [{ name: "list_blocks", description: "Lists the blocks." }, { name: "render_block" }], stderr: "design server on 5199" }
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    const row = await screen.findByRole("listitem", { name: "design" })
    expect(within(row).getByText("node tools/design/mcp.mjs")).toBeTruthy()
    expect(within(row).getByText(/in \/home\/me\/Development\/autopilot-proposals-styles/)).toBeTruthy()
    expect(within(row).queryByRole("button", { name: "Connect design" })).toBeNull()
    fireEvent.click(within(row).getByRole("button", { name: "Test design" }))
    expect(await within(row).findByText("list_blocks")).toBeTruthy()
    expect(within(row).getByText("render_block")).toBeTruthy()
    expect(within(row).getByText(/design server on 5199/)).toBeTruthy()
  })

  it("says why a server failed its test, with what it wrote to stderr", async () => {
    conns = [local()]
    testResult = { tools: [], error: `command not found: "node" is not in PATH /usr/bin`, stderr: "" }
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    const row = await screen.findByRole("listitem", { name: "design" })
    fireEvent.click(within(row).getByRole("button", { name: "Test design" }))
    expect((await within(row).findByRole("alert")).textContent).toContain(`"node" is not in PATH`)
  })

  it("changes what a local server runs only through the operator's approval", async () => {
    conns = [local()]
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    const row = await screen.findByRole("listitem", { name: "design" })
    fireEvent.click(within(row).getByText("Change what it runs"))
    fireEvent.change(within(row).getByRole("textbox", { name: "Arguments of design, one per line" }), { target: { value: "tools/design/other.mjs" } })
    fireEvent.click(within(row).getByRole("button", { name: "Ask to change design" }))
    expect((await within(row).findByRole("status")).textContent).toMatch(/approv/i)
    expect(requests.find((r) => r.method === "PATCH")?.body).toEqual({
      command: "node", args: ["tools/design/other.mjs"], dir: "/home/me/Development/autopilot-proposals-styles", env: { PATH: "/opt/node/bin:/usr/bin" }, expected_revision: 1,
    })
    expect(within(screen.getByRole("listitem", { name: "design" })).getByText("node tools/design/mcp.mjs")).toBeTruthy()
  })
})

describe("An MCP connection's row", () => {
  it("marks a server that failed, one turned off, and one authorized", async () => {
    conns = [
      notion({ connection: "failed" }),
      local({ connection: "failed", enabled: false }),
      notion({ name: "ok", authorization: "authorized", problem: undefined }),
    ]
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    const remote = await screen.findByRole("listitem", { name: "notion" })
    expect(within(remote).getByText("unreachable")).toBeTruthy()
    expect(within(remote).getByText("Not connected").className).toContain("text-lw-text-2")
    const design = screen.getByRole("listitem", { name: "design" })
    expect(within(design).getByText("failed")).toBeTruthy()
    expect(within(design).getByText("off")).toBeTruthy()
    expect(within(design).getByText("local")).toBeTruthy()
    expect(within(design).getByText("No authorization needed").className).toContain("text-lw-live")
    expect((within(design).getByRole("button", { name: "Test design" }) as HTMLButtonElement).disabled).toBe(true)
    const ok = screen.getByRole("listitem", { name: "ok" })
    expect(within(ok).getByText("Authorized").className).toContain("text-lw-live")
    expect(within(ok).getByRole("button", { name: "Connect ok" }).textContent).toBe("Connect again")
  })

  it("offers to connect again a server that needs it, and nothing while it is off", async () => {
    conns = [notion({ authorization: "reauth_required" }), notion({ name: "off", enabled: false })]
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    expect((await screen.findByRole("button", { name: "Connect notion" })).textContent).toBe("Connect again")
    expect(screen.getByText("Needs connecting again")).toBeTruthy()
    expect((screen.getByRole("button", { name: "Connect off" }) as HTMLButtonElement).disabled).toBe(true)
    expect(screen.queryByRole("button", { name: "Disconnect" })).toBeNull()
  })

  it("links the consent page when the browser blocked the tab", async () => {
    vi.stubGlobal("open", vi.fn(() => null))
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    fireEvent.click(await screen.findByRole("button", { name: "Connect notion" }))
    const link = await screen.findByRole("link", { name: "Open the consent page" })
    expect(link.getAttribute("href")).toBe("https://auth.notion.example/authorize?state=att-1")
    expect(screen.getByText(/Waiting for you to consent in the tab Legato opened/)).toBeTruthy()
  })

  it("closes the tab and says why when connecting cannot begin", async () => {
    conns = [notion()]
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    const row = await screen.findByRole("listitem", { name: "notion" })
    const tab = { location: { href: "" }, opener: window, close: vi.fn() }
    vi.stubGlobal("open", vi.fn(() => tab))
    vi.mocked(fetch).mockImplementationOnce(async () => json({ error: "the server names no authorization server" }, 502))
    fireEvent.click(within(row).getByRole("button", { name: "Connect notion" }))
    expect((await within(row).findByRole("alert")).textContent).toContain("names no authorization server")
    expect(tab.close).toHaveBeenCalled()
  })

  it("says why an attempt failed or stopped", async () => {
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    fireEvent.click(await screen.findByRole("button", { name: "Connect notion" }))
    await screen.findByText(/Waiting for you to consent/)
    act(() => { Object.assign(attempts["att-1"], { status: "failed", error_code: "token_exchange_failed" }); Object.assign(conns[0], { authorization: "required", attempt: undefined }) })
    expect(await screen.findByText("The authorization server did not accept the authorization. Connect again.")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Connect notion" }))
    await waitFor(() => expect(attempts["att-2"]).toBeTruthy())
    act(() => { Object.assign(attempts["att-2"], { status: "failed", error_code: "" }); Object.assign(conns[0], { authorization: "required", attempt: undefined }) })
    expect(await screen.findByText("Authorizing failed (unknown). Connect again.")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Connect notion" }))
    await waitFor(() => expect(attempts["att-3"]).toBeTruthy())
    act(() => { attempts["att-3"].status = "interrupted"; Object.assign(conns[0], { authorization: "required", attempt: undefined }) })
    expect(await screen.findByText(/Legato stopped listening for the consent page's answer/)).toBeTruthy()
  })

  it("shows the scope asked for and an authorization that does not renew", async () => {
    attempts["att-7"] = { id: "att-7", connection: "notion", status: "awaiting_confirmation", expires_at: 1900000000, scope: "write",
      evidence: { issuer: "https://auth.example", resource: "https://r.example", refreshable: false } }
    conns[0] = notion({ authorization: "awaiting_confirmation", attempt: "att-7" })
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    const row = await screen.findByRole("listitem", { name: "notion" })
    await within(row).findByRole("button", { name: "Confirm notion" })
    expect(within(row).getByText("write")).toBeTruthy()
    expect(within(row).getByText("No: connect again when it expires")).toBeTruthy()
    expect(within(row).queryByText("Account")).toBeNull()
    expect(within(row).queryByRole("button", { name: "Connect notion" })).toBeNull()
    fireEvent.click(within(row).getByRole("button", { name: "Cancel" }))
    await within(row).findByRole("button", { name: "Connect notion" })
  })

  it("counts the tools a test found and names those it did not offer", async () => {
    conns = [notion({ authorization: "not_required", auth: "none", problem: undefined })]
    testResult = { tools: [{ name: "search" }], rejected: [{ name: "bad", reason: "its schema names a remote reference" }] }
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    const row = await screen.findByRole("listitem", { name: "notion" })
    expect(within(row).queryByText(/Starts it, lists its tools/)).toBeNull()
    fireEvent.click(within(row).getByRole("button", { name: "Test notion" }))
    expect(await within(row).findByText("1 tool:")).toBeTruthy()
    expect(within(row).getByText("bad").parentElement?.textContent).toBe("Not offered: bad, its schema names a remote reference")
    expect(within(row).queryByText("Error output")).toBeNull()
  })

  it("describes each rule in words", async () => {
    conns = [notion({ rules: [
      { tools: "search", repository: "acme/app", approve: "auto", access: "read" },
      { tools: "*", repository: "*/*", approve: "odd", access: "write" },
      { tools: "*", repository: "-/-", approve: "deny" },
    ] })]
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    const rules = await screen.findByRole("list", { name: "Rules for notion" })
    expect(within(rules).getAllByRole("listitem").map((li) => li.firstChild?.textContent)).toEqual([
      "read-only tools matching search, for agents in acme/app: allow without asking",
      "tools that change things matching *, for agents in */*: odd",
      "tools matching *, for agents in -/-: refuse",
    ])
    expect(screen.getByText(/Agent access: 3 rules/)).toBeTruthy()
  })

  it("keeps a server when removing it is not confirmed", async () => {
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    fireEvent.click(await screen.findByRole("button", { name: "Remove" }))
    expect(screen.getByText("Remove notion, its authorization and its rules?")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Keep" }))
    expect(screen.queryByRole("button", { name: "Remove notion" })).toBeNull()
    expect(requests.some((r) => r.method === "DELETE")).toBe(false)
  })

  it("keeps a server connected when disconnecting is not confirmed", async () => {
    conns[0] = notion({ authorization: "authorized", problem: undefined })
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    fireEvent.click(await screen.findByRole("button", { name: "Disconnect" }))
    expect(screen.queryByRole("button", { name: "Connect notion" })).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Keep" }))
    expect(screen.getByRole("button", { name: "Connect notion" })).toBeTruthy()
  })

  it("turns a server off", async () => {
    render(<MCPSettings baseUrl="" pollEvery={10} />)
    fireEvent.click(await screen.findByRole("button", { name: "Use notion" }))
    await waitFor(() => expect(requests.find((r) => r.method === "PATCH")?.body).toEqual({ enabled: false, expected_revision: 1 }))
    expect(await screen.findByText("off")).toBeTruthy()
  })
})

function local(over: Partial<Conn> = {}): Conn {
  return {
    name: "design", url: "", kind: "stdio", auth: "none", enabled: true, revision: 1, rules: [], authorization: "not_required", connection: "unknown",
    command: "node", args: ["tools/design/mcp.mjs"], dir: "/home/me/Development/autopilot-proposals-styles", env: { PATH: "/opt/node/bin:/usr/bin" }, ...over,
  }
}
