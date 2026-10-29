package cli

import (
	"context"
	"encoding/json"

	"github.com/cpave3/legato/internal/engine/mcpserver"
)

// MCPBrokerOps serve Legato's MCP tools over the broker: `legato mcp serve`
// in Legato's sandbox lists them with mcp.list and relays each call with
// mcp.call, and the host runs the tool for the caller's own session. build
// makes the server's options for a caller, with the host's store, agent
// service and credential broker.
func MCPBrokerOps(build func(BrokerCaller) MCPOptions) map[string]BrokerOp {
	server := func(caller BrokerCaller) mcpserver.Options {
		opts := build(caller)
		opts.TaskID, opts.ExecutionID = caller.TaskID, caller.ExecutionID
		return MCPServerOptions(opts)
	}
	return map[string]BrokerOp{
		"mcp.list": func(_ context.Context, caller BrokerCaller, _ json.RawMessage) (any, error) {
			opts := server(caller)
			specs := make([]mcpserver.Spec, 0, len(opts.Tools))
			for _, tool := range opts.Tools {
				specs = append(specs, tool.Spec())
			}
			return map[string]any{"instructions": opts.Instructions, "version": opts.Version, "tools": specs}, nil
		},
		"mcp.call": func(ctx context.Context, caller BrokerCaller, raw json.RawMessage) (any, error) {
			var args struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if err := json.Unmarshal(raw, &args); err != nil || args.Name == "" {
				return nil, refuse("bad_request", "name is required")
			}
			for _, tool := range server(caller).Tools {
				if tool.Name != args.Name {
					continue
				}
				result, err := tool.Call(ctx, args.Arguments)
				if err != nil {
					// The tool's own error is the call's result, for the model.
					return map[string]any{"text": err.Error(), "is_error": true}, nil
				}
				return map[string]any{"text": result.Text, "data": result.Data}, nil
			}
			return nil, refuse("unknown_tool", "this session has no tool named "+args.Name)
		},
	}
}

// MCPRelay is a server whose tools are the host's, relayed through the
// broker at socket for the session token names. It is what `legato mcp
// serve` offers in Legato's sandbox.
func MCPRelay(ctx context.Context, socket, token, version string) (mcpserver.Options, error) {
	var listed struct {
		Instructions string           `json:"instructions"`
		Tools        []mcpserver.Spec `json:"tools"`
	}
	if err := callBroker(ctx, socket, token, "mcp.list", nil, &listed); err != nil {
		return mcpserver.Options{}, err
	}
	opts := mcpserver.Options{Name: "legato", Title: "Legato", Version: version, Instructions: listed.Instructions}
	for _, spec := range listed.Tools {
		name := spec.Name
		opts.Tools = append(opts.Tools, mcpserver.NewRelayTool(spec, func(ctx context.Context, raw json.RawMessage) (mcpserver.Result, error) {
			var out struct {
				Text    string `json:"text"`
				Data    any    `json:"data"`
				IsError bool   `json:"is_error"`
			}
			if err := callBroker(ctx, socket, token, "mcp.call", map[string]any{"name": name, "arguments": raw}, &out); err != nil {
				return mcpserver.Result{}, err
			}
			if out.IsError {
				return mcpserver.Result{}, toolError(out.Text)
			}
			return mcpserver.Result{Text: out.Text, Data: out.Data}, nil
		}))
	}
	return opts, nil
}

type toolError string

func (e toolError) Error() string { return string(e) }
