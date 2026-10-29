package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/mcpserver"
	"github.com/cpave3/legato/internal/service"
)

// MCPCredentials is the part of the credential broker request_access uses.
type MCPCredentials interface {
	Request(ctx context.Context, req service.AccessRequest) (service.AccessResult, error)
	Credentials() []service.CredentialSummary
}

type requestAccessInput struct {
	Credential string   `json:"credential" jsonschema:"the credential's name"`
	Operations []string `json:"operations" jsonschema:"what you will do with it, such as read"`
	Resources  []string `json:"resources,omitempty" jsonschema:"what you will do it to, as narrowly as the task allows: one URL, one host, one branch. Leave out for the whole credential"`
	Reason     string   `json:"reason" jsonschema:"one sentence the person reads: what you need it for"`
	Blocking   bool     `json:"blocking,omitempty" jsonschema:"true when you cannot go on without it"`
}

// noCredentials stands in for the broker where only tool names are wanted.
type noCredentials struct{}

func (noCredentials) Request(context.Context, service.AccessRequest) (service.AccessResult, error) {
	return service.AccessResult{}, nil
}
func (noCredentials) Credentials() []service.CredentialSummary { return nil }

func (m *mcpTools) requestAccess() mcpserver.Tool {
	var names []string
	for _, c := range m.opts.Credentials.Credentials() {
		names = append(names, c.Name)
	}
	known := "none"
	if len(names) > 0 {
		known = strings.Join(names, ", ")
	}
	return mcpserver.NewTool(mcpserver.Definition{
		Name:  "request_access",
		Title: "Request access to a credential",
		Description: "Asks Legato for access to a credential it holds for you (an API key, a login), for exactly the operations and resources you name, with your reason. " +
			"Ask before you need it, and ask for the smallest scope the task needs; a pattern such as a branch prefix is shown to the person as broader. " +
			"Legato answers at once when the person's rules allow it, or asks the person and waits up to a minute. " +
			"If the answer is pending, Legato tells you the decision later in a message that starts with [legato approvals]. " +
			"Never look for the secret another way. Credentials here: " + known + ".",
	}, func(ctx context.Context, in requestAccessInput) (mcpserver.Result, error) {
		if _, err := m.session(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		result, err := m.opts.Credentials.Request(ctx, service.AccessRequest{
			TaskID: m.opts.TaskID, ExecutionID: m.opts.ExecutionID,
			Credential: in.Credential, Operations: in.Operations, Resources: in.Resources,
			Reason: in.Reason, Blocking: in.Blocking,
		})
		if err != nil {
			return mcpserver.Result{}, err
		}
		return mcpserver.Result{Text: accessText(in, result)}, nil
	})
}

// AccessText tells an agent what a request for a credential came to and
// what to do next: the text of request_access and of legato credential.
func AccessText(credentialName string, operations, resources []string, result service.AccessResult) string {
	return accessText(requestAccessInput{Credential: credentialName, Operations: operations, Resources: resources}, result)
}

// accessText tells the agent what a request came to and what to do next.
func accessText(in requestAccessInput, result service.AccessResult) string {
	scope := strings.Join(in.Operations, ", ")
	if len(in.Resources) > 0 {
		scope += " on " + strings.Join(in.Resources, ", ")
	}
	switch result.Status {
	case service.AccessGranted:
		return fmt.Sprintf("Granted: %s with %s, for %s. Use the value with `legato credential exec %s --env VARIABLE -- <command>`, which keeps it out of your output; `legato credential get %s` prints it.",
			scope, in.Credential, firstNonBlank(result.Grant, "now"), in.Credential, in.Credential)
	case service.AccessPending:
		return fmt.Sprintf("Pending: Legato asked the person (%s). The decision will come in a message that starts with [legato approvals] %s. If you are not blocked, continue with other work and do not use %s until then. If you are blocked, stop and end your turn now. Do not try another way to get this credential.",
			result.ApprovalID, result.ApprovalID, in.Credential)
	default:
		return fmt.Sprintf("Denied: %s with %s. %s. Do not look for another way to get this credential; say so and go on without it, or ask the person.",
			scope, in.Credential, firstNonBlank(result.Reason, "The person did not allow it"))
	}
}

func firstNonBlank(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
