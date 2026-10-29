// Package mcpserver serves a list of tools to one MCP client over a pair of
// streams, as a stdio MCP server does for the agent tool that started it. It
// knows the protocol and nothing about what the tools do.
package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Definition describes a tool to the client and to its model.
type Definition struct {
	Name        string
	Title       string
	Description string
	// ReadOnly marks a tool that changes nothing the person would have to
	// undo. Clients use it to decide whether a call needs approval.
	ReadOnly bool
}

// Result is what a tool call hands back.
type Result struct {
	// Text is what the model reads.
	Text string
	// Data, when set, is the same result as structured content. Some clients
	// give their model Data in place of Text, so set it only when it says
	// everything Text does.
	Data any
}

// Tool is one tool a server offers. NewTool makes one.
type Tool struct {
	// Name is the name a client calls the tool by.
	Name string
	add  func(*mcp.Server)
	spec Spec
	call func(context.Context, json.RawMessage) (Result, error)
}

// Spec is a tool's definition with its input schema, as JSON: enough to
// offer the same tool elsewhere (NewRelayTool).
type Spec struct {
	Definition
	InputSchema json.RawMessage `json:"input_schema"`
}

// Spec describes the tool.
func (t Tool) Spec() Spec { return t.spec }

// Call runs the tool on raw arguments, checked against its schema as a
// client's call would be.
func (t Tool) Call(ctx context.Context, args json.RawMessage) (Result, error) {
	if t.call == nil {
		return Result{}, fmt.Errorf("tool %q cannot be called directly", t.Name)
	}
	return t.call(ctx, args)
}

// NewTool returns a tool whose arguments are decoded into In. The tool's
// input schema is inferred from In, with each property's description read
// from its `jsonschema` struct tag, and arguments that do not fit it are
// turned down before handler runs. An error from handler reaches the model as
// the call's result, so it can correct itself.
//
// NewTool panics when In has no JSON schema, which is a mistake in the tool's
// definition, not something a caller can meet at run time.
func NewTool[In any](def Definition, handler func(context.Context, In) (Result, error)) Tool {
	schema, err := jsonschema.For[In](nil)
	if err != nil {
		panic(fmt.Sprintf("mcpserver: tool %q: %v", def.Name, err))
	}
	nameOneType(schema)
	closedWorld := false
	tool := &mcp.Tool{
		Name:        def.Name,
		Title:       def.Title,
		Description: def.Description,
		InputSchema: schema,
		Annotations: &mcp.ToolAnnotations{
			Title:        def.Title,
			ReadOnlyHint: def.ReadOnly,
			// A Legato tool acts on the person's own Legato only, and none of
			// them removes anything.
			DestructiveHint: &closedWorld,
			OpenWorldHint:   &closedWorld,
		},
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		panic(fmt.Sprintf("mcpserver: tool %q: %v", def.Name, err))
	}
	rawSchema, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("mcpserver: tool %q: %v", def.Name, err))
	}
	call := func(ctx context.Context, raw json.RawMessage) (Result, error) {
		if len(bytes.TrimSpace(raw)) == 0 {
			raw = json.RawMessage(`{}`)
		}
		var value map[string]any
		if err := json.Unmarshal(raw, &value); err != nil {
			return Result{}, fmt.Errorf("the arguments are not a JSON object: %w", err)
		}
		if err := resolved.Validate(value); err != nil {
			return Result{}, fmt.Errorf("the arguments do not fit the tool's schema: %w", err)
		}
		var in In
		if err := json.Unmarshal(raw, &in); err != nil {
			return Result{}, err
		}
		return handler(ctx, in)
	}
	return Tool{Name: def.Name, spec: Spec{Definition: def, InputSchema: rawSchema}, call: call, add: func(server *mcp.Server) {
		mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
			result, err := handler(ctx, in)
			if err != nil {
				return nil, nil, err
			}
			return &mcp.CallToolResult{
				Content:           []mcp.Content{&mcp.TextContent{Text: result.Text}},
				StructuredContent: result.Data,
			}, nil, nil
		})
	}}
}

// NewRelayTool offers a tool described by spec, whose calls handler
// forwards with their raw arguments: a server that relays another's tools.
func NewRelayTool(spec Spec, handler func(context.Context, json.RawMessage) (Result, error)) Tool {
	closedWorld := false
	tool := &mcp.Tool{
		Name: spec.Name, Title: spec.Title, Description: spec.Description,
		InputSchema: json.RawMessage(spec.InputSchema),
		Annotations: &mcp.ToolAnnotations{Title: spec.Title, ReadOnlyHint: spec.ReadOnly, DestructiveHint: &closedWorld, OpenWorldHint: &closedWorld},
	}
	return Tool{Name: spec.Name, spec: spec, call: handler, add: func(server *mcp.Server) {
		server.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			result, err := handler(ctx, req.Params.Arguments)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: result.Text}}, StructuredContent: result.Data}, nil
		})
	}}
}

// nameOneType rewrites every "this type or null" in a schema to the type
// alone. Go slices and pointers are inferred as nullable, but a tool's schema
// is passed on to model providers, and several of those turn down a property
// that names more than one type. An absent property says the same as a null.
func nameOneType(schema *jsonschema.Schema) {
	if schema == nil {
		return
	}
	var named []string
	for _, name := range schema.Types {
		if name != "null" {
			named = append(named, name)
		}
	}
	if len(named) == 1 {
		schema.Type, schema.Types = named[0], nil
	}
	for _, property := range schema.Properties {
		nameOneType(property)
	}
	nameOneType(schema.Items)
	nameOneType(schema.AdditionalProperties)
}

// Options says what a server is and what it offers.
type Options struct {
	Name    string
	Title   string
	Version string
	// Instructions tell the client's model when the tools are worth using.
	Instructions string
	Tools        []Tool
}

// Serve runs a server for the one client on the other end of in and out,
// until that client goes away or ctx ends.
func Serve(ctx context.Context, opts Options, in io.ReadCloser, out io.WriteCloser) error {
	server := mcp.NewServer(
		&mcp.Implementation{Name: opts.Name, Title: opts.Title, Version: opts.Version},
		&mcp.ServerOptions{Instructions: opts.Instructions, Capabilities: &mcp.ServerCapabilities{}},
	)
	for _, tool := range opts.Tools {
		tool.add(server)
	}
	return server.Run(ctx, &mcp.IOTransport{Reader: in, Writer: out})
}
