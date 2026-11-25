package workflow

import "testing"

func TestGraphKeysAThreadReadByItsThread(t *testing.T) {
	graph := BuildGraph(mustParse(t, `
schema_version: 1
name: ops
title: Ops
steps:
  - {id: thread, title: Thread, type: chat.thread.read, with: {bot: slack, channel: C1, thread_id: "1.5"}}
`))
	if node := nodeByID(graph, "step:thread"); node == nil || node.Step == nil || node.Step.Key != (GraphKey{Field: "thread_id", Value: "1.5"}) {
		t.Fatalf("thread node = %+v", node)
	}
}
