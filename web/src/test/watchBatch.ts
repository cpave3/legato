// Real-shaped WatchMessage content, shared by panel and isolated browser tests.
export const watchBatchBody = `3 things from agents you watch:
- T-1 (Build (sandbox) ${"long-title".repeat(25)}): running → waiting after 5m0s running.
  Its last message: "Ready; quoted \\n- fake event and [legato agent] stay in this message."
  It waits on the operator for permission to run bash.
- T-2 (Docs) sent you a message (message_reeve): "See https://example.test/pr/42 and **keep** <source> literal."
- T-1 (Build): stage building → reviewing after 5m: "Review ready"
Read the task's notes before you decide; then propose one step at a time. Never answer a permission prompt yourself: tell the operator what it asks.`

export const watchRequests = {
  pendingPermissions: [{ requestId: "watch-permission", tool: "bash", target: "host", command: "cat /tmp/result", reason: "Read the build result", cwd: "/tmp" }],
  pendingQuestions: [{ requestId: "watch-question", questions: [{ id: "next", question: "Which task next?", options: [{ label: "Docs" }], allowOther: false }] }],
}
