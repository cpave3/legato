package mcpserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type greetInput struct {
	Name  string `json:"name" jsonschema:"who to greet"`
	Shout bool   `json:"shout,omitempty" jsonschema:"answer in capitals"`
}

func greetTool() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name: "greet", Title: "Greet", Description: "Greets someone by name.", ReadOnly: true,
	}, func(_ context.Context, in greetInput) (mcpserver.Result, error) {
		if in.Name == "nobody" {
			return mcpserver.Result{}, errors.New("there is nobody to greet")
		}
		text := "hello " + in.Name
		if in.Shout {
			text = strings.ToUpper(text)
		}
		return mcpserver.Result{Text: text, Data: map[string]any{"greeted": in.Name}}, nil
	})
}

// connect serves opts over a pair of pipes, as a client's child process would
// be served over stdio, and returns a client session on the other end.
func connect(t *testing.T, opts mcpserver.Options) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()
	served := make(chan error, 1)
	go func() { served <- mcpserver.Serve(ctx, opts, serverIn, serverOut) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.IOTransport{Reader: clientIn, Writer: clientOut}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		select {
		case <-served:
		case <-time.After(5 * time.Second):
			t.Error("the server kept running after its client went away")
		}
	})
	return session
}

func textOf(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	var out strings.Builder
	for _, content := range result.Content {
		text, ok := content.(*mcp.TextContent)
		if !ok {
			t.Fatalf("content = %#v, want text", content)
		}
		out.WriteString(text.Text)
	}
	return out.String()
}

func TestServeOffersItsToolsToAClientAndRunsACall(t *testing.T) {
	session := connect(t, mcpserver.Options{Name: "legato", Version: "1.2.3", Tools: []mcpserver.Tool{greetTool()}})
	ctx := context.Background()

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 1 {
		t.Fatalf("tools = %#v", listed.Tools)
	}
	tool := listed.Tools[0]
	if tool.Name != "greet" || tool.Title != "Greet" || tool.Description != "Greets someone by name." {
		t.Fatalf("tool = %#v", tool)
	}
	if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
		t.Fatalf("annotations = %#v, want read-only", tool.Annotations)
	}
	schema, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Type       string `json:"type"`
		Required   []string
		Properties map[string]struct {
			Type        string `json:"type"`
			Description string `json:"description"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("schema %s: %v", schema, err)
	}
	if parsed.Type != "object" || parsed.Properties["name"].Type != "string" || parsed.Properties["name"].Description != "who to greet" || parsed.Properties["shout"].Type != "boolean" {
		t.Fatalf("schema = %s", schema)
	}
	if len(parsed.Required) != 1 || parsed.Required[0] != "name" {
		t.Fatalf("required = %v, want [name]", parsed.Required)
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "greet", Arguments: map[string]any{"name": "ada", "shout": true}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || textOf(t, result) != "HELLO ADA" {
		t.Fatalf("result = %#v (%q)", result, textOf(t, result))
	}
	structured, _ := json.Marshal(result.StructuredContent)
	if string(structured) != `{"greeted":"ada"}` {
		t.Fatalf("structured content = %s", structured)
	}
}

func TestAToolErrorReachesTheModelAsTheCallsResult(t *testing.T) {
	session := connect(t, mcpserver.Options{Name: "legato", Version: "1", Tools: []mcpserver.Tool{greetTool()}})

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "greet", Arguments: map[string]any{"name": "nobody"}})
	if err != nil {
		t.Fatalf("a tool's own error must not fail the request: %v", err)
	}
	if !result.IsError || !strings.Contains(textOf(t, result), "there is nobody to greet") {
		t.Fatalf("result = %#v (%q)", result, textOf(t, result))
	}
}

func TestArgumentsThatDoNotFitTheSchemaNeverReachTheHandler(t *testing.T) {
	called := false
	tool := mcpserver.NewTool(mcpserver.Definition{Name: "greet"}, func(_ context.Context, in greetInput) (mcpserver.Result, error) {
		called = true
		return mcpserver.Result{Text: "hello"}, nil
	})
	session := connect(t, mcpserver.Options{Name: "legato", Version: "1", Tools: []mcpserver.Tool{tool}})

	for name, arguments := range map[string]map[string]any{
		"missing required": {"shout": true},
		"wrong type":       {"name": 7},
		"unknown property": {"name": "ada", "volume": 11},
	} {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "greet", Arguments: arguments})
		rejected := err != nil || (result != nil && result.IsError)
		if !rejected {
			t.Errorf("%s: accepted %v", name, arguments)
		}
	}
	if called {
		t.Fatal("the handler ran for arguments that do not fit its schema")
	}
}

func TestAServerWithNoToolsStillAnswersAndCarriesItsInstructions(t *testing.T) {
	session := connect(t, mcpserver.Options{Name: "legato", Version: "1", Instructions: "Not inside a Legato session."})

	initialized := session.InitializeResult()
	if initialized == nil || initialized.Instructions != "Not inside a Legato session." {
		t.Fatalf("initialize result = %#v", initialized)
	}
	if initialized.ServerInfo == nil || initialized.ServerInfo.Name != "legato" {
		t.Fatalf("server info = %#v", initialized.ServerInfo)
	}
	if err := session.Ping(context.Background(), nil); err != nil {
		t.Fatalf("ping: %v", err)
	}
	listed, err := session.ListTools(context.Background(), nil)
	if err == nil && len(listed.Tools) != 0 {
		t.Fatalf("tools = %#v, want none", listed.Tools)
	}
}

type chartInput struct {
	Bars  []chartBar `json:"bars" jsonschema:"the bars"`
	Scale *float64   `json:"scale,omitempty" jsonschema:"how much to stretch the bars"`
	Rows  [][]string `json:"rows,omitempty"`
}

type chartBar struct {
	Label string   `json:"label"`
	Value *float64 `json:"value,omitempty"`
}

// Tool schemas are handed to model providers by the client, and several of
// those turn down a property that may be one of two types.
func TestInputSchemasNameOneTypePerProperty(t *testing.T) {
	var seen chartInput
	tool := mcpserver.NewTool(mcpserver.Definition{Name: "chart"}, func(_ context.Context, in chartInput) (mcpserver.Result, error) {
		seen = in
		return mcpserver.Result{Text: "drawn"}, nil
	})
	session := connect(t, mcpserver.Options{Name: "legato", Version: "1", Tools: []mcpserver.Tool{tool}})
	ctx := context.Background()

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	schema, _ := json.Marshal(listed.Tools[0].InputSchema)
	var walk func(path string, node any)
	walk = func(path string, node any) {
		switch value := node.(type) {
		case map[string]any:
			if _, union := value["type"].([]any); union {
				t.Errorf("%s names several types: %v", path, value["type"])
			}
			for _, key := range []string{"anyOf", "oneOf"} {
				if _, ok := value[key]; ok {
					t.Errorf("%s uses %s", path, key)
				}
			}
			for key, child := range value {
				walk(path+"/"+key, child)
			}
		case []any:
			for _, child := range value {
				walk(path+"[]", child)
			}
		}
	}
	var parsed any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatal(err)
	}
	walk("", parsed)
	properties := parsed.(map[string]any)["properties"].(map[string]any)
	if got := properties["bars"].(map[string]any)["type"]; got != "array" {
		t.Fatalf("bars type = %v, want array (schema %s)", got, schema)
	}
	if got := properties["scale"].(map[string]any)["type"]; got != "number" {
		t.Fatalf("scale type = %v, want number", got)
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "chart", Arguments: map[string]any{
		"bars": []any{map[string]any{"label": "Mon", "value": 0}}, "rows": []any{[]any{"a", "b"}},
	}})
	if err != nil || result.IsError {
		t.Fatalf("call = %#v, %v", result, err)
	}
	if len(seen.Bars) != 1 || seen.Bars[0].Value == nil || *seen.Bars[0].Value != 0 || seen.Scale != nil || len(seen.Rows) != 1 {
		t.Fatalf("decoded input = %#v", seen)
	}
}
