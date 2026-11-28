import { describe, expect, it } from "vitest"
import { buildSpawnRequest, spawnWorkingDir, type SpawnChoices } from "../spawnRequest"

const plain: SpawnChoices = {
  title: "", agentKind: "", effectiveAdapter: "chimera", account: "", path: "~/Projects/legato",
  inClone: false, branch: "", baseBranch: "",
  offersSessionChoice: false, sessionPolicy: "resume", resources: [],
  unsandboxed: false, hostNetworkOffered: false, hostNetwork: false,
  prompt: "", tier: "", engramShown: false, engram: "", canRemember: false, rememberEngram: false,
}

describe("buildSpawnRequest", () => {
  it("sends only the title, tool and folder when nothing else is chosen", () => {
    expect(buildSpawnRequest(plain)).toEqual({ title: "", agentKind: "", workingDir: "~/Projects/legato" })
  })

  it("trims the title, the path and the first message", () => {
    const request = buildSpawnRequest({ ...plain, title: "  fix  ", path: " ~/x ", prompt: "  go  " })
    expect(request).toEqual({ title: "fix", agentKind: "", workingDir: "~/x", prompt: "go" })
  })

  it("keeps the keys in the order the server's body is built from", () => {
    const request = buildSpawnRequest({
      ...plain, agentKind: "codex", effectiveAdapter: "claude-code", account: "work", offersSessionChoice: true, sessionPolicy: "new",
      resources: ["Vault"], unsandboxed: true, prompt: "go", tier: "smart", engramShown: true, engram: "argus", canRemember: true, rememberEngram: true,
    })
    expect(Object.keys(request)).toEqual(["title", "agentKind", "account", "workingDir", "sessionPolicy", "resources", "sandbox", "prompt", "tier", "engram", "rememberEngram"])
    expect(request.agentKind).toBe("claude-code")
  })

  describe("the account", () => {
    const claude = { ...plain, effectiveAdapter: "claude-code", account: "work" }
    it("is sent for Claude Code, naming the tool", () => {
      expect(buildSpawnRequest(claude)).toMatchObject({ agentKind: "claude-code", account: "work" })
    })
    it("is not sent while resuming the saved session", () => {
      expect(buildSpawnRequest({ ...claude, offersSessionChoice: true }).account).toBeUndefined()
    })
    it("is sent for a new session", () => {
      expect(buildSpawnRequest({ ...claude, offersSessionChoice: true, sessionPolicy: "new" }).account).toBe("work")
    })
    it("is not sent for another tool", () => {
      expect(buildSpawnRequest({ ...claude, effectiveAdapter: "codex" }).account).toBeUndefined()
    })
  })

  describe("a clone", () => {
    const clone = { ...plain, inClone: true, branch: " feat ", baseBranch: " main " }
    it("is new, of the repository, with the branches trimmed", () => {
      expect(buildSpawnRequest({ ...clone, cloneRepo: "o/r" })).toEqual({
        title: "", agentKind: "", workingDir: "", clone: { repo: "o/r", baseBranch: "main", branch: "feat" },
      })
    })
    it("is joined on a new branch when it exists", () => {
      expect(buildSpawnRequest({ ...clone, cloneRepo: "o/r", jobClone: { id: 3, busy: false } }).joinClone).toEqual({ id: 3, branch: "feat" })
    })
    it("is joined on its own branch while an agent works in it", () => {
      expect(buildSpawnRequest({ ...clone, jobClone: { id: 5, busy: true } }).joinClone).toEqual({ id: 5, branch: "" })
    })
    it("is nothing without a repository", () => {
      expect(buildSpawnRequest(clone)).toEqual({ title: "", agentKind: "", workingDir: "" })
    })
    it("is ignored in a folder", () => {
      expect(buildSpawnRequest({ ...plain, cloneRepo: "o/r", jobClone: { id: 3, busy: false } })).toEqual(buildSpawnRequest(plain))
    })
  })

  it("sends the host network only when it is offered", () => {
    expect(buildSpawnRequest({ ...plain, hostNetwork: true }).dockerHostNetwork).toBeUndefined()
    expect(buildSpawnRequest({ ...plain, hostNetwork: true, hostNetworkOffered: true }).dockerHostNetwork).toBe(true)
  })

  it("sends the engram and remember only when they are offered", () => {
    expect(buildSpawnRequest({ ...plain, engram: "argus", rememberEngram: true })).toEqual(buildSpawnRequest(plain))
    expect(buildSpawnRequest({ ...plain, engramShown: true, engram: "argus", rememberEngram: true })).toMatchObject({ engram: "argus" })
    expect(buildSpawnRequest({ ...plain, canRemember: true, rememberEngram: true }).rememberEngram).toBe(true)
  })
})

describe("spawnWorkingDir", () => {
  it("is the trimmed path, or empty in a clone", () => {
    expect(spawnWorkingDir({ inClone: false, path: " ~/a " })).toBe("~/a")
    expect(spawnWorkingDir({ inClone: true, path: "~/a" })).toBe("")
  })
})
