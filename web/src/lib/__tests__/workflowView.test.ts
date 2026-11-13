import { describe, expect, it } from "vitest"
import { ago, attentionCopy, elapsed, inputText, runWithAttention, producedValues, secretInputNames, stepMark, inputsLabel, runState, runSubject, runSummary, sourceLabel, stepIntent, stepPosition, stepSegments, triggerText } from "../workflowView"

const step = (step_id: string, title: string, status: string) => ({ step_id, title, status })

describe("runState", () => {
  it("names each run state in the board's tones", () => {
    expect(runState({ status: "running" })).toEqual({ tone: "live", label: "Running" })
    expect(runState({ status: "running", needs_attention: true })).toEqual({ tone: "needs", label: "Needs you" })
    expect(runState({ status: "waiting", needs_attention: true })).toEqual({ tone: "needs", label: "Needs you" })
    expect(runState({ status: "succeeded" })).toEqual({ tone: "done", label: "Done" })
    expect(runState({ status: "failed" })).toEqual({ tone: "error", label: "Failed" })
    expect(runState({ status: "cancelled" })).toEqual({ tone: "idle", label: "Cancelled" })
  })

  it("counts a run waiting for its agent to end as running, not as waiting on the operator", () => {
    expect(runState({ status: "waiting" })).toEqual({ tone: "live", label: "Running" })
  })
})

describe("stepSegments", () => {
  it("greys finished steps, colours the current one by the run's state, and leaves the rest empty", () => {
    const steps = [step("a", "Read", "succeeded"), step("b", "Plan", "waiting_agent"), step("c", "Note", "pending")]
    expect(stepSegments({ status: "running" }, steps)).toEqual(["ok", "live", "empty"])
    expect(stepSegments({ status: "running", needs_attention: true }, steps)).toEqual(["ok", "needs", "empty"])
  })

  it("marks where a failed or cancelled run stopped", () => {
    expect(stepSegments({ status: "failed" }, [step("a", "Prepare", "failed"), step("b", "Run", "pending")])).toEqual(["error", "empty"])
    expect(stepSegments({ status: "cancelled" }, [step("a", "Read", "succeeded"), step("b", "Plan", "cancelled"), step("c", "Note", "cancelled")])).toEqual(["ok", "idle", "empty"])
  })

  it("fills every segment of a done run", () => {
    expect(stepSegments({ status: "succeeded" }, [step("a", "Read", "succeeded"), step("b", "Skip", "skipped")])).toEqual(["done", "done"])
  })
})

describe("stepPosition", () => {
  it("names the step a run is on, counting from one", () => {
    const steps = [step("a", "Read the task", "succeeded"), step("b", "Record triage note", "succeeded"), step("c", "Move to Ready", "waiting_manual")]
    expect(stepPosition({ status: "waiting" }, steps)).toEqual({ number: 3, total: 3, title: "Move to Ready" })
  })

  it("has no current step once a run is done", () => {
    expect(stepPosition({ status: "succeeded" }, [step("a", "Read", "succeeded")])).toBeNull()
  })
})

describe("runSummary", () => {
  const triage = [step("context", "Read the task", "succeeded"), { ...step("approve", "Move to Ready", "waiting_manual"), trigger: "manual" }]

  it("says what waits for the operator", () => {
    expect(runSummary({ status: "waiting", needs_attention: true, attention_reason: "manual action required: Move to Ready", steps: triage })).toEqual({ text: "“Move to Ready” waits for you to run it", tone: "needs" })
    const agent = [step("checkout", "Prepare checkout", "succeeded"), step("agent", "Run agent", "waiting_agent")]
    expect(runSummary({ status: "running", needs_attention: true, attention_reason: "agent idle before step completion", steps: agent }).text).toBe("The agent went idle before finishing “Run agent”")
    expect(runSummary({ status: "running", needs_attention: true, attention_reason: "agent waiting for input", steps: agent }).text).toBe("The agent is waiting for an answer in “Run agent”")
    expect(runSummary({ status: "running", needs_attention: true, attention_reason: "agent error before step completion", steps: agent }).text).toBe("The agent hit an error before finishing “Run agent”")
    expect(runSummary({ status: "running", needs_attention: true, attention_reason: "agent session ended before step completion", steps: agent }).text).toBe("The agent ended before finishing “Run agent”")
    expect(runSummary({ status: "running", needs_attention: true, attention_reason: "something new", steps: agent }).text).toBe("something new")
  })

  it("says what a running run is doing", () => {
    expect(runSummary({ status: "running", steps: [step("read", "Read the task", "succeeded"), step("plan", "Write the plan", "waiting_agent")] })).toEqual({ text: "An agent is working on “Write the plan”", tone: undefined })
    expect(runSummary({ status: "running", steps: [step("prepare", "Prepare checkout", "running")] }).text).toBe("Running “Prepare checkout”")
    expect(runSummary({ status: "waiting", steps: [step("agent", "Run agent", "succeeded"), { ...step("end", "Finish", "waiting_manual"), trigger: "agent_ended" }] }).text).toBe("“Finish” starts when the agent ends")
  })

  it("names the step that failed and why", () => {
    expect(runSummary({ status: "failed", error: "column \"Shipped\" not found", steps: [step("note", "Record release note", "succeeded"), step("move", "Move to Shipped", "failed")] })).toEqual({ text: "Move to Shipped: column \"Shipped\" not found", tone: "error" })
    expect(runSummary({ status: "failed", error: "boom", steps: [] }).text).toBe("boom")
  })

  it("says how long a finished run took", () => {
    expect(runSummary({ status: "succeeded", started_at: "2026-11-13T04:30:31Z", completed_at: "2026-11-13T04:34:43Z", steps: [] }).text).toBe("Took 4m 12s")
    expect(runSummary({ status: "cancelled", error: "cancelled by user", steps: [] })).toEqual({ text: "cancelled by user", tone: undefined })
  })
})

describe("stepIntent", () => {
  const task = { id: "gpri1ycr", title: "Board search ignores workspace filter" }

  it("says what a step will do before the operator runs it", () => {
    expect(stepIntent({ executor_type: "task.move", resolved_inputs_json: JSON.stringify({ task, column: "Ready" }) })).toBe("Moves gpri1ycr “Board search ignores workspace filter” to Ready.")
    expect(stepIntent({ executor_type: "worktree.remove", resolved_inputs_json: JSON.stringify({ expected_path: "/wt/legato-412" }) })).toBe("Removes the worktree at /wt/legato-412.")
    expect(stepIntent({ executor_type: "task.note", resolved_inputs_json: JSON.stringify({ task }) })).toBe("Adds a note to gpri1ycr “Board search ignores workspace filter”.")
    expect(stepIntent({ executor_type: "agent.stop", resolved_inputs_json: JSON.stringify({ task }) })).toBe("Stops the agent of gpri1ycr “Board search ignores workspace filter”.")
  })

  it("has nothing to say about a step it does not know", () => {
    expect(stepIntent({ executor_type: "github.pr.prepare", resolved_inputs_json: "{}" })).toBeNull()
    expect(stepIntent({ executor_type: "task.move", resolved_inputs_json: "not json" })).toBeNull()
  })
})

describe("triggerText", () => {
  it("says when a workflow starts on its own", () => {
    expect(triggerText({ "task.entered_column": { column: "Backlog" } })).toBe("Starts when a task enters Backlog")
    expect(triggerText({ "task.entered_column": { column: "Done", confirm: true } })).toBe("Asks to start when a task enters Done")
    expect(triggerText({})).toBeNull()
    expect(triggerText(undefined)).toBeNull()
  })
})

describe("labels", () => {
  it("names where a definition comes from", () => {
    expect(sourceLabel("builtin:legato-job")).toBe("built in")
    expect(sourceLabel("/home/me/.config/legato/workflows/plan-task.yaml")).toBe("plan-task.yaml")
  })

  it("says what a workflow takes", () => {
    expect(inputsLabel({ task: { type: "task", required: true } })).toBe("a task")
    expect(inputsLabel({ repo: { type: "string", required: true }, pr_number: { type: "integer", required: true }, primary_dir: { type: "string", required: true } })).toBe("repo, pr number, primary dir")
    expect(inputsLabel({ prompt: { type: "string", required: true }, agent: { type: "string" }, branch: { type: "string" }, ref: { type: "string" }, repo: { type: "string" }, working_dir: { type: "string" } })).toBe("prompt, agent, branch, ref…")
    expect(inputsLabel({})).toBe("")
  })

  it("formats how long something took, and how long ago", () => {
    expect(elapsed("2026-11-13T04:30:31Z", "2026-11-13T04:30:31Z")).toBe("0s")
    expect(elapsed("2026-11-13T04:30:31Z", "2026-11-13T04:34:43Z")).toBe("4m 12s")
    expect(elapsed("2026-11-13 04:00:00", "2026-11-13 06:05:00")).toBe("2h 5m")
    expect(ago("2026-11-13T04:30:00Z", Date.parse("2026-11-13T04:42:30Z"))).toBe("12m")
    expect(ago("2026-11-13T04:30:00Z", Date.parse("2026-11-13T04:30:20Z"))).toBe("now")
    expect(ago("2026-11-11T04:30:00Z", Date.parse("2026-11-13T05:30:00Z"))).toBe("2d")
  })
})

describe("runSubject", () => {
  it("names the task a run is for, else its prompt", () => {
    expect(runSubject({ related_task_title: "Sync retries hammer Jira", inputs_json: "{}" })).toBe("Sync retries hammer Jira")
    expect(runSubject({ inputs_json: JSON.stringify({ prompt: "Find why the sync test is flaky" }) })).toBe("Find why the sync test is flaky")
    expect(runSubject({ inputs_json: "{}" })).toBe("")
  })
})

describe("producedValues", () => {
  it("lists what agents reported, leaving files to the artifacts and other steps' bookkeeping to the steps", () => {
    const steps = [
      { step_id: "context", title: "Read the task", executor_type: "task.context", outputs_json: JSON.stringify({ content: "# Task" }) },
      { step_id: "plan", title: "Write the plan", executor_type: "agent.run", outputs_json: JSON.stringify({ plan: { artifact_id: "wfa-1", path: "/x/plan.md" }, risk: "low", findings: [{ line: 3 }] }) },
      { step_id: "note", title: "Attach", executor_type: "task.note", outputs_json: JSON.stringify({ note_id: "tn-1" }) },
    ]
    expect(producedValues(steps)).toEqual([
      { step: "Write the plan", key: "risk", value: "low" },
      { step: "Write the plan", key: "findings", value: '[{"line":3}]' },
    ])
    expect(producedValues([{ title: "X", executor_type: "agent.run", outputs_json: "not json" }])).toEqual([])
  })
})

describe("attentionCopy", () => {
  it("turns each attention reason into a heading and a sentence about the step", () => {
    expect(attentionCopy("agent idle before step completion", "Run agent")).toEqual({ heading: "The agent went idle before finishing", sentence: "The agent went idle before finishing “Run agent”" })
    expect(attentionCopy("agent waiting for input", "Run agent")).toEqual({ heading: "The agent is waiting for an answer", sentence: "The agent is waiting for an answer in “Run agent”" })
    expect(attentionCopy("agent error before step completion", "Run agent").heading).toBe("The agent hit an error")
    expect(attentionCopy("agent session ended before step completion", "Run agent").heading).toBe("The agent ended before finishing")
    expect(attentionCopy("manual action required: Move to Ready", "Move to Ready")).toEqual({ heading: "Move to Ready waits for you", sentence: "“Move to Ready” waits for you to run it" })
    expect(attentionCopy("something new", "X")).toEqual({ heading: "something new", sentence: "something new" })
  })
})

describe("stepMark", () => {
  it("marks each step by its state", () => {
    const mark = (status: string, extra: { trigger?: string; needs?: boolean } = {}) => stepMark({ step_id: "s", title: "S", status, trigger: extra.trigger }, { status: "running", needs_attention: extra.needs })
    expect(mark("succeeded")).toEqual({ glyph: "✓", tone: "done" })
    expect(mark("pending")).toEqual({ glyph: "○", tone: null })
    expect(mark("failed")).toEqual({ glyph: "✕", tone: "error" })
    expect(mark("cancelled")).toEqual({ glyph: "–", tone: "idle" })
    expect(mark("waiting_agent")).toEqual({ glyph: "●", tone: "live" })
    expect(mark("waiting_agent", { needs: true })).toEqual({ glyph: "◆", tone: "needs" })
    expect(mark("waiting_manual", { trigger: "manual", needs: true })).toEqual({ glyph: "◆", tone: "needs" })
    expect(mark("waiting_manual", { trigger: "agent_ended" })).toEqual({ glyph: "●", tone: "live" })
  })
})

describe("secret inputs", () => {
  const definition = JSON.stringify({ inputs: { prompt: { type: "string", secret: true }, repo: { type: "string" } } })

  it("names the inputs a definition marks secret", () => {
    expect([...secretInputNames(definition)]).toEqual(["prompt"])
    expect(secretInputNames(undefined).size).toBe(0)
  })

  it("never shows a secret prompt as a run's subject", () => {
    expect(runSubject({ inputs_json: JSON.stringify({ prompt: "token=abc" }), definition_json: definition })).toBe("")
  })
})

describe("runWithAttention", () => {
  it("counts a run as needing the operator when one of its agents does, as the runs list does", () => {
    expect(runWithAttention({ status: "running" }, [{ needs_attention: true }]).needs_attention).toBe(true)
    expect(runWithAttention({ status: "running" }, [{}]).needs_attention).toBe(false)
    expect(runWithAttention({ status: "waiting", needs_attention: true }, []).needs_attention).toBe(true)
  })
})

describe("inputText", () => {
  it("shows a task input as its ID and title, and other values as text", () => {
    expect(inputText({ id: "xk2pb7do", title: "Add keyboard help", description: "", metadata: {} })).toBe("xk2pb7do Add keyboard help")
    expect(inputText({ name: "Legato", repo: "cpave3/legato", primary_dir: "/p", base_branch: "main" })).toBe("Legato (cpave3/legato)")
    expect(inputText("staging")).toBe("staging")
    expect(inputText(42)).toBe("42")
    expect(inputText({ a: 1 })).toBe('{"a":1}')
  })
})
