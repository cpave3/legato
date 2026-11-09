package cli_test

import (
	"context"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/service"
)

type noSecrets struct{}

func (noSecrets) Request(context.Context, string, string, string) (service.SecretOutcome, error) {
	return service.SecretOutcome{}, nil
}

// everyReeveTool is a Reeve with everything its tools need, so that each
// is offered.
func everyReeveTool() *cli.MCPReeve {
	return &cli.MCPReeve{
		ChangeSets:     &fakeChangeSets{},
		Secrets:        noSecrets{},
		Legato:         &fakeLegato{},
		HostOperations: func(context.Context) bool { return true },
		Peers:          &fakeReevePeers{},
	}
}
