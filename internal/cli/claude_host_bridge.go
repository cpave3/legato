package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/broker"
	"github.com/cpave3/legato/internal/engine/store"
)

const claudeRelayLease = 15 * time.Second
const claudeRelayPoll = time.Second
const claudeCommandLimit = 256

type bridgeCommand struct {
	Seq  int64  `json:"seq"`
	Line string `json:"line"`
}
type bridgePoll struct {
	Generation string          `json:"generation"`
	Commands   []bridgeCommand `json:"commands"`
}
type bridgePollArgs struct {
	RelayID    string `json:"relay_id"`
	Generation string `json:"generation"`
	After      int64  `json:"after"`
}

type hostClaudeBridge struct {
	mu         sync.Mutex
	commands   []bridgeCommand
	next       int64
	notified   chan struct{}
	done       chan struct{}
	cancel     context.CancelFunc
	relayID    string
	generation string
	seen       time.Time
	err        error
}

func (b *hostClaudeBridge) Write(line []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-b.done:
		return 0, io.ErrClosedPipe
	default:
	}
	if len(b.commands) >= claudeCommandLimit {
		b.cancel()
		return 0, fmt.Errorf("Claude command relay is full")
	}
	b.next++
	b.commands = append(b.commands, bridgeCommand{Seq: b.next, Line: string(line)})
	close(b.notified)
	b.notified = make(chan struct{})
	return len(line), nil
}

// claudeHostOps owns authority in the host broker, never in its caller.
func claudeHostOps(db *store.Store, deps BrokerDeps) map[string]BrokerOp {
	var mu sync.Mutex
	bridges := map[string]*hostClaudeBridge{}
	return map[string]BrokerOp{
		"claude.bridge.poll": func(ctx context.Context, caller BrokerCaller, raw json.RawMessage) (any, error) {
			var args bridgePollArgs
			if err := json.Unmarshal(raw, &args); err != nil || args.RelayID == "" || len(args.RelayID) > 128 || args.After < 0 {
				return nil, refuse("bad_request", "relay identity and cursor required")
			}
			session, err := db.GetRunningAgentSession(ctx, caller.TaskID, caller.ExecutionID)
			if err != nil {
				return nil, err
			}
			if session.AgentKind != "claude-code" {
				return nil, refuse("forbidden", "only a Claude session can open a Claude relay")
			}
			mu.Lock()
			b := bridges[caller.ExecutionID]
			if b != nil {
				select {
				case <-b.done:
					delete(bridges, caller.ExecutionID)
					b = nil
				default:
				}
			}
			if b == nil {
				generation, err := bridgeToken()
				if err != nil {
					mu.Unlock()
					return nil, err
				}
				runCtx, cancel := context.WithCancel(ctx)
				b = &hostClaudeBridge{relayID: args.RelayID, generation: generation, seen: time.Now(), cancel: cancel, done: make(chan struct{}), notified: make(chan struct{})}
				bridges[caller.ExecutionID] = b
				shellRoot := ""
				if session.Sandboxed {
					if deps.ClaudeTmpDir == nil {
						delete(bridges, caller.ExecutionID)
						cancel()
						mu.Unlock()
						return nil, fmt.Errorf("sandbox Claude bridge needs an execution tmp root")
					}
					shellRoot = deps.ClaudeTmpDir(caller.ExecutionID)
					if shellRoot == "" {
						delete(bridges, caller.ExecutionID)
						cancel()
						mu.Unlock()
						return nil, fmt.Errorf("sandbox Claude tmp root is missing")
					}
				}
				go func(b *hostClaudeBridge) {
					err := RunClaudeBridge(runCtx, db, ClaudeBridgeOptions{TaskID: caller.TaskID, ExecutionID: caller.ExecutionID, Stdout: b, RuntimeDir: deps.ClaudeRuntimeDir, Models: deps.ClaudeModels, ShellRoot: shellRoot})
					b.mu.Lock()
					b.err = err
					close(b.done)
					b.mu.Unlock()
					mu.Lock()
					if bridges[caller.ExecutionID] == b {
						delete(bridges, caller.ExecutionID)
					}
					mu.Unlock()
					cancel()
				}(b)
				go func(b *hostClaudeBridge) {
					ticker := time.NewTicker(time.Second)
					defer ticker.Stop()
					for {
						select {
						case <-runCtx.Done():
							return
						case <-ticker.C:
							b.mu.Lock()
							expired := time.Since(b.seen) > claudeRelayLease
							b.mu.Unlock()
							execution, err := db.GetExecution(runCtx, caller.ExecutionID)
							if expired || err != nil || execution.Status != store.ExecutionRunning {
								cancel()
								return
							}
						}
					}
				}(b)
			}
			mu.Unlock()
			b.mu.Lock()
			if b.relayID != args.RelayID {
				b.mu.Unlock()
				return nil, refuse("conflict", "another Claude relay owns this session")
			}
			b.seen = time.Now()
			if args.Generation != b.generation {
				args.After = 0
			}
			if args.After > b.next {
				b.mu.Unlock()
				return nil, refuse("bad_request", "command cursor is ahead of bridge")
			}
			// Retire only commands the relay already wrote. A lost poll reply can
			// be retried using the same cursor, without losing the command batch.
			first := 0
			for first < len(b.commands) && b.commands[first].Seq <= args.After {
				first++
			}
			b.commands = b.commands[first:]
			notify := b.notified
			ready := len(b.commands) > 0
			b.mu.Unlock()
			if !ready {
				timer := time.NewTimer(claudeRelayPoll)
				defer timer.Stop()
				select {
				case <-ctx.Done():
					return nil, refuse("host_unavailable", "Claude bridge host is stopping")
				case <-b.done:
				case <-notify:
				case <-timer.C:
				}
			}
			b.mu.Lock()
			defer b.mu.Unlock()
			if ctx.Err() != nil {
				return nil, refuse("host_unavailable", "Claude bridge host is stopping")
			}
			select {
			case <-b.done:
				if b.err != nil {
					return nil, b.err
				}
				return nil, refuse("closed", "Claude bridge ended")
			default:
			}
			return bridgePoll{Generation: b.generation, Commands: append([]bridgeCommand(nil), b.commands...)}, nil
		},
		"claude.bridge.close": func(ctx context.Context, caller BrokerCaller, raw json.RawMessage) (any, error) {
			var args bridgePollArgs
			if err := json.Unmarshal(raw, &args); err != nil {
				return nil, err
			}
			mu.Lock()
			b := bridges[caller.ExecutionID]
			mu.Unlock()
			if b != nil && b.relayID == args.RelayID {
				b.cancel()
				select {
				case <-b.done:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			return map[string]bool{"ok": true}, nil
		},
	}
}

// RunClaudeRelay is the unprivileged child read by the mod. It owns no key
// and serves no native control routes. Losing its stdout ends its lease.
func RunClaudeRelay(ctx context.Context, socket, token string, out io.Writer) error {
	relayID, err := bridgeToken()
	if err != nil {
		return err
	}
	args := bridgePollArgs{RelayID: relayID}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = broker.Call(stopCtx, socket, token, "claude.bridge.close", args, nil)
	}()
	for {
		var result bridgePoll
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := broker.Call(callCtx, socket, token, "claude.bridge.poll", args, &result)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var refused *broker.Error
			if errors.As(err, &refused) && refused.Code != "host_unavailable" {
				return err
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Second):
				continue
			}
		}
		if result.Generation != args.Generation {
			args.Generation = result.Generation
			args.After = 0
		}
		for _, command := range result.Commands {
			if command.Seq <= args.After {
				continue
			}
			if command.Seq != args.After+1 {
				return fmt.Errorf("Claude command relay cursor gap")
			}
			if _, err := io.WriteString(out, command.Line); err != nil {
				return fmt.Errorf("writing Claude relay: %w", err)
			}
			args.After = command.Seq
		}
	}
}
