import { describe, expect, it } from "vitest"
import { DEFAULT_CONFIG } from "./config"
import { prepareGitHubWorkflow } from "./actions"
import type { WorkflowDiscovery } from "./legato"

const prInputs = {
  repo: { type: "string", required: true },
  pr_number: { type: "integer", required: true },
  primary_dir: { type: "string", required: true },
}
const discovery: WorkflowDiscovery = {
  definitions: [{ name: "review-pr", inputs: prInputs }, { name: "discuss-pr", inputs: prInputs }],
  errors: [],
  repositories: [{ name: "Legato", repo: "cpave3/legato", primary_dir: "/projects/legato" }],
}

describe("prepareGitHubWorkflow", () => {
  it("resolves a configured workflow and canonical repository preset for confirmation", () => {
    expect(prepareGitHubWorkflow(DEFAULT_CONFIG, { repository: "CPAVE3/LEGATO", pullRequestNumber: 42 }, discovery)).toEqual({
      workflowName: "review-pr",
      repositoryPresetName: "Legato",
      repository: "cpave3/legato",
      pullRequestNumber: 42,
      primaryDir: "/projects/legato",
      inputs: { repo: "cpave3/legato", pr_number: "42", primary_dir: "/projects/legato" },
    })
  })

  it("resolves the discussion workflow when asked for it", () => {
    expect(prepareGitHubWorkflow(DEFAULT_CONFIG, { repository: "cpave3/legato", pullRequestNumber: 42 }, discovery, "discuss")).toMatchObject({
      workflowName: "discuss-pr",
      inputs: { repo: "cpave3/legato", pr_number: "42", primary_dir: "/projects/legato" },
    })
    expect(() => prepareGitHubWorkflow({ ...DEFAULT_CONFIG, discussWorkflowName: "missing" }, { repository: "cpave3/legato", pullRequestNumber: 1 }, discovery, "discuss"))
      .toThrow('Workflow "missing" was not found')
  })

  it("rejects an absent or ambiguous repository preset rather than guessing", () => {
    expect(() => prepareGitHubWorkflow(DEFAULT_CONFIG, { repository: "other/repo", pullRequestNumber: 1 }, discovery)).toThrow("No Legato repository preset")
    expect(() => prepareGitHubWorkflow(DEFAULT_CONFIG, { repository: "cpave3/legato", pullRequestNumber: 1 }, {
      ...discovery,
      repositories: [...discovery.repositories!, { name: "Other", repo: "CPAVE3/LEGATO", primary_dir: "/tmp/other" }],
    })).toThrow("Multiple Legato repository presets")
  })

  it("rejects missing workflows and incompatible configured inputs", () => {
    expect(() => prepareGitHubWorkflow({ ...DEFAULT_CONFIG, workflowName: "missing" }, { repository: "cpave3/legato", pullRequestNumber: 1 }, discovery)).toThrow('Workflow "missing" was not found')
    expect(() => prepareGitHubWorkflow({ ...DEFAULT_CONFIG, repoInput: "pr_number" }, { repository: "cpave3/legato", pullRequestNumber: 1 }, discovery)).toThrow("must be distinct")
    expect(() => prepareGitHubWorkflow(DEFAULT_CONFIG, { repository: "cpave3/legato", pullRequestNumber: 1 }, {
      ...discovery,
      definitions: [{ name: "review-pr", inputs: { repo: { type: "repository" }, pr_number: { type: "integer" }, primary_dir: { type: "string" } } }],
    })).toThrow('input "repo" must have type string')
    expect(() => prepareGitHubWorkflow(DEFAULT_CONFIG, { repository: "cpave3/legato", pullRequestNumber: 1 }, {
      ...discovery,
      definitions: [{ name: "review-pr", inputs: {
        ...discovery.definitions![0].inputs,
        reviewer: { type: "string", required: true },
      } }],
    })).toThrow('Required workflow input "reviewer" is not mapped')
  })
})
