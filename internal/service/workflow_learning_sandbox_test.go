package service

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/sandbox"
	"github.com/cpave3/legato/internal/engine/store"
)

func TestLearningWorkflowAgentsUseTheirTasksCloneAcrossHomeSpellings(t *testing.T) {
	for _, dataLinked := range []bool{false, true} {
		for _, recordLinked := range []bool{false, true} {
			for _, workLinked := range []bool{false, true} {
				name := fmtHomeSpellings(dataLinked, recordLinked, workLinked)
				t.Run(name, func(t *testing.T) {
					linked, real := linkedHome(t)
					home := func(link bool) string {
						if link {
							return linked
						}
						return real
					}
					data := filepath.Join(home(dataLinked), ".local/share/legato")
					cloneAt := func(link bool) string { return filepath.Join(home(link), ".local/share/legato/jobs/ex-old/widget") }
					if err := os.MkdirAll(cloneAt(false), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(data, "auth-token"), []byte("private fixture"), 0o600); err != nil {
						t.Fatal(err)
					}
					backend := &fakeBackend{}
					agents, db, _ := sandboxedService(t, backend, func(s *SandboxSettings) { s.Base.DataDir = data; s.Base.DefaultMounts = false })
					ctx := context.Background()
					createTask(t, db, "original")
					createTask(t, db, "subject")
					id, err := db.CreateJobClone(ctx, store.JobClone{ExecutionID: "ex-old", TaskID: "original", Repo: "acme/widget", Path: cloneAt(recordLinked), CreatedAt: time.Now()})
					if err != nil {
						t.Fatal(err)
					}
					// A task can own the clone by joining it for the next branch of a stack.
					if err := db.AttachJobCloneTask(ctx, id, "subject", time.Now()); err != nil {
						t.Fatal(err)
					}
					definitions, err := BuiltInTriggeredLearningWorkflows([]string{"Review"}, LearningWorkflowOptions{Adapter: "claude-code", PacketMaxItems: 25, PacketMaxBytes: 8192})
					if err != nil {
						t.Fatal(err)
					}
					// Separate the two agent lifetimes: automatic advancement before
					// cleanup of a live curator is outside this sandbox regression.
					definitions[0].Steps[3].Trigger = "manual"
					learning := NewLearningService(db, nil)
					interview := &fakeWorkflowExecutor{typeName: "agent.interview", result: WorkflowExecutionResult{Status: "waiting_agent"}}
					workflows := NewWorkflowService(db, []WorkflowExecutor{interview, NewLearningPacketPrepareExecutor(learning), NewAgentRunExecutor(NewWorkflowAgentRunner(agents, db)), NewLearningProposalsSubmitExecutor(learning)})
					view, err := workflows.Start(ctx, definitions[0], map[string]string{"task": "subject", "working_dir": cloneAt(workLinked)})
					if err != nil {
						t.Fatal(err)
					}
					view, err = workflows.Complete(ctx, view.Run.ID, "interview", view.Attempts[0].ID, map[string]string{"available": "true", "request_id": "reflection"}, nil)
					if err != nil {
						t.Fatalf("start curator: %v", err)
					}
					for i, step := range []string{"curator", "critic"} {
						if step == "critic" {
							view, err = workflows.TriggerStep(ctx, view.Run.ID, step, false)
							if err != nil {
								t.Fatal(err)
							}
						}
						if view.Run.CurrentStepID != step || view.Run.Status != "running" {
							t.Fatalf("want %s running: %+v", step, view.Run)
						}
						session, err := db.GetAgentSessionByTaskID(ctx, view.Run.TaskID)
						if err != nil || !session.Sandboxed {
							t.Fatalf("sandboxed session: %+v, %v", session, err)
						}
						if len(backend.plans) != i+1 {
							t.Fatalf("sandbox plans: %d", len(backend.plans))
						}
						plan := backend.plans[i]
						if plan.Chdir != cloneAt(false) {
							t.Fatalf("working directory = %s", plan.Chdir)
						}
						if m, ok := mountOf(plan, cloneAt(false)); !ok || m.Kind != sandbox.ReadWrite {
							t.Fatalf("clone mount: %+v, %v", m, ok)
						}
						t.Run(step+"-bubblewrap", func(t *testing.T) {
							if err := exec.Command("bwrap", "--ro-bind", "/", "/", "--unshare-pid", "true").Run(); err != nil {
								t.Skipf("bwrap cannot run here: %v", err)
							}
							script := `test "$LEGATO_SANDBOXED" = 1 && test ! -e ` + shellWorkflowCommand(filepath.Join((HostFS{}).Real(data), "auth-token")) + ` && printf completed > sandbox-result`
							args, err := (sandbox.Bwrap{}).Wrap(plan, []string{"sh", "-c", script})
							if err != nil {
								t.Fatal(err)
							}
							if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
								t.Fatalf("workflow sandbox: %v: %s", err, out)
							}
							if got, err := os.ReadFile(filepath.Join(cloneAt(false), "sandbox-result")); err != nil || string(got) != "completed" {
								t.Fatalf("sandbox output in clone: %q, %v", got, err)
							}
						})
						// The clone exception must not expose the data directory or sibling clones.
						for _, m := range plan.Mounts {
							if m.Path == (HostFS{}).Real(data) && m.Kind != sandbox.HideDir {
								t.Fatalf("data exposed: %+v", m)
							}
						}
						// Get the digest from the packet output, exactly as the agent receives it.
						var packetOutputs struct {
							Digest string `json:"packet_digest"`
						}
						for _, output := range view.Steps {
							if output.StepID == "packet" {
								if err := json.Unmarshal([]byte(output.OutputsJSON), &packetOutputs); err != nil {
									t.Fatal(err)
								}
							}
						}
						operations, _ := json.Marshal(map[string]any{"schema_version": 1, "packet_digest": packetOutputs.Digest, "operations": []any{}})
						var attemptID string
						for _, attempt := range view.Attempts {
							if attempt.StepID == step {
								attemptID = attempt.ID
							}
						}
						view, err = workflows.Complete(ctx, view.Run.ID, step, attemptID, map[string]string{"operations": string(operations)}, nil)
						if err != nil {
							t.Fatalf("complete %s: %v", step, err)
						}
					}
					if view.Run.Status != "succeeded" {
						t.Fatalf("run: %+v", view.Run)
					}
					createTask(t, db, "unrelated")
					refused, err := workflows.Start(ctx, definitions[0], map[string]string{"task": "unrelated", "working_dir": cloneAt(workLinked)})
					if err != nil {
						t.Fatal(err)
					}
					_, err = workflows.Complete(ctx, refused.Run.ID, "interview", refused.Attempts[0].ID, map[string]string{"available": "true", "request_id": "other-reflection"}, nil)
					if err == nil || !strings.Contains(err.Error(), "holds credentials or Legato's own state") {
						t.Fatalf("unrelated task was not refused: %v", err)
					}
					if len(backend.plans) != 2 {
						t.Fatal("refused workflow reached the sandbox backend")
					}
				})
			}
		}
	}
}

func fmtHomeSpellings(data, record, work bool) string {
	spelling := func(link bool) string {
		if link {
			return "home"
		}
		return "var-home"
	}
	return "data=" + spelling(data) + "/record=" + spelling(record) + "/work=" + spelling(work)
}
